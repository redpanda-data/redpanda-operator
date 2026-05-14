// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console_test

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	consolecontroller "github.com/redpanda-data/redpanda-operator/operator/internal/controller/console"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestMulticlusterConsole(t *testing.T) {
	testutil.SkipIfNotMulticluster(t)
	suite.Run(t, new(MulticlusterConsoleSuite))
}

// MulticlusterConsoleSuite exercises the Console controller wired through
// [consolecontroller.Controller.SetupWithMulticlusterManager]: the multicluster
// operator binary registers Console alongside the StretchCluster reconciler,
// and a Console CR referencing a StretchCluster must reach reconciliation in
// the raft leader's local cluster.
type MulticlusterConsoleSuite struct {
	suite.Suite

	ctx context.Context
	mc  *testenv.MulticlusterEnv
}

var (
	_ suite.SetupAllSuite  = (*MulticlusterConsoleSuite)(nil)
	_ suite.SetupTestSuite = (*MulticlusterConsoleSuite)(nil)
)

func (s *MulticlusterConsoleSuite) SetupTest() {
	prev := s.ctx
	s.ctx = trace.Test(s.T())
	s.T().Cleanup(func() { s.ctx = prev })
}

func (s *MulticlusterConsoleSuite) SetupSuite() {
	t := s.T()
	s.ctx = trace.Test(t)

	s.mc = testenv.NewMulticluster(t, s.ctx, testenv.MulticlusterOptions{
		Name:               "console-mc",
		ClusterSize:        3,
		Scheme:             controller.MulticlusterScheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(s.ctx),
		WatchAllNamespaces: true,
		// Distinct CIDR block keeps this suite from colliding with the
		// existing MulticlusterControllerSuite when both run in parallel.
		CIDRBlock: 110,
		SetupFn: func(mgr multicluster.Manager) error {
			localMgr := mgr.GetLocalManager()
			ctl, err := kube.FromRESTConfig(localMgr.GetConfig(), kube.Options{
				Options: client.Options{
					Scheme: localMgr.GetScheme(),
					Cache: &client.CacheOptions{
						Reader: localMgr.GetCache(),
					},
				},
				FieldManager: string(lifecycle.DefaultFieldOwner),
			})
			if err != nil {
				return err
			}
			return (&consolecontroller.Controller{Ctl: ctl, Config: localMgr.GetConfig()}).
				SetupWithMulticlusterManager(s.ctx, mgr)
		},
	})
}

func (s *MulticlusterConsoleSuite) setup() (*testing.T, context.Context, context.CancelFunc, *testenv.MulticlusterTestNamespace) {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 5*time.Minute)
	ns := s.mc.CreateTestNamespace(t)
	return t, ctx, cancel, ns
}

// TestReconcilesAgainstStretchCluster applies a Console CR referencing a
// StretchCluster on the raft leader's local cluster and verifies the
// controller reconciles it to a Deployment in that same cluster.
func (s *MulticlusterConsoleSuite) TestReconcilesAgainstStretchCluster() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	leaderEnv := s.waitForLeaderEnv(t)
	t.Logf("[TestReconcilesAgainstStretchCluster] applying to leader env %q", leaderEnv.Name)
	s.assertConsoleReconciles(t, ctx, ns.Name, "leader", leaderEnv)
}

// TestReconcilesOnProviderCluster applies a Console CR (and its referenced
// StretchCluster) to a non-leader env. This exercises Controller.ctlFor's
// per-cluster routing — the controller running on the raft leader must
// load/apply against the peer cluster where the CR actually lives, not its
// own local cluster.
func (s *MulticlusterConsoleSuite) TestReconcilesOnProviderCluster() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	leaderEnv := s.waitForLeaderEnv(t)
	peerEnv := s.firstNonLeaderEnv(t, leaderEnv)
	t.Logf("[TestReconcilesOnProviderCluster] leader=%q applying to peer=%q", leaderEnv.Name, peerEnv.Name)
	s.assertConsoleReconciles(t, ctx, ns.Name, "peer", peerEnv)
}

// assertConsoleReconciles creates a StretchCluster + Console CR pair in the
// given env's cluster and verifies the controller produces a Deployment and
// advances the Console CR's Status.ObservedGeneration.
func (s *MulticlusterConsoleSuite) assertConsoleReconciles(t *testing.T, ctx context.Context, namespace, suffix string, env *testenv.Env) {
	t.Helper()

	stretchName := "console-stretch-" + suffix
	consoleName := "redpanda-console-" + suffix

	cl := env.Client()

	require.NoError(t, cl.Create(ctx, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: stretchName, Namespace: namespace},
		Spec: redpandav1alpha2.StretchClusterSpec{
			TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(false)},
		},
	}))

	require.NoError(t, cl.Create(ctx, &redpandav1alpha2.Console{
		ObjectMeta: metav1.ObjectMeta{Name: consoleName, Namespace: namespace},
		Spec: redpandav1alpha2.ConsoleSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: stretchName,
					Kind: ptr.To(redpandav1alpha2.StretchClusterRefKind),
				},
			},
		},
	}))

	consoleKey := types.NamespacedName{Name: consoleName, Namespace: namespace}

	require.Eventually(t, func() bool {
		var dep appsv1.Deployment
		err := cl.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      consoleName + "-console",
		}, &dep)
		if err != nil {
			t.Logf("[assertConsoleReconciles/%s] waiting for Deployment in %s: %v", suffix, env.Name, err)
			return false
		}
		return true
	}, 2*time.Minute, 2*time.Second, "Console controller never created its Deployment in env %q", env.Name)

	require.Eventually(t, func() bool {
		var cons redpandav1alpha2.Console
		if err := cl.Get(ctx, consoleKey, &cons); err != nil {
			return false
		}
		if cons.Status.ObservedGeneration != cons.Generation {
			t.Logf("[assertConsoleReconciles/%s] waiting for ObservedGeneration in %s (have %d, want %d)",
				suffix, env.Name, cons.Status.ObservedGeneration, cons.Generation)
			return false
		}
		return true
	}, 2*time.Minute, 2*time.Second, "Console.Status.ObservedGeneration never advanced in env %q", env.Name)
}

// waitForLeaderEnv polls until the raft manager reports a leader and returns
// the corresponding [testenv.Env].
func (s *MulticlusterConsoleSuite) waitForLeaderEnv(t *testing.T) *testenv.Env {
	t.Helper()
	var leaderName string
	require.Eventually(t, func() bool {
		leaderName = s.mc.PrimaryManager().GetLeader()
		return leaderName != ""
	}, 2*time.Minute, 1*time.Second, "raft leader never elected")

	for _, env := range s.mc.Envs {
		if env.Name == leaderName {
			return env
		}
	}
	t.Fatalf("leader %q does not match any env name", leaderName)
	return nil
}

// firstNonLeaderEnv returns the first env whose Name does not match the
// leader's. Used to drive the per-cluster routing test on a provider cluster.
func (s *MulticlusterConsoleSuite) firstNonLeaderEnv(t *testing.T, leader *testenv.Env) *testenv.Env {
	t.Helper()
	for _, env := range s.mc.Envs {
		if env.Name != leader.Name {
			return env
		}
	}
	t.Fatalf("no non-leader env found alongside leader %q", leader.Name)
	return nil
}
