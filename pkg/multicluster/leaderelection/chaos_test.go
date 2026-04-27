// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package leaderelection

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests document higher-level raft behavior under network conditions
// so regressions in the transport layer (especially the per-peer fan-out
// work for finding #1 in the resilience memo) can be caught at the
// state-machine level, not just at the DoSend unit-test level.
//
// Each test uses setupLockTestWithHooks to stand up a 3-node raft cluster
// with real gRPC transports on localhost and short election timings
// (100ms heartbeat, 1s election timeout), so full runs complete in
// seconds.

// findLeader returns the first node that reports IsLeader within timeout,
// and the indices of the two followers. Fails the test if the cluster
// has not converged on exactly one leader.
func findLeader(t *testing.T, leaders []*testLeader, timeout time.Duration) (leaderIdx int, followerIdxs []int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var leaderCount, found int
		for i, l := range leaders {
			if l.IsLeader() {
				leaderCount++
				found = i
			}
		}
		if leaderCount == 1 {
			followers := make([]int, 0, len(leaders)-1)
			for i := range leaders {
				if i != found {
					followers = append(followers, i)
				}
			}
			return found, followers
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("cluster did not converge on a single leader within %s", timeout)
	return -1, nil
}

// TestLeaderRemainsStable_WhenAllPeersHealthy is the happy-path
// regression control: once a leader is elected, it should not change in
// the absence of faults. Any modification to the transport that causes
// spurious re-elections will break this test.
func TestLeaderRemainsStable_WhenAllPeersHealthy(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	leaders := setupLockTest(t, ctx, 3)
	defer func() {
		for _, l := range leaders {
			l.Stop()
		}
		for _, l := range leaders {
			if !l.IsStopped() {
				l.WaitForStopped(t, 5*time.Second)
			}
		}
	}()

	leaderIdx, _ := findLeader(t, leaders, 5*time.Second)

	// Observe for 5 seconds. The initial leader must still be the leader,
	// and no follower may have transitioned to leader at any point.
	const observationWindow = 5 * time.Second
	deadline := time.Now().Add(observationWindow)
	for time.Now().Before(deadline) {
		for i, l := range leaders {
			if i == leaderIdx {
				require.True(t, l.IsLeader(), "initial leader (idx=%d) lost leadership mid-window", i)
			} else {
				require.True(t, l.IsFollower(), "follower idx=%d became leader mid-window", i)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLeaderStepsDown_WhenMinorityIsolated is the negative control: raft
// SHOULD relinquish leadership when the leader cannot reach a majority.
// Stopping both followers leaves the leader alone, and CheckQuorum must
// fire — otherwise we'd silently be running without a real quorum.
func TestLeaderStepsDown_WhenMinorityIsolated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	leaders := setupLockTest(t, ctx, 3)
	defer func() {
		for _, l := range leaders {
			l.Stop()
		}
		for _, l := range leaders {
			if !l.IsStopped() {
				l.WaitForStopped(t, 5*time.Second)
			}
		}
	}()

	leaderIdx, followerIdxs := findLeader(t, leaders, 5*time.Second)
	leader := leaders[leaderIdx]

	// Take both followers offline. The leader is now isolated and must
	// step down within a few election-timeout intervals.
	for _, idx := range followerIdxs {
		leaders[idx].Stop()
	}

	require.Eventually(t, func() bool {
		return leader.IsFollower()
	}, 10*time.Second, 100*time.Millisecond,
		"leader did not step down after losing contact with both followers")
}

// TestLeaderSurvives_AsymmetricSilentDropOnOnePeer characterises finding
// #1 from the resilience memo. Inject a silent black-hole on exactly one
// follower's ingress path (the follower's TCP listener still accepts
// connections but its Send/Check handlers hang until the caller's context
// is cancelled). A majority (the leader + the other follower) remains
// fully reachable, so raft's correctness invariant says leadership must
// not change.
//
// Today, because DoSend is synchronous in the Ready loop, one stuck peer
// can starve heartbeats to the healthy peer, and CheckQuorum steps the
// leader down despite a healthy majority. The test is intentionally left
// in place so it will automatically start passing once the per-peer
// fan-out work lands (see the fix PR for finding #1).
func TestLeaderSurvives_AsymmetricSilentDropOnOnePeer(t *testing.T) {
	// Wire a BlockIngress control into each node's TestHooks; we'll only
	// flip the one on the isolated follower.
	hooks := make([]*TestHooks, 3)
	for i := range hooks {
		hooks[i] = &TestHooks{BlockIngress: new(atomic.Bool)}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	leaders := setupLockTestWithHooks(t, ctx, 3, hooks)
	defer func() {
		for _, l := range leaders {
			l.Stop()
		}
		for _, l := range leaders {
			if !l.IsStopped() {
				l.WaitForStopped(t, 5*time.Second)
			}
		}
	}()

	leaderIdx, followerIdxs := findLeader(t, leaders, 5*time.Second)
	isolated := followerIdxs[0]
	healthy := followerIdxs[1]

	// Silently black-hole the leader-to-isolated direction.
	hooks[isolated].BlockIngress.Store(true)

	// Observe for 15 seconds (comfortably exceeds the 1s election timeout
	// configured in setupLockTest). Leader must remain leader; the healthy
	// follower must remain a follower.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		require.True(t, leaders[leaderIdx].IsLeader(),
			"leader (idx=%d) stepped down despite a healthy majority — the asymmetric drop on idx=%d stalled heartbeats to idx=%d (finding #1)",
			leaderIdx, isolated, healthy)
		require.True(t, leaders[healthy].IsFollower(),
			"healthy follower (idx=%d) started an election — the leader failed to heartbeat it through the chaos (finding #1)",
			healthy)
		time.Sleep(100 * time.Millisecond)
	}
}
