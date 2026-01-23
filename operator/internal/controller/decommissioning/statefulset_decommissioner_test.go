// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

const redpandaChartPath = "../../../../charts/redpanda/chart"

//go:embed testdata/role.yaml
var decommissionerRBAC []byte

func TestIntegrationStatefulSetDecommissioner(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	suite.Run(t, new(StatefulSetDecommissionerSuite))
}

type StatefulSetDecommissionerSuite struct {
	suite.Suite

	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	helm          *helm.Client
	clientFactory internalclient.ClientFactory
	releases      map[string]*chart
}

var _ suite.SetupAllSuite = (*StatefulSetDecommissionerSuite)(nil)

func (s *StatefulSetDecommissionerSuite) TestDecommission() {
	chart := s.installChart("basic", map[string]any{
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
	defer adminClient.Close()

	s.waitFor(func(ctx context.Context) (bool, error) {
		health, err := adminClient.GetHealthOverview(ctx)
		if err != nil {
			s.T().Log("failed to fetch health overview", "error", err)
			return false, nil
		}
		// make sure that we've removed all stale nodes
		return len(health.NodesDown) == 0, nil
	})

	var firstBroker corev1.Pod
	s.Require().NoError(s.client.Get(s.ctx, types.NamespacedName{Namespace: s.env.Namespace(), Name: chart.name + "-0"}, &firstBroker))
	var firstPVC corev1.PersistentVolumeClaim
	s.Require().NoError(s.client.Get(s.ctx, types.NamespacedName{Namespace: s.env.Namespace(), Name: "datadir-" + chart.name + "-0"}, &firstPVC))

	// now we simulate node failure by tainting a node with NoSchedule and evicting the pod
	firstBrokerNode := firstBroker.Spec.NodeName
	s.taintNode(firstBrokerNode)
	s.T().Cleanup(func() {
		s.untaintNode(firstBrokerNode)
	})
	// TODO(chrisseto): Evictions fail in CI with `Cannot evict pod as it would violate the pod's disruption budget.` but not locally.
	// For now use a forced delete as that mimics node failure equally well.
	// s.Require().NoError(s.client.SubResource("eviction").Create(s.ctx, &firstBroker, &policyv1.Eviction{}))
	s.Require().NoError(s.client.Delete(s.ctx, &firstBroker, client.GracePeriodSeconds(0)))

	s.waitFor(func(ctx context.Context) (bool, error) {
		health, err := adminClient.GetHealthOverview(ctx)
		if err != nil {
			s.T().Log("failed to fetch health overview", "error", err)
			return false, nil
		}
		// make sure that the pod has been taken offline
		return len(health.NodesDown) == 1, nil
	})

	// we have to manually delete both the broker and its PVC, which would normally
	// be done by the PVC unbinder
	s.Require().NoError(s.client.Delete(s.ctx, &firstPVC))
	s.Require().NoError(s.client.Delete(s.ctx, &firstBroker))

	s.waitFor(func(ctx context.Context) (bool, error) {
		health, err := adminClient.GetHealthOverview(ctx)
		if err != nil {
			s.T().Log("failed to fetch health overview", "error", err)
			return false, nil
		}
		// now make sure it comes back online and the broker is decommissioned
		return len(health.NodesDown) == 0, nil
	})

	s.cleanupChart(chart)
}

func (s *StatefulSetDecommissionerSuite) taintNode(name string) {
	var node corev1.Node
	s.Require().NoError(s.client.Get(s.ctx, types.NamespacedName{Name: name}, &node))
	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
		Key:    "decommission-test",
		Effect: corev1.TaintEffectNoSchedule,
	})
	s.Require().NoError(s.client.Update(s.ctx, &node))
}

func (s *StatefulSetDecommissionerSuite) untaintNode(name string) {
	var node corev1.Node
	s.Require().NoError(s.client.Get(s.ctx, types.NamespacedName{Name: name}, &node))
	node.Spec.Taints = functional.Filter(node.Spec.Taints, func(taint corev1.Taint) bool {
		return taint.Key != "decommission-test"
	})
	s.Require().NoError(s.client.Update(s.ctx, &node))
}

func (s *StatefulSetDecommissionerSuite) SetupSuite() {
	t := s.T()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	log := testr.NewWithOptions(t, testr.Options{
		Verbosity: 10,
	})

	s.ctx = context.Background()
	s.releases = map[string]*chart{}
	s.env = testenv.New(t, testenv.Options{
		// We need our own cluster for these tests since we need additional
		// agents. Otherwise we can just turn up the default... but we'll
		// need a different cluster to manipulate for node cleanup anyway.
		Name:         "decommissioning",
		Agents:       5,
		SkipVCluster: true,
		Scheme:       scheme,
		Logger:       log,
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		helmClient, err := helm.New(helm.Options{
			KubeConfig: mgr.GetConfig(),
		})
		if err != nil {
			return err
		}

		s.helm = helmClient
		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient(), nil).WithDialer(dialer.DialContext)

		decommissioner := decommissioning.NewStatefulSetDecommissioner(
			mgr,
			func(ctx context.Context, sts *appsv1.StatefulSet) (*rpadmin.AdminAPI, error) {
				// NB: We expect to see some errors here. The release isn't
				// populated until after the chart installation finishes.
				name := sts.Labels["app.kubernetes.io/instance"]
				release, ok := s.releases[name]
				if !ok {
					return nil, errors.Newf("no release: %q", name)
				}
				return s.adminClientFor(release), nil
			},
			// set these low so that we don't have to wait forever in the test
			// these settings should give about a 5-10 second window before
			// actually running a decommission
			decommissioning.WithDelayedCacheInterval(5*time.Second),
			decommissioning.WithDelayedCacheMaxCount(2),
			decommissioning.WithRequeueTimeout(2*time.Second),
		)

		if err := decommissioner.SetupWithManager(mgr); err != nil {
			return err
		}

		return nil
	})
}

type chart struct {
	name    string
	release helm.Release
	values  map[string]any
}

func (s *StatefulSetDecommissionerSuite) installChart(name string, overrides map[string]any) *chart {
	values := map[string]any{
		"statefulset": map[string]any{
			"replicas": 1,
			"sideCars": map[string]any{
				"image": map[string]any{
					"repository": "localhost/redpanda-operator",
					"tag":        "dev",
				},
			},
		},
		"console": map[string]any{
			"enabled": false,
		},
		"external": map[string]any{
			"enabled": false,
		},
	}

	if overrides != nil {
		values = functional.MergeMaps(values, overrides)
	}

	release, err := s.helm.Install(s.ctx, redpandaChartPath, helm.InstallOptions{
		CreateNamespace: true,
		Name:            name,
		Namespace:       s.env.Namespace(),
		Values:          values,
	})
	s.Require().NoError(err)

	c := &chart{
		name:    name,
		values:  values,
		release: release,
	}

	s.releases[name] = c

	return c
}

func (s *StatefulSetDecommissionerSuite) adminClientFor(chart *chart) *rpadmin.AdminAPI {
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

func (s *StatefulSetDecommissionerSuite) upgradeChart(chart *chart, overrides map[string]any) {
	values := functional.MergeMaps(chart.values, overrides)
	release, err := s.helm.Upgrade(s.ctx, chart.release.Name, redpandaChartPath, helm.UpgradeOptions{
		Namespace: s.env.Namespace(),
		Values:    values,
	})
	s.Require().NoError(err)

	chart.release = release
	chart.values = values
}

func (s *StatefulSetDecommissionerSuite) cleanupChart(chart *chart) {
	s.Require().NoError(s.helm.Uninstall(s.ctx, chart.release))
}

func (s *StatefulSetDecommissionerSuite) setupRBAC() string {
	roles, err := kube.DecodeYAML(decommissionerRBAC, s.client.Scheme())
	s.Require().NoError(err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Inject additional permissions required for running in testenv.
	clusterRole.Rules = append(clusterRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods/portforward"},
		Verbs:     []string{"*"},
	}, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	})

	name := "testenv-" + testenv.RandString(6)

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

func (s *StatefulSetDecommissionerSuite) applyAndWait(objs ...client.Object) {
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

func (s *StatefulSetDecommissionerSuite) applyAndWaitFor(cond func(client.Object) bool, objs ...client.Object) {
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

func (s *StatefulSetDecommissionerSuite) waitFor(cond func(ctx context.Context) (bool, error)) {
	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, cond))
}
