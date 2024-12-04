package redpanda_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/helm-charts/pkg/helm"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSidecarDecommissionController(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test as -short was specified")
	}
	suite.Run(t, new(SidecarDecommissionControllerSuite))
}

type SidecarDecommissionControllerSuite struct {
	suite.Suite

	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	helm          *helm.Client
	clientFactory internalclient.ClientFactory
}

var _ suite.SetupAllSuite = (*SidecarDecommissionControllerSuite)(nil)

func (s *SidecarDecommissionControllerSuite) TestBasicTriggering() {
	chart := s.installChart("trigger", "", map[string]any{
		"statefulset": map[string]any{
			"replicas": 5,
		},
	})

	s.upgradeChart(chart, map[string]any{
		"statefulset": map[string]any{
			"replicas": 4,
		},
	})

	s.waitFor(func(ctx context.Context) (bool, error) {
		var pvcs corev1.PersistentVolumeClaimList
		if err := s.client.List(ctx, &pvcs, client.InNamespace(s.env.Namespace())); err != nil {
			return false, err
		}
		// make sure we've deleted the PVC
		return len(pvcs.Items) == 4, nil
	})

	adminClient := s.adminClientFor(chart)

	s.waitFor(func(ctx context.Context) (bool, error) {
		health, err := adminClient.GetHealthOverview(ctx)
		if err != nil {
			return false, err
		}
		// make sure that we've removed all stale nodes
		return len(health.NodesDown) == 0, nil
	})

	s.cleanupChart(chart)
}

func (s *SidecarDecommissionControllerSuite) SetupSuite() {
	t := s.T()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	s.ctx = context.Background()
	s.env = testenv.New(t, testenv.Options{
		Scheme: scheme,
		Logger: testr.New(t),
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		helmClient, err := helm.New(helm.Options{
			KubeConfig: mgr.GetConfig(),
		})
		if err != nil {
			return err
		}
		if err := helmClient.RepoAdd(s.ctx, "redpandadata", "https://charts.redpanda.com"); err != nil {
			return err
		}

		s.helm = helmClient
		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		decommissioner := decommissioning.NewStatefulSetDecommissioner(mgr, decommissioning.NewHelmFetcher(mgr), decommissioning.WithFactory(s.clientFactory))
		if err := (&redpanda.SidecarDecommissionReconciler{
			Client:         mgr.GetClient(),
			Decommissioner: decommissioner,
		}).SetupWithManager(mgr); err != nil {
			return err
		}

		return nil
	})
}

type chart struct {
	name    string
	version string
	release helm.Release
	values  map[string]any
}

func (s *SidecarDecommissionControllerSuite) installChart(name, version string, overrides map[string]any) *chart {
	values := map[string]any{
		"statefulset": map[string]any{
			"replicas": 1,
		},
		"console": map[string]any{
			"enabled": false,
		},
		"external": map[string]any{
			"enabled": false,
		},
		"image": map[string]any{
			"repository": "redpandadata/redpanda-unstable",
			"tag":        "v24.3.1-rc8",
		},
	}

	if overrides != nil {
		values = functional.MergeMaps(values, overrides)
	}

	release, err := s.helm.Install(s.ctx, "redpandadata/redpanda", helm.InstallOptions{
		Version:         version,
		CreateNamespace: true,
		Name:            name,
		Namespace:       s.env.Namespace(),
		Values:          values,
	})
	s.Require().NoError(err)

	return &chart{
		name:    name,
		version: version,
		values:  values,
		release: release,
	}
}

func (s *SidecarDecommissionControllerSuite) adminClientFor(chart *chart) *rpadmin.AdminAPI {
	data, err := json.Marshal(chart.values)
	s.Require().NoError(err)

	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chart.name,
			Namespace: s.env.Namespace(),
		},
		Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{}},
	}

	err = json.Unmarshal(data, cluster)
	s.Require().NoError(err)

	adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, cluster)
	s.Require().NoError(err)

	return adminClient
}

func (s *SidecarDecommissionControllerSuite) upgradeChart(chart *chart, overrides map[string]any) {
	values := functional.MergeMaps(chart.values, overrides)
	release, err := s.helm.Upgrade(s.ctx, chart.release.Name, "redpandadata/redpanda", helm.UpgradeOptions{
		Version:   chart.version,
		Namespace: s.env.Namespace(),
		Values:    values,
	})
	s.Require().NoError(err)

	chart.release = release
	chart.values = values
}

func (s *SidecarDecommissionControllerSuite) cleanupChart(chart *chart) {
	s.Require().NoError(s.helm.Uninstall(s.ctx, chart.release))
}

func (s *SidecarDecommissionControllerSuite) setupRBAC() string {
	roles, err := kube.DecodeYAML(operatorRBAC, s.client.Scheme())
	s.Require().NoError(err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Inject additional permissions required for running in testenv.
	role.Rules = append(role.Rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods/portforward"},
		Verbs:     []string{"*"},
	})

	name := "testenv-" + randString(6)

	role.Name = name
	role.Namespace = s.env.Namespace()
	clusterRole.Name = name
	clusterRole.Namespace = s.env.Namespace()

	s.applyAndWait(roles...)
	s.applyAndWait(
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.Name,
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.Name,
			},
		},
	)

	return name
}

func (s *SidecarDecommissionControllerSuite) applyAndWait(objs ...client.Object) {
	s.applyAndWaitFor(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *corev1.Secret, *corev1.ConfigMap, *corev1.ServiceAccount,
			*rbacv1.ClusterRole, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRoleBinding:
			return true

		default:
			s.T().Fatalf("unhandled object %T in applyAndWait", obj)
			panic("unreachable")
		}
	}, objs...)
}

func (s *SidecarDecommissionControllerSuite) applyAndWaitFor(cond func(client.Object) bool, objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := s.client.GroupVersionKindFor(obj)
		s.NoError(err)

		obj.SetManagedFields(nil)
		obj.GetObjectKind().SetGroupVersionKind(gvk)

		s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))
	}

	for _, obj := range objs {
		s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
			if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				return false, err
			}

			if cond(obj) {
				return true, nil
			}

			s.T().Logf("waiting for %T %q to be ready", obj, obj.GetName())
			return false, nil
		}))
	}
}

func (s *SidecarDecommissionControllerSuite) waitFor(cond func(ctx context.Context) (bool, error)) {
	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, cond))
}
