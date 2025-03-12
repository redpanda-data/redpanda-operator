// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller_test

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/resources"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// operatorRBAC is the ClusterRole and Role generated via controller-gen and
// goembeded so it can be used for tests.
//
//go:embed role.yaml
var operatorRBAC []byte

func TestIntegrationClusterController(t *testing.T) {
	testutil.SkipIfNotIntegration(t)
	suite.Run(t, new(ClusterControllerSuite))
}

type ClusterControllerSuite struct {
	suite.Suite

	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	clientFactory internalclient.ClientFactory
}

var _ suite.SetupAllSuite = (*ClusterControllerSuite)(nil)

func (s *ClusterControllerSuite) TestBasicScaleUpAndDownReconciliation() {
	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(5)

	s.applyAndWait(rp)

	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)

	s.applyAndWait(rp)

	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(4)

	s.applyAndWait(rp)

	s.deleteAndWait(rp)
}

func (s *ClusterControllerSuite) SetupSuite() {
	t := s.T()

	s.ctx = context.Background()
	s.env = testenv.New(t, testenv.Options{
		Scheme: controller.V2Scheme,
		CRDs:   crds.All(),
		Logger: testr.New(t),
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		resourceManager := resources.NewV2ResourceManager(mgr)
		resourceClient := resources.NewResourceClient(mgr, resourceManager)

		if err := (&controller.ClusterReconciler[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda]{
			Client:          mgr.GetClient(),
			ResourceManager: resourceManager,
			ResourceClient:  resourceClient,
			ClientFactory:   s.clientFactory,
		}).SetupWithManager(mgr); err != nil {
			return err
		}

		return nil
	})

	t.Cleanup(func() {
		// Due to a fun race condition in testenv, we need to clear out all the
		// redpandas before we can let testenv shutdown. If we don't, the
		// operator's ClusterRoles and Roles may get GC'd before all the Redpandas
		// do which will prevent the operator from cleaning up said Redpandas.
		var redpandas redpandav1alpha2.RedpandaList
		s.NoError(s.env.Client().List(s.ctx, &redpandas))

		for _, rp := range redpandas.Items {
			s.deleteAndWait(&rp)
		}
	})
}

func (s *ClusterControllerSuite) setupRBAC() string {
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

func (s *ClusterControllerSuite) minimalRP() *redpandav1alpha2.Redpanda {
	return &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rp-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			// Any empty structs are to make setting them more ergonomic
			// without having to worry about nil pointers.
			ChartRef: redpandav1alpha2.ChartRef{},
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Config: &redpandav1alpha2.Config{},
				External: &redpandav1alpha2.External{
					// Disable NodePort creation to stop broken tests from blocking others due to port conflicts.
					Enabled: ptr.To(false),
				},
				Image: &redpandav1alpha2.RedpandaImage{
					Repository: ptr.To("redpandadata/redpanda"), // Use docker.io to make caching easier and to not inflate our own metrics.
				},
				Console: &redpandav1alpha2.RedpandaConsole{
					Enabled: ptr.To(false), // Speed up most cases by not enabling console to start.
				},
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1), // Speed up tests ever so slightly.
					PodAntiAffinity: &redpandav1alpha2.PodAntiAffinity{
						// Disable the default "hard" affinity so we can
						// schedule multiple redpanda Pods on a single
						// kubernetes node. Useful for tests that require > 3
						// brokers.
						Type: ptr.To("soft"),
					},
					// Speeds up managed decommission tests. Decommissioned
					// nodes will take the entirety of
					// TerminationGracePeriodSeconds as the pre-stop hook
					// doesn't account for decommissioned nodes.
					TerminationGracePeriodSeconds: ptr.To(10),
				},
				Resources: &redpandav1alpha2.Resources{
					CPU: &redpandav1alpha2.CPU{
						// Inform redpanda/seastar that it's not going to get
						// all the resources it's promised.
						Overprovisioned: ptr.To(true),
					},
				},
			},
		},
	}
}

func (s *ClusterControllerSuite) deleteAndWait(obj client.Object) {
	gvk, err := s.client.GroupVersionKindFor(obj)
	s.NoError(err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if err := s.client.Delete(s.ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		// obj, might not exist at all. If so, no-op.
		if apierrors.IsNotFound(err) {
			return
		}
		s.Require().NoError(err)
	}

	s.waitFor(obj, func(_ client.Object, err error) (bool, error) {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func (s *ClusterControllerSuite) applyAndWait(objs ...client.Object) {
	s.applyAndWaitFor(func(obj client.Object, err error) (bool, error) {
		if err != nil {
			return false, err
		}

		switch obj := obj.(type) {
		case *redpandav1alpha2.Redpanda:
			ready := apimeta.IsStatusConditionTrue(obj.Status.Conditions, "Quiesced")
			upToDate := obj.Generation != 0 && obj.Generation == obj.Status.ObservedGeneration
			return upToDate && ready, nil

		case *corev1.Secret, *corev1.ConfigMap, *corev1.ServiceAccount,
			*rbacv1.ClusterRole, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRoleBinding:
			return true, nil

		default:
			s.T().Fatalf("unhandled object %T in applyAndWait", obj)
			panic("unreachable")
		}
	}, objs...)
}

func (s *ClusterControllerSuite) applyAndWaitFor(cond func(client.Object, error) (bool, error), objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := s.client.GroupVersionKindFor(obj)
		s.NoError(err)

		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		obj.GetObjectKind().SetGroupVersionKind(gvk)

		s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))
	}

	for _, obj := range objs {
		s.waitFor(obj, cond)
	}
}

func (s *ClusterControllerSuite) waitFor(obj client.Object, cond func(client.Object, error) (bool, error)) {
	start := time.Now()
	lastLog := time.Now()
	logEvery := 10 * time.Second

	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		err = s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)

		done, err = cond(obj, err)
		if done || err != nil {
			return done, err
		}

		if time.Since(lastLog) > logEvery {
			lastLog = time.Now()
			s.T().Logf("waited %s for %T %q", time.Since(start), obj, obj.GetName())
		}

		return false, nil
	}))
}
