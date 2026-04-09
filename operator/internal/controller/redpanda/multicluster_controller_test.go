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

	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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

	ctx context.Context
	mc  *testenv.MulticlusterEnv
}

var (
	_ suite.SetupAllSuite  = (*MulticlusterControllerSuite)(nil)
	_ suite.SetupTestSuite = (*MulticlusterControllerSuite)(nil)
)

func (s *MulticlusterControllerSuite) setup() (*testing.T, context.Context, context.CancelFunc, *testenv.MulticlusterTestNamespace) {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 5*time.Minute)
	ns := s.mc.CreateTestNamespace(t)
	return t, ctx, cancel, ns
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

	cloudSecrets := lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false}
	redpandaImage := lifecycle.Image{
		Repository: os.Getenv("TEST_REDPANDA_REPO"),
		Tag:        os.Getenv("TEST_REDPANDA_VERSION"),
	}
	sidecarImage := lifecycle.Image{
		Repository: "localhost/redpanda-operator",
		Tag:        "dev",
	}

	s.mc = testenv.NewMulticluster(t, s.ctx, testenv.MulticlusterOptions{
		Name:               "multicluster",
		ClusterSize:        3,
		Scheme:             controller.MulticlusterScheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(s.ctx),
		WatchAllNamespaces: true,
		SetupFn: func(mgr multicluster.Manager) error {
			return redpanda.SetupMulticlusterController(s.ctx, mgr, redpandaImage, sidecarImage, cloudSecrets, nil)
		},
	})
}

func (s *MulticlusterControllerSuite) TestManagesFinalizers() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	nn := types.NamespacedName{Name: "stretch", Namespace: ns.Name}

	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: nn.Name,
		},
	})

	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &cluster); err != nil {
				t.Logf("[TestManagesFinalizers] Get on env %d (%s): %v", i, env.Name, err)
				return false
			}
			t.Logf("[TestManagesFinalizers] Get on env %d (%s): finalizers=%v", i, env.Name, cluster.Finalizers)
			return slices.Contains(cluster.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d (%s) never contained finalizer", i, env.Name))
	}

	s.mc.DeleteAll(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{})

	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			err := cl.Get(ctx, nn, &cluster)
			return k8sapierrors.IsNotFound(err)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d was never deleted", i))
	}
}

func (s *MulticlusterControllerSuite) TestSpecConsistencyConditionSetOnDrift() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	nn := types.NamespacedName{Name: "spec-drift", Namespace: ns.Name}

	// Apply identical StretchCluster to all clusters.
	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: nn.Name,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			CommonLabels: map[string]string{"env": "prod"},
		},
	})

	// Wait for the reconciler to pick it up (finalizer added = reconciler ran).
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &sc); err != nil {
				return false
			}
			return slices.Contains(sc.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d never got finalizer", i))
	}

	// With identical specs, SpecSynced should be True.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
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
	driftedEnv := s.mc.Envs[0]
	var sc redpandav1alpha2.StretchCluster
	require.NoError(t, driftedEnv.Client().Get(ctx, nn, &sc))
	sc.Spec.CommonLabels = map[string]string{"env": "staging"}
	require.NoError(t, driftedEnv.Client().Update(ctx, &sc))

	// The reconciler should detect drift and set the condition.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
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
	for _, env := range s.mc.Envs {
		var sc redpandav1alpha2.StretchCluster
		if err := env.Client().Get(ctx, nn, &sc); err != nil {
			continue
		}
		cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			require.Contains(t, cond.Message, "commonLabels", "condition message should mention the drifting field")
		}
	}

	// Fix the drift: align the spec back.
	require.NoError(t, driftedEnv.Client().Get(ctx, nn, &sc))
	sc.Spec.CommonLabels = map[string]string{"env": "prod"}
	require.NoError(t, driftedEnv.Client().Update(ctx, &sc))

	// The condition should go back to True.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, 1*time.Minute, 1*time.Second, "SpecSynced condition never went back to True after fixing drift")
}
