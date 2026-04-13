// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build integration

package multicluster_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// engageTracker records Engage calls per cluster name, captures the context
// passed to each call, and can optionally block Engage for specific clusters.
type engageTracker struct {
	started atomic.Bool

	mu      sync.Mutex
	engaged map[string]int
	ctxs    map[string][]context.Context
	blockOn map[string]chan struct{}
}

func newEngageTracker() *engageTracker {
	return &engageTracker{
		engaged: make(map[string]int),
		ctxs:    make(map[string][]context.Context),
		blockOn: make(map[string]chan struct{}),
	}
}

func (e *engageTracker) Start(ctx context.Context) error {
	e.started.Store(true)
	<-ctx.Done()
	e.started.Store(false)
	return nil
}

func (e *engageTracker) Engage(ctx context.Context, name string, _ cluster.Cluster) error {
	e.mu.Lock()
	e.engaged[name]++
	e.ctxs[name] = append(e.ctxs[name], ctx)
	ch := e.blockOn[name]
	e.mu.Unlock()

	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (e *engageTracker) NeedLeaderElection() bool {
	return true
}

func (e *engageTracker) engageCount(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.engaged[name]
}

func (e *engageTracker) lastContext(name string) context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctxs := e.ctxs[name]
	if len(ctxs) == 0 {
		return nil
	}
	return ctxs[len(ctxs)-1]
}

// engageNode bundles a raft manager with its registered engage trackers.
type engageNode struct {
	name     string
	mgr      multicluster.Manager
	trackers []*engageTracker
	cancel   context.CancelFunc
	done     chan error
	env      *kube.Ctl
}

// setupEngageTestClusters creates numClusters single-replica raft clusters, each
// with numTrackers engageTracker runnables registered. Returns the nodes and a
// cleanup function.
func setupEngageTestClusters(t *testing.T, ctx context.Context, logger logr.Logger, numClusters, numTrackers int) []*engageNode {
	t.Helper()

	ports := testutil.FreePorts(t, numClusters)
	peers := make([]multicluster.RaftCluster, numClusters)
	for i := range numClusters {
		peers[i] = multicluster.RaftCluster{
			Name:    fmt.Sprintf("cluster-%d", i),
			Address: fmt.Sprintf("127.0.0.1:%d", ports[i]),
		}
	}

	nodes := make([]*engageNode, numClusters)
	for i := range numClusters {
		env := kubetest.NewEnv(t)
		rctx, rcancel := context.WithCancel(ctx)

		mgr, err := multicluster.NewRaftRuntimeManager(&multicluster.RaftConfiguration{
			Name:               peers[i].Name,
			Address:            peers[i].Address,
			Peers:              peers,
			RestConfig:         env.RestConfig(),
			Scheme:             env.Scheme(),
			Logger:             logger.WithName(peers[i].Name),
			Insecure:           true,
			SkipNameValidation: true,
			ElectionTimeout:    raftElectionTimeout,
			HeartbeatInterval:  raftHeartbeatInterval,
			GRPCMaxBackoff:     raftGRPCMaxBackoff,
		})
		require.NoError(t, err)

		trackers := make([]*engageTracker, numTrackers)
		for j := range numTrackers {
			trackers[j] = newEngageTracker()
			require.NoError(t, mgr.Add(trackers[j]))
		}

		done := make(chan error, 1)
		go func() {
			done <- mgr.Start(rctx)
			close(done)
		}()

		nodes[i] = &engageNode{
			name:     peers[i].Name,
			mgr:      mgr,
			trackers: trackers,
			cancel:   rcancel,
			done:     done,
			env:      env,
		}
	}

	t.Cleanup(func() {
		for _, n := range nodes {
			n.cancel()
		}
		for _, n := range nodes {
			<-n.done
		}
	})

	return nodes
}

// findLeaderNode returns the node that IS the raft leader (not just any
// node that knows the leader). Returns nil if no leader elected yet.
func findLeaderNode(nodes []*engageNode) *engageNode {
	for _, n := range nodes {
		leader := n.mgr.GetLeader()
		if leader == "" {
			continue
		}
		// Find the node whose name matches the leader.
		for _, m := range nodes {
			if m.name == leader {
				return m
			}
		}
	}
	return nil
}

// TestIntegrationRestartNotifiesAllRunnables verifies that when a new cluster is
// added via AddOrReplaceCluster, ALL registered runnables are re-engaged for
// the new cluster — not just one.
//
// BUG: The restart channel (capacity 1) is shared across all runnables'
// wrapStart goroutines. Only one goroutine receives each restart signal,
// so the other runnables never learn about the new cluster.
func TestIntegrationRestartNotifiesAllRunnables(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	logger := testr.New(t)
	ctrllog.SetLogger(logger)
	ctx := testutil.Context(t)

	// 3 clusters, 1 replica each, 2 trackers per replica.
	nodes := setupEngageTestClusters(t, ctx, logger, 3, 2)

	// Wait for raft leader.
	var leaderNode *engageNode
	require.Eventually(t, func() bool {
		leaderNode = findLeaderNode(nodes)
		return leaderNode != nil
	}, waitTimeout, waitInterval, "raft leader should be elected")
	t.Logf("leader: %s", leaderNode.mgr.GetLeader())

	// Wait for both trackers to start on the leader.
	require.Eventually(t, func() bool {
		return leaderNode.trackers[0].started.Load() && leaderNode.trackers[1].started.Load()
	}, shortTimeout, shortInterval, "both trackers should be started on the leader")

	// Create a new cluster (reuse envtest from node 0).
	newCluster, err := cluster.New(nodes[0].env.RestConfig(), func(o *cluster.Options) {
		o.Scheme = nodes[0].env.Scheme()
	})
	require.NoError(t, err)

	// Add the new cluster. This triggers the restart signal.
	require.NoError(t, leaderNode.mgr.AddOrReplaceCluster(ctx, "new-cluster", newCluster))

	// ASSERTION: Both trackers should be engaged for "new-cluster".
	// With the shared-channel bug, only one tracker will be notified.
	require.Eventually(t, func() bool {
		c0 := leaderNode.trackers[0].engageCount("new-cluster")
		c1 := leaderNode.trackers[1].engageCount("new-cluster")
		t.Logf("engage counts for new-cluster: tracker0=%d, tracker1=%d", c0, c1)
		return c0 > 0 && c1 > 0
	}, shortTimeout, shortInterval,
		"both runnables should be engaged for the new cluster")
}

// TestIntegrationEngageContextCancellation verifies that the context passed to
// Engage is derived from the leader context — not context.Background() — so that
// a blocked Engage call is cancelled when leadership is lost.
//
// BUG: doEngage() calls r.Engage(context.Background(), ...) which means
// a blocked Engage never receives cancellation, even when the leader
// context is cancelled. This can cause a permanent deadlock when Engage
// blocks on GetInformer/WaitForCacheSync for an unreachable cluster.
func TestIntegrationEngageContextCancellation(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	logger := testr.New(t)
	ctrllog.SetLogger(logger)
	ctx := testutil.Context(t)

	// 3 clusters, 1 replica each, 1 tracker per replica.
	nodes := setupEngageTestClusters(t, ctx, logger, 3, 1)

	// Wait for raft leader.
	var leaderNode *engageNode
	require.Eventually(t, func() bool {
		leaderNode = findLeaderNode(nodes)
		return leaderNode != nil
	}, waitTimeout, waitInterval, "raft leader should be elected")
	t.Logf("leader: %s", leaderNode.mgr.GetLeader())

	tracker := leaderNode.trackers[0]

	// Wait for the tracker to start.
	require.Eventually(t, func() bool {
		return tracker.started.Load()
	}, shortTimeout, shortInterval, "tracker should be started on the leader")

	// Set up the tracker to block Engage for "new-cluster".
	blockCh := make(chan struct{})
	t.Cleanup(func() {
		// Ensure any leaked Engage goroutine can exit during cleanup.
		select {
		case <-blockCh:
		default:
			close(blockCh)
		}
	})
	tracker.mu.Lock()
	tracker.blockOn["new-cluster"] = blockCh
	tracker.mu.Unlock()

	// Create a new cluster and add it (triggers restart → doEngage → blocked Engage).
	newCluster, err := cluster.New(nodes[0].env.RestConfig(), func(o *cluster.Options) {
		o.Scheme = nodes[0].env.Scheme()
	})
	require.NoError(t, err)
	require.NoError(t, leaderNode.mgr.AddOrReplaceCluster(ctx, "new-cluster", newCluster))

	// Wait for the Engage to be entered (it should block on blockCh).
	require.Eventually(t, func() bool {
		return tracker.engageCount("new-cluster") > 0
	}, shortTimeout, shortInterval, "Engage should be called for the new cluster")

	// Now cancel the leader node's context, simulating leadership loss.
	leaderNode.cancel()

	// The blocked Engage should return because its context should be cancelled.
	// With the bug (context.Background()), the Engage stays blocked forever
	// and the manager goroutine leaks.
	engageReturned := make(chan struct{})
	go func() {
		// Wait for the manager to fully stop.
		<-leaderNode.done
		close(engageReturned)
	}()

	select {
	case <-engageReturned:
		// Manager stopped — but did the Engage context get cancelled?
		// Check the context that was passed to Engage.
		engageCtx := tracker.lastContext("new-cluster")
		require.NotNil(t, engageCtx, "Engage should have received a context")
		require.ErrorIs(t, engageCtx.Err(), context.Canceled,
			"the context passed to Engage should be cancelled when leadership is lost")
	case <-time.After(15 * time.Second):
		// The manager did not stop in time. This means the Engage goroutine
		// is leaked because it used context.Background().
		// Unblock it so the test can clean up, then fail.
		close(blockCh)
		t.Fatal("manager did not stop within 15s — Engage is stuck because doEngage uses context.Background() instead of the leader context")
	}
}
