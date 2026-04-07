// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestMulticlusterController(t *testing.T) {
	testutil.SkipIfNotMulticluster(t)
	suite.Run(t, new(MulticlusterControllerSuite))
}

type MulticlusterControllerSuite struct {
	suite.Suite

	ctx  context.Context
	envs []*testenv.Env
}

var (
	_ suite.SetupAllSuite  = (*MulticlusterControllerSuite)(nil)
	_ suite.SetupTestSuite = (*MulticlusterControllerSuite)(nil)
)

func (s *MulticlusterControllerSuite) TestManagesFinalizers() {
	s.ApplyAll(&redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stretch",
			Namespace: "multicluster",
		},
	})

	for _, env := range s.envs {
		client := env.Client()
		s.Require().Eventually(func() bool {
			var cluster redpandav1alpha2.StretchCluster
			s.Require().NoError(client.Get(s.ctx, types.NamespacedName{
				Name:      "stretch",
				Namespace: "multicluster",
			}, &cluster))

			return slices.Contains(cluster.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in %s never contained finalizer", env.Name))
	}

	s.DeleteAll(&redpandav1alpha2.StretchCluster{})

	for _, env := range s.envs {
		client := env.Client()
		s.Require().Eventually(func() bool {
			var cluster redpandav1alpha2.StretchCluster
			err := client.Get(s.ctx, types.NamespacedName{
				Name:      "stretch",
				Namespace: "multicluster",
			}, &cluster)

			return k8sapierrors.IsNotFound(err)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in %s was never deleted", env.Name))
	}
}

func (s *MulticlusterControllerSuite) Client(n int) client.Client {
	return s.envs[n].Client()
}

func (s *MulticlusterControllerSuite) DeleteAll(objs ...client.Object) {
	for _, env := range s.envs {
		for _, obj := range objs {
			s.Require().NoError(env.Client().DeleteAllOf(s.ctx, obj))
		}
	}
}

func (s *MulticlusterControllerSuite) ApplyAll(objs ...client.Object) {
	for _, env := range s.envs {
		apply := func(objs ...client.Object) {
			for _, obj := range objs {
				gvk, err := env.Client().GroupVersionKindFor(obj)
				s.NoError(err)

				obj.SetManagedFields(nil)
				obj.SetResourceVersion("")
				obj.GetObjectKind().SetGroupVersionKind(gvk)

				s.Require().NoError(env.Client().Patch(s.ctx, obj.DeepCopyObject().(client.Object), client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO: migrate to client.Apply() with typed apply configurations
			}
		}
		apply(objs...)
	}
}

func (s *MulticlusterControllerSuite) SetupTest() {
	prev := s.ctx
	s.ctx = trace.Test(s.T())
	s.T().Cleanup(func() {
		s.ctx = prev
	})
}

func (s *MulticlusterControllerSuite) SetupSuite() {
	t := s.T()
	s.ctx = trace.Test(t)

	clusterSize := 3
	ports := testutil.FreePorts(t, clusterSize)

	for i := range clusterSize {
		s.envs = append(s.envs, testenv.New(t, testenv.Options{
			Name:         fmt.Sprintf("multicluster-%d", i),
			Agents:       1,
			Scheme:       controller.MulticlusterScheme,
			CRDs:         crds.All(),
			Network:      "multicluster",
			Namespace:    "multicluster",
			Logger:       log.FromContext(s.ctx).WithName(fmt.Sprintf("multicluster-%d", i)),
			SkipVCluster: true,
		}))
	}
	cloudSecrets := lifecycle.CloudSecretsFlags{
		CloudSecretsEnabled: false,
	}
	redpandaImage := lifecycle.Image{
		Repository: os.Getenv("TEST_REDPANDA_REPO"),
		Tag:        os.Getenv("TEST_REDPANDA_VERSION"),
	}
	sidecarImage := lifecycle.Image{
		Repository: "localhost/redpanda-operator",
		Tag:        "dev",
	}

	for i, env := range s.envs {
		peers := []multicluster.RaftCluster{}
		for i, peer := range s.envs {
			peers = append(peers, multicluster.RaftCluster{
				Name:       peer.Name,
				Address:    fmt.Sprintf("127.0.0.1:%d", ports[i]),
				Kubeconfig: peer.RESTConfig(),
			})
		}

		env.SetupMulticlusterManager(s.setupMulticlusterRBAC(env), fmt.Sprintf("127.0.0.1:%d", ports[i]), peers, func(mgr multicluster.Manager) error {
			return redpanda.SetupMulticlusterController(s.ctx, mgr, redpandaImage, sidecarImage, cloudSecrets, nil)
		})
	}
}

func (s *MulticlusterControllerSuite) setupMulticlusterRBAC(env *testenv.Env) string {
	roles, err := kube.DecodeYAML(operatorRBAC, env.Client().Scheme())
	s.Require().NoError(err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Inject additional permissions required for running in testenv.
	// For this style of tests we port-forward into Pods to emulate "in-cluster networking"
	// and we need list and get pods in kube-system to emulate in cluster DNS.
	// As our client is namespace scoped, it's non-trivial to make a kube-system
	// dedicated role so we're settling with overscoped Pod get and list
	// permissions.
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
	role.Namespace = env.Namespace()
	clusterRole.Name = name
	clusterRole.Namespace = env.Namespace()

	apply := func(objs ...client.Object) {
		for _, obj := range objs {
			gvk, err := env.Client().GroupVersionKindFor(obj)
			s.NoError(err)

			obj.SetManagedFields(nil)
			obj.SetResourceVersion("")
			obj.GetObjectKind().SetGroupVersionKind(gvk)

			s.Require().NoError(env.Client().Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO: migrate to client.Apply() with typed apply configurations
		}
	}

	apply(roles...)
	apply(
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
				{Kind: "ServiceAccount", Namespace: env.Namespace(), Name: name},
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
				{Kind: "ServiceAccount", Namespace: env.Namespace(), Name: name},
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

func (s *MulticlusterControllerSuite) TestSpecConsistencyConditionSetOnDrift() {
	nn := types.NamespacedName{Name: "spec-drift", Namespace: "multicluster"}

	// Apply identical StretchCluster to all clusters.
	s.ApplyAll(&redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			CommonLabels: map[string]string{"env": "prod"},
		},
	})

	// Wait for the reconciler to pick it up (finalizer added = reconciler ran).
	for _, env := range s.envs {
		cl := env.Client()
		s.Require().Eventually(func() bool {
			var sc redpandav1alpha2.StretchCluster
			s.Require().NoError(cl.Get(s.ctx, nn, &sc))
			return slices.Contains(sc.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in %s never got finalizer", env.Name))
	}

	// With identical specs, SpecSynced should be True.
	s.Require().Eventually(func() bool {
		for _, env := range s.envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(s.ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, 1*time.Minute, 1*time.Second, "SpecSynced=True condition never appeared")

	// Introduce drift: patch the spec on one cluster only.
	driftedEnv := s.envs[0]
	var sc redpandav1alpha2.StretchCluster
	s.Require().NoError(driftedEnv.Client().Get(s.ctx, nn, &sc))
	sc.Spec.CommonLabels = map[string]string{"env": "staging"}
	s.Require().NoError(driftedEnv.Client().Update(s.ctx, &sc))

	// The reconciler should detect drift and set the condition.
	s.Require().Eventually(func() bool {
		for _, env := range s.envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(s.ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == "DriftDetected" {
				return true
			}
		}
		return false
	}, 1*time.Minute, 1*time.Second, "SpecSynced=False condition never appeared after drift")

	// Verify the condition message mentions the differing field.
	for _, env := range s.envs {
		var sc redpandav1alpha2.StretchCluster
		if err := env.Client().Get(s.ctx, nn, &sc); err != nil {
			continue
		}
		cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			s.Contains(cond.Message, "commonLabels", "condition message should mention the drifting field")
		}
	}

	// Fix the drift: align the spec back.
	s.Require().NoError(driftedEnv.Client().Get(s.ctx, nn, &sc))
	sc.Spec.CommonLabels = map[string]string{"env": "prod"}
	s.Require().NoError(driftedEnv.Client().Update(s.ctx, &sc))

	// The condition should go back to True.
	s.Require().Eventually(func() bool {
		for _, env := range s.envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(s.ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, 1*time.Minute, 1*time.Second, "SpecSynced condition never went back to True after fixing drift")

	s.DeleteAll(&redpandav1alpha2.StretchCluster{})
}
