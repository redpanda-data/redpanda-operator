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
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

const (
	// Raft timing.
	// Raft election timeout is 10x the heartbeat interval, giving enough
	// margin for heartbeats to be missed under envtest load without
	// triggering spurious elections.
	raftElectionTimeout   = 2 * time.Second
	raftHeartbeatInterval = 200 * time.Millisecond
	// gRPC reconnect backoff is capped low so that when a peer restarts
	// (standby replacing a killed active), the leader reconnects quickly
	// instead of waiting up to the default 5s.
	raftGRPCMaxBackoff = 1 * time.Second

	// K8s lease timing is deliberately short to speed up failover in
	// tests. The lease duration must exceed the renew deadline, and the
	// retry period controls how quickly a standby notices a released
	// lease. These values ensure failover completes within a few seconds
	// rather than the typical 15-30s.
	leaseDuration      = 5 * time.Second
	leaseRenewDeadline = 4 * time.Second
	leaseRetryPeriod   = 1 * time.Second
	leaseID            = "raft-leader-test"

	// waitTimeout / waitInterval are for operations that involve a full
	// election cycle (initial raft election or post-failover recovery),
	// which may need multiple raft election timeouts to converge.
	waitTimeout  = 60 * time.Second
	waitInterval = 500 * time.Millisecond

	// shortTimeout / shortInterval are for lookups that should resolve
	// quickly because the lease is already held and the raft group is
	// stable (e.g. finding which replica is active).
	shortTimeout  = 10 * time.Second
	shortInterval = 200 * time.Millisecond

	// stopTimeout bounds how long we wait for a cancelled replica to
	// shut down its raft transport and release its K8s lease. This
	// must exceed leaseDuration to allow a clean lease release.
	stopTimeout = 15 * time.Second
)

// trackingRunnable is a LeaderElectionRunnable that records whether its
// Start method has been called. This lets us verify the double-election
// gating: only the replica holding both the K8s lease and raft leadership
// should have its runnables started.
type trackingRunnable struct {
	started atomic.Bool
}

func (r *trackingRunnable) Start(ctx context.Context) error {
	r.started.Store(true)
	<-ctx.Done()
	r.started.Store(false)
	return nil
}

func (r *trackingRunnable) Engage(_ context.Context, _ string, _ cluster.Cluster) error {
	return nil
}

func (r *trackingRunnable) NeedLeaderElection() bool {
	return true
}

// replica represents a single operator instance within a cluster.
type replica struct {
	name     string
	manager  multicluster.Manager
	cancel   context.CancelFunc
	done     chan error
	stopped  bool
	runnable *trackingRunnable
}

// stop cancels the replica's context and waits for it to shut down.
func (r *replica) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(stopTimeout):
		t.Fatalf("replica %s did not stop in time", r.name)
	}
	r.stopped = true
}

// testCluster holds the replicas for a single envtest-backed cluster.
type testCluster struct {
	name     string
	replicas []*replica
}

// findActiveAndStandbys returns the active (lease-holding) replica and all
// live standby replicas within this cluster.
func (tc *testCluster) findActiveAndStandbys() (active *replica, standbys []*replica, found bool) {
	for _, r := range tc.replicas {
		if r.stopped {
			continue
		}
		if r.manager.GetLeader() != "" {
			for _, s := range tc.replicas {
				if s != r && !s.stopped {
					standbys = append(standbys, s)
				}
			}
			return r, standbys, true
		}
	}
	return nil, nil, false
}

// setupClusters creates numClusters envtest instances with numReplicas each,
// all wired together via raft with local leader election enabled. Each
// replica gets a trackingRunnable registered as a LeaderElectionRunnable
// to verify the double-election gating.
func setupClusters(t *testing.T, ctx context.Context, logger logr.Logger, numClusters, numReplicas int) []*testCluster {
	t.Helper()

	ports := testutil.FreePorts(t, numClusters)

	peers := make([]multicluster.RaftCluster, numClusters)
	for i := range numClusters {
		peers[i] = multicluster.RaftCluster{
			Name:    fmt.Sprintf("cluster-%d", i),
			Address: fmt.Sprintf("127.0.0.1:%d", ports[i]),
		}
	}

	clusters := make([]*testCluster, numClusters)
	for i := range numClusters {
		ctl := kubetest.NewEnv(t)
		clusterName := peers[i].Name

		clusters[i] = &testCluster{
			name:     clusterName,
			replicas: make([]*replica, numReplicas),
		}
		for j := range numReplicas {
			replicaName := fmt.Sprintf("%s-r%d", clusterName, j)
			rctx, rcancel := context.WithCancel(ctx)

			mgr, err := multicluster.NewRaftRuntimeManager(multicluster.RaftConfiguration{
				Name:               clusterName,
				Address:            peers[i].Address,
				Peers:              peers,
				RestConfig:         ctl.RestConfig(),
				Scheme:             ctl.Scheme(),
				Logger:             logger.WithName(replicaName),
				Insecure:           true,
				SkipNameValidation: true,
				ElectionTimeout:    raftElectionTimeout,
				HeartbeatInterval:  raftHeartbeatInterval,
				GRPCMaxBackoff:     raftGRPCMaxBackoff,
				LocalLeaderElection: &multicluster.LocalLeaderElectionConfig{
					ID:            leaseID,
					Namespace:     "default",
					LeaseDuration: leaseDuration,
					RenewDeadline: leaseRenewDeadline,
					RetryPeriod:   leaseRetryPeriod,
				},
				BaseContext: func() context.Context {
					return ctx
				},
			})
			require.NoError(t, err)

			// Register a tracking runnable before starting the manager
			// so we can verify it only runs on the double-elected replica.
			tr := &trackingRunnable{}
			require.NoError(t, mgr.Add(tr))

			done := make(chan error, 1)
			go func() {
				done <- mgr.Start(rctx)
				close(done)
			}()

			clusters[i].replicas[j] = &replica{
				name:     replicaName,
				manager:  mgr,
				cancel:   rcancel,
				done:     done,
				runnable: tr,
			}
		}
	}

	t.Cleanup(func() {
		for _, c := range clusters {
			for _, r := range c.replicas {
				r.cancel()
			}
		}
		for _, c := range clusters {
			for _, r := range c.replicas {
				<-r.done
			}
		}
	})

	return clusters
}

// waitForLeader waits until at least one live replica across all clusters
// reports a raft leader, then returns that leader name.
func waitForLeader(t *testing.T, clusters []*testCluster) string {
	t.Helper()

	var leader string
	require.Eventually(t, func() bool {
		for _, c := range clusters {
			for _, r := range c.replicas {
				if r.stopped {
					continue
				}
				if l := r.manager.GetLeader(); l != "" {
					leader = l
					return true
				}
			}
		}
		return false
	}, waitTimeout, waitInterval, "raft leader should be elected")
	return leader
}

// assertLeaderConsensus verifies every live active replica agrees on the
// same raft leader and returns that leader name.
func assertLeaderConsensus(t *testing.T, clusters []*testCluster) string {
	t.Helper()

	var leader string
	for _, c := range clusters {
		for _, r := range c.replicas {
			if r.stopped {
				continue
			}
			if l := r.manager.GetLeader(); l != "" {
				if leader == "" {
					leader = l
				}
				require.Equal(t, leader, l, "replica %s disagrees on raft leader", r.name)
			}
		}
	}
	require.NotEmpty(t, leader, "at least one replica should report a raft leader")
	return leader
}

// waitForExactlyOneRunnable waits until exactly one live replica across
// all clusters has its tracking runnable started.
func waitForExactlyOneRunnable(t *testing.T, clusters []*testCluster) {
	t.Helper()
	require.Eventually(t, func() bool {
		var count int
		for _, c := range clusters {
			for _, r := range c.replicas {
				if !r.stopped && r.runnable.started.Load() {
					count++
				}
			}
		}
		return count == 1
	}, shortTimeout, shortInterval, "exactly one replica should have its runnable started")
}

// assertExactlyOneRunnableStarted verifies that exactly one live replica
// across all clusters has its tracking runnable started, and that it
// belongs to the raft leader's cluster.
func assertExactlyOneRunnableStarted(t *testing.T, clusters []*testCluster, raftLeader string) {
	t.Helper()

	var count int
	var found *replica
	var foundCluster *testCluster
	for _, c := range clusters {
		for _, r := range c.replicas {
			if r.stopped {
				continue
			}
			if r.runnable.started.Load() {
				count++
				found = r
				foundCluster = c
			}
		}
	}
	require.Equal(t, 1, count, "exactly one replica should have its runnable started")
	require.Equal(t, raftLeader, foundCluster.name, "runnable should be started on the raft leader's cluster (started on %s)", found.name)
}

// clusterForLeader returns the testCluster whose name matches the raft leader.
func clusterForLeader(clusters []*testCluster, leader string) *testCluster {
	for _, c := range clusters {
		if c.name == leader {
			return c
		}
	}
	return nil
}

// TestIntegrationDoubleLeaderElection validates the "double leader-election"
// mechanism: K8s lease-based election within each local cluster, wrapping
// cross-cluster raft election.
//
// Setup:
//   - 3 envtest instances (simulating 3 K8s clusters)
//   - 2 replicas per cluster (competing for the local K8s lease)
//   - Only the lease holder starts the raft node
//   - Each replica has a tracking runnable registered as a
//     LeaderElectionRunnable
//
// The test verifies:
//  1. A raft leader is elected (proves double-election works end-to-end)
//  2. Standby replicas do not participate in raft
//  3. Exactly one replica (the raft leader's local lease holder) has its
//     controller runnables started
//  4. After killing the active replica in each cluster one-by-one, the
//     standby takes over and the raft group recovers every time
//  5. After each failover, exactly one replica still has its runnables
//     started
func TestIntegrationDoubleLeaderElection(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	logger := testr.New(t)
	ctrllog.SetLogger(logger)

	ctx := testutil.Context(t)
	clusters := setupClusters(t, ctx, logger, 3, 2)

	// Wait for initial raft leader election.
	t.Log("waiting for initial raft leader election...")
	leader := waitForLeader(t, clusters)
	t.Logf("initial raft leader: %s", leader)

	// Verify all active replicas agree on the leader.
	assertLeaderConsensus(t, clusters)

	// Verify that exactly one replica has its controller runnable started,
	// and it's the one in the raft leader's cluster.
	t.Log("verifying controller runnable gating...")
	waitForExactlyOneRunnable(t, clusters)
	assertExactlyOneRunnableStarted(t, clusters, leader)

	// For each cluster, identify the active replica, verify the standby
	// is not participating, kill the active, and confirm the standby
	// takes over and the raft group stabilizes.
	for _, c := range clusters {
		t.Logf("--- %s: finding active/standby replicas ---", c.name)

		var active *replica
		var standbys []*replica
		require.Eventually(t, func() bool {
			var found bool
			active, standbys, found = c.findActiveAndStandbys()
			return found
		}, shortTimeout, shortInterval, "%s should have an active replica", c.name)

		t.Logf("%s: active=%s, standbys=%d", c.name, active.name, len(standbys))

		for _, s := range standbys {
			require.Empty(t, s.manager.GetLeader(), "%s is standby and should not know the raft leader", s.name)
		}

		// Kill the active replica. LeaderElectionReleaseOnCancel ensures
		// the K8s lease is released immediately and the raft transport
		// shuts down, freeing the port.
		t.Logf("%s: killing active replica %s...", c.name, active.name)
		active.stop(t)
		t.Logf("%s: active replica stopped", c.name)

		// One of the standbys should acquire the K8s lease, start its
		// raft node (catching up via snapshot), and report a raft leader.
		t.Logf("%s: waiting for a standby to take over...", c.name)
		require.Eventually(t, func() bool {
			for _, s := range standbys {
				if s.manager.GetLeader() != "" {
					return true
				}
			}
			return false
		}, waitTimeout, waitInterval, "%s: a standby should become active and report a raft leader", c.name)

		newLeader := assertLeaderConsensus(t, clusters)
		t.Logf("%s: raft leader after failover: %s", c.name, newLeader)

		// After failover, verify exactly one replica still has its
		// runnable started — the new raft leader's local lease holder.
		t.Logf("%s: verifying runnable gating after failover...", c.name)
		waitForExactlyOneRunnable(t, clusters)
		assertExactlyOneRunnableStarted(t, clusters, newLeader)
	}
}

// TestIntegrationFollowerDeath validates that when a follower cluster's active
// replica dies and its standby takes over with fresh MemoryStorage, the raft
// leader reactivates the peer's progress tracker and sends a snapshot so the
// new follower catches up.
//
// This exercises the synthetic MsgHeartbeatResp path in the Send handler:
// without it, the leader marks the restarted peer as inactive and stops
// producing messages, causing a permanent deadlock.
//
// Setup:
//   - 3 envtest instances, 2 replicas each (same as the main test)
//
// The test:
//  1. Waits for initial raft leader election
//  2. Identifies a follower cluster (not the raft leader's cluster)
//  3. Kills the follower cluster's active replica
//  4. Verifies the standby takes over and the raft group recovers
//  5. Verifies exactly one runnable is still started (on the leader's cluster)
func TestIntegrationFollowerDeath(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	logger := testr.New(t)
	ctrllog.SetLogger(logger)

	ctx := testutil.Context(t)
	clusters := setupClusters(t, ctx, logger, 3, 2)

	// Wait for initial raft leader election.
	t.Log("waiting for initial raft leader election...")
	leader := waitForLeader(t, clusters)
	t.Logf("initial raft leader: %s", leader)

	assertLeaderConsensus(t, clusters)

	// Wait for the runnable to be started on the leader's cluster.
	waitForExactlyOneRunnable(t, clusters)
	assertExactlyOneRunnableStarted(t, clusters, leader)

	// Find a follower cluster (any cluster that is NOT the raft leader's).
	leaderCluster := clusterForLeader(clusters, leader)
	require.NotNil(t, leaderCluster, "should find the leader cluster")

	var followerCluster *testCluster
	for _, c := range clusters {
		if c != leaderCluster {
			followerCluster = c
			break
		}
	}
	require.NotNil(t, followerCluster, "should find a follower cluster")
	t.Logf("raft leader on %s, will kill %s's active replica", leaderCluster.name, followerCluster.name)

	// Identify the active replica in the follower cluster.
	var active *replica
	require.Eventually(t, func() bool {
		var found bool
		active, _, found = followerCluster.findActiveAndStandbys()
		return found
	}, shortTimeout, shortInterval, "%s should have an active replica", followerCluster.name)
	t.Logf("%s: active=%s", followerCluster.name, active.name)

	// Kill the follower's active replica. Its standby will acquire the K8s
	// lease and start a fresh raft node with empty MemoryStorage. The leader
	// must reactivate the peer (via the synthetic MsgHeartbeatResp) and send
	// a snapshot so it can catch up.
	t.Logf("killing %s...", active.name)
	active.stop(t)
	t.Log("follower active replica stopped")

	// Wait for a standby in the follower cluster to take over and report
	// the raft leader. This proves the leader reactivated the peer.
	t.Log("waiting for follower standby to take over...")
	require.Eventually(t, func() bool {
		for _, r := range followerCluster.replicas {
			if r != active && !r.stopped && r.manager.GetLeader() != "" {
				return true
			}
		}
		return false
	}, waitTimeout, waitInterval, "%s: standby should become active and report a raft leader", followerCluster.name)

	newLeader := assertLeaderConsensus(t, clusters)
	t.Logf("raft leader after follower death: %s (should be unchanged: %s)", newLeader, leader)

	// The raft leader should not have changed — we only killed a follower.
	require.Equal(t, leader, newLeader, "raft leader should be unchanged after follower death")

	// Verify exactly one runnable is still started.
	waitForExactlyOneRunnable(t, clusters)
	assertExactlyOneRunnableStarted(t, clusters, newLeader)
}
