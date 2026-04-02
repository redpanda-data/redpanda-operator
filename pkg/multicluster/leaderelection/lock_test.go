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
	"fmt"
	"log"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/raft/v3"
	raftpb "go.etcd.io/raft/v3/raftpb"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// TestLaggingPeerCatchesUpViaSnapshot reproduces the "need non-empty snapshot"
// panic that occurred when a follower's log index fell below the leader's
// compacted index. The fix calls storage.CreateSnapshot before storage.Compact
// so the leader always has a valid snapshot to send to lagging peers.
//
// Scenario:
//  1. Start a 3-node cluster and wait for initial leader election.
//  2. Stop one follower to simulate a network partition.
//  3. Wait for at least one full compaction cycle (~10 s) on the running nodes.
//  4. Restart the follower; its fresh in-memory log is behind the compacted index.
//  5. The leader must catch it up via snapshot — without this fix it panicked.
func TestLaggingPeerCatchesUpViaSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	// Wait for the cluster to elect its first leader.
	_, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Isolate one follower. The two remaining nodes retain quorum.
	laggingNode := followers[0]
	laggingNode.Stop()
	laggingNode.WaitForStopped(t, 5*time.Second)
	t.Logf("isolated follower %d", laggingNode.config.ID)

	// Wait for at least one full compaction cycle (compaction runs every ~10 s).
	// After this point the leader's MemoryStorage no longer holds the entries
	// the lagging node needs, so only a snapshot can bring it up to date.
	t.Log("waiting for log compaction to occur on running nodes (~10 s)...")
	select {
	case <-time.After(12 * time.Second):
	case <-ctx.Done():
		t.Fatal("timed out while waiting for compaction")
	}

	// Drain the stale follower signal that was written when the node first
	// initialised as a follower, so WaitForFollower below only sees the
	// signal from the post-restart catch-up.
	select {
	case <-laggingNode.follower:
	default:
	}

	// Restart the lagging node. Its fresh MemoryStorage starts at index 0,
	// which is below the leader's compacted index. The leader must send a
	// snapshot. Before the fix this triggered: panic("need non-empty snapshot").
	t.Logf("restarting follower %d", laggingNode.config.ID)
	laggingNode.Start(t, ctx)

	// The node must settle as a follower, proving snapshot-based catch-up works.
	laggingNode.WaitForFollower(t, 30*time.Second)
	t.Logf("follower %d successfully caught up via snapshot", laggingNode.config.ID)
}

// TestCompactionRaceWithCatchUp validates the guard "applied <= lastStored"
// added to the compaction goroutine in runRaft.
//
// Background: node.Status().Applied can return an index that the raft node has
// accepted into its unstable log but that the Ready loop has not yet persisted
// to MemoryStorage via storage.Append. Without the guard, calling
// storage.CreateSnapshot(applied, ...) when applied > storage.LastIndex()
// panics with "snapshot X is out of bound lastindex(Y)".
//
// The scenario that reliably triggers the race:
//  1. Isolate a follower so the two remaining nodes continue committing entries.
//  2. Wait for at least one compaction cycle so the leader's log is compacted.
//  3. Restart the follower: the leader sends a snapshot, the follower applies
//     it, and Applied jumps forward — potentially ahead of what the local
//     Ready loop has yet written to storage.
//  4. Allow the cluster to run for two more compaction cycles (~22 s). Without
//     the guard the compaction goroutine would panic during this window.
func TestCompactionRaceWithCatchUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	// Wait for the cluster to elect its first leader.
	_, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Isolate one follower. The two remaining nodes retain quorum and keep
	// committing heartbeat entries.
	laggingNode := followers[0]
	laggingNode.Stop()
	laggingNode.WaitForStopped(t, 5*time.Second)
	t.Logf("isolated follower %d", laggingNode.config.ID)

	// Wait for at least one full compaction cycle on the running nodes.
	t.Log("waiting for first compaction cycle on running nodes (~10 s)...")
	select {
	case <-time.After(12 * time.Second):
	case <-ctx.Done():
		t.Fatal("timed out waiting for first compaction")
	}

	// Drain any stale follower signal from initial startup so
	// WaitForFollower below only observes the post-restart signal.
	select {
	case <-laggingNode.follower:
	default:
	}

	// Restart the follower. The leader will send a snapshot to bring it
	// up to date. After applying the snapshot, Applied jumps forward while
	// the local Ready loop may not yet have persisted those entries — this
	// is the exact window the guard protects against.
	t.Logf("restarting follower %d", laggingNode.config.ID)
	laggingNode.Start(t, ctx)
	laggingNode.WaitForFollower(t, 30*time.Second)
	t.Logf("follower %d rejoined the cluster", laggingNode.config.ID)

	// Allow two more compaction cycles (~22 s) to fire while the follower
	// is actively catching up. Without the guard, the compaction goroutine
	// panics during this window. A panic crashes the test binary immediately.
	t.Log("checking cluster stability across two more compaction cycles (~22 s)...")
	select {
	case <-time.After(22 * time.Second):
		// No panic — the guard worked correctly.
	case err := <-laggingNode.err:
		t.Fatalf("follower failed after rejoining: %v", err)
	case <-ctx.Done():
		t.Fatal("context cancelled during stability check")
	}

	if laggingNode.IsStopped() {
		t.Fatal("follower stopped unexpectedly during compaction cycles")
	}
	t.Logf("follower %d remained stable across multiple compaction cycles", laggingNode.config.ID)
}

// TestFreshNodeJoinsRunningCluster validates the MsgHeartbeat guard added to
// grpcTransport.Send in raft.go.
//
// Background: raft.StartNode with N peers appends N ConfChange entries,
// giving the fresh node lastIndex=N. After the first leader election the
// leader writes a no-op entry, advancing commit to N+1 or higher. Without the
// guard, the next MsgHeartbeat from the leader carries Commit=N+1, causing the
// raft library to call commitTo(N+1) on the fresh follower whose lastIndex is
// still N — resulting in:
//
//	panic: tocommit(N+1) is out of range [lastIndex(N)]. Was the raft log
//	corrupted, truncated, or lost?
//
// This becomes a crash loop: every pod restart produces a fresh node with
// lastIndex=N, and every heartbeat from the still-running leader re-triggers
// the panic before the snapshot catch-up can land.
//
// The guard in Send intercepts such heartbeats and returns Applied=false,
// giving the leader time to discover the follower is lagging and send MsgSnap.
// Once the snapshot is applied, heartbeats no longer exceed lastIndex and
// processing continues normally.
func TestFreshNodeJoinsRunningCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	// Wait for leader election. By this point the leader has written its
	// no-op entry, so commit >= len(peers)+1 = 4 > lastIndex of a fresh node.
	_, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Simulate a pod restart: stop one follower then immediately restart it
	// with fresh in-memory state. runRaft always creates a new MemoryStorage,
	// so the restarted node begins with lastIndex=3 (3 ConfChange entries from
	// StartNode) while the leader's commit is already >= 4.
	laggingNode := followers[0]
	laggingNode.Stop()
	laggingNode.WaitForStopped(t, 5*time.Second)
	t.Logf("stopped follower %d, restarting with fresh state", laggingNode.config.ID)

	// Drain the initial follower signal so WaitForFollower sees only the
	// post-restart transition.
	select {
	case <-laggingNode.follower:
	default:
	}

	// Restart. Without the heartbeat guard this panics immediately because
	// the first heartbeat from the leader has Commit > lastIndex(3).
	laggingNode.Start(t, ctx)
	laggingNode.WaitForFollower(t, 30*time.Second)
	t.Logf("follower %d rejoined running cluster without panic", laggingNode.config.ID)
}

// TestSnapshotAppliedBeforeAppend verifies that the Ready loop in runRaft
// applies a snapshot to MemoryStorage before appending entries.
//
// This is a direct unit test for the fix: without storage.ApplySnapshot,
// storage.Append panics with "missing log entry [last: N, append at: M]"
// when there is a gap between MemoryStorage's last index and the first
// entry in rd.Entries (which follows a snapshot the leader sent).
//
// The test simulates a fresh follower's MemoryStorage state:
//   - 3 initial ConfChange entries (lastIndex=3), simulating raft.StartNode
//     with 3 peers.
//   - A snapshot at index 5 with term 2, simulating what the leader sends
//     via MsgSnap after the cluster has progressed.
//   - Entries starting at index 6, which follow the snapshot.
//
// Without applying the snapshot first, Append sees last=3 and first
// entry at index 6 — a gap — and panics. With ApplySnapshot, storage
// advances to index 5, and the Append at index 6 succeeds.
func TestSnapshotAppliedBeforeAppend(t *testing.T) {
	storage := raft.NewMemoryStorage()

	// Simulate a fresh follower: 3 ConfChange entries from StartNode.
	initialEntries := []raftpb.Entry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
	}
	require.NoError(t, storage.Append(initialEntries))

	lastIdx, err := storage.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIdx)

	// Simulate a snapshot from the leader at index 5.
	snap := raftpb.Snapshot{
		Metadata: raftpb.SnapshotMetadata{
			Index: 5,
			Term:  2,
			ConfState: raftpb.ConfState{
				Voters: []uint64{1, 2, 3},
			},
		},
	}

	// Entries that follow the snapshot.
	postSnapEntries := []raftpb.Entry{
		{Index: 6, Term: 2},
		{Index: 7, Term: 2},
	}

	// Without applying the snapshot, Append must panic.
	require.Panics(t, func() {
		_ = storage.Append(postSnapEntries)
	}, "Append should panic when there is a gap between lastIndex and first entry")

	// Re-create storage to get a clean state (the panic may have left it inconsistent).
	storage = raft.NewMemoryStorage()
	require.NoError(t, storage.Append(initialEntries))

	// Now apply the snapshot first — this is the fix under test.
	require.False(t, raft.IsEmptySnap(snap))
	err = storage.ApplySnapshot(snap)
	require.NoError(t, err)

	// Verify storage advanced to the snapshot index.
	lastIdx, err = storage.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(5), lastIdx)

	// Append entries after the snapshot — this must not panic.
	require.NotPanics(t, func() {
		err = storage.Append(postSnapEntries)
	})
	require.NoError(t, err)

	// Verify final state.
	lastIdx, err = storage.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(7), lastIdx)
}

// TestMsgAppGuardRejectsStaleFollower verifies the MsgApp guard in
// grpcTransport.Send: when a follower has fresh MemoryStorage with
// lastIndex=3 (3 ConfChange entries from StartNode) and the leader sends
// MsgApp with prevLogIndex=4 (msg.Index=4), the guard returns
// Applied=false without stepping the message.
//
// This prevents the infinite rejection loop that occurs when the leader's
// progress tracker has a stale Match=4 from before the follower restarted,
// and the leader's log is compacted at the same boundary — making
// MaybeDecrTo unable to decrease Next below Match+1.
func TestMsgAppGuardRejectsStaleFollower(t *testing.T) {
	storage := raft.NewMemoryStorage()

	// Simulate a fresh follower: 3 ConfChange entries from StartNode.
	require.NoError(t, storage.Append([]raftpb.Entry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
	}))

	lastIdx, err := storage.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIdx)

	// Build a minimal transport with the storage set and a stub node.
	transport := &grpcTransport{}
	transport.setStorage(storage)
	transport.setNode(&stubNode{})

	ctx := context.Background()

	// MsgApp with msg.Index > lastIndex must be rejected.
	msgApp := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    2,
		To:      1,
		Index:   4, // prevLogIndex — beyond our lastIndex(3)
		LogTerm: 8,
		Entries: []raftpb.Entry{{Index: 5, Term: 8}},
	}
	data, err := msgApp.Marshal()
	require.NoError(t, err)

	resp, err := transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.False(t, resp.Applied, "MsgApp with Index > lastIndex should be rejected")

	// MsgApp with msg.Index <= lastIndex should be accepted (stepped).
	msgAppOK := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    2,
		To:      1,
		Index:   3, // prevLogIndex — within our log
		LogTerm: 1,
	}
	data, err = msgAppOK.Marshal()
	require.NoError(t, err)

	resp, err = transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.True(t, resp.Applied, "MsgApp with Index <= lastIndex should be accepted")

	// MsgHeartbeat with Commit > lastIndex is clamped (not rejected) so the
	// raft node can generate a proper MsgHeartbeatResp, keeping the peer
	// active in the leader's progress tracker.
	msgHB := raftpb.Message{
		Type:   raftpb.MsgHeartbeat,
		From:   2,
		To:     1,
		Commit: 5, // beyond our lastIndex(3), will be clamped to 3
	}
	data, err = msgHB.Marshal()
	require.NoError(t, err)

	resp, err = transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.True(t, resp.Applied, "MsgHeartbeat with Commit > lastIndex should be clamped and accepted")

	// MsgHeartbeat with Commit <= lastIndex should be accepted.
	msgHBOK := raftpb.Message{
		Type:   raftpb.MsgHeartbeat,
		From:   2,
		To:     1,
		Commit: 3,
	}
	data, err = msgHBOK.Marshal()
	require.NoError(t, err)

	resp, err = transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.True(t, resp.Applied, "MsgHeartbeat with Commit <= lastIndex should be accepted")
}

// TestFollowerCatchesUpAfterCompaction reproduces the MsgApp rejection
// deadlock that occurs when the leader's log is compacted exactly at the
// follower's stale Match index.
//
// Scenario:
//  1. Start a 3-node cluster, wait for leader election.
//  2. Stop one follower and wait for compaction on the remaining nodes.
//     At this point the leader's first available log index is beyond
//     what the stopped follower has.
//  3. Restart the follower. The leader's progress tracker still has the
//     stale Match from before the restart, and its log is compacted at
//     that boundary. Without the MsgApp guard + ReportUnreachable fix,
//     the leader sends MsgApp that the follower rejects, but
//     MaybeDecrTo can't decrease Next below Match+1 — deadlock.
//  4. With the fix, the follower rejects the MsgApp via the transport
//     guard, the leader marks it unreachable, and eventually sends a
//     snapshot to catch it up.
func TestFollowerCatchesUpAfterCompaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	_, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Stop a follower and wait for compaction so the leader's log no
	// longer contains the entries the follower needs.
	laggingNode := followers[0]
	laggingNode.Stop()
	laggingNode.WaitForStopped(t, 5*time.Second)
	t.Logf("stopped follower %d, waiting for compaction", laggingNode.config.ID)

	select {
	case <-time.After(12 * time.Second):
	case <-ctx.Done():
		t.Fatal("timed out waiting for compaction")
	}

	// Drain the stale follower signal from initial startup.
	select {
	case <-laggingNode.follower:
	default:
	}

	// Restart. Without the fix, this would deadlock in an infinite
	// MsgApp rejection loop and never become a follower.
	t.Logf("restarting follower %d after compaction", laggingNode.config.ID)
	laggingNode.Start(t, ctx)
	laggingNode.WaitForFollower(t, 30*time.Second)
	t.Logf("follower %d caught up after compaction — no deadlock", laggingNode.config.ID)
}

// stubNode is a minimal raft.Node implementation used by unit tests that
// only exercise the grpcTransport.Send guard (which returns before
// calling node.Step for guarded messages). For messages that pass the
// guard, Step returns nil to simulate a successful step.
type stubNode struct{}

func (s *stubNode) Tick()                                                       {}
func (s *stubNode) Campaign(context.Context) error                              { return nil }
func (s *stubNode) Propose(context.Context, []byte) error                       { return nil }
func (s *stubNode) ProposeConfChange(context.Context, raftpb.ConfChangeI) error { return nil }
func (s *stubNode) Step(context.Context, raftpb.Message) error                  { return nil }
func (s *stubNode) Ready() <-chan raft.Ready                                    { return nil }
func (s *stubNode) Advance()                                                    {}
func (s *stubNode) ApplyConfChange(raftpb.ConfChangeI) *raftpb.ConfState        { return nil }
func (s *stubNode) TransferLeadership(context.Context, uint64, uint64)          {}
func (s *stubNode) ForgetLeader(context.Context) error                          { return nil }
func (s *stubNode) ReadIndex(context.Context, []byte) error                     { return nil }
func (s *stubNode) Status() raft.Status                                         { return raft.Status{} }
func (s *stubNode) ReportUnreachable(uint64)                                    {}
func (s *stubNode) ReportSnapshot(uint64, raft.SnapshotStatus)                  {}
func (s *stubNode) Stop()                                                       {}

// TestFreshLeaderSnapshotSend validates the initial snapshot seed added to
// runRaft. Without it, a fresh node that wins leadership before its first
// compaction cycle (10 s) panics with "need non-empty snapshot" when it tries
// to send a snapshot to a peer whose log is behind.
//
// Scenario:
//  1. Start a 3-node cluster and wait for leader election.
//  2. Stop the leader and one follower — only one node remains.
//  3. Wait for a compaction cycle so the surviving node's log is compacted.
//  4. Restart both stopped nodes. They start with fresh MemoryStorage.
//  5. The surviving node loses quorum, so one of the fresh nodes wins the
//     new election. As leader it must send a snapshot to a peer — without
//     the initial snapshot seed this panics.
func TestFreshLeaderSnapshotSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	// Wait for initial leader election.
	leader, followers := waitForAnyLeader(t, 30*time.Second, leaders...)
	t.Logf("initial leader: %d", leader.config.ID)

	// Stop the leader and one follower, leaving a single node running.
	leader.Stop()
	leader.WaitForStopped(t, 5*time.Second)
	followers[0].Stop()
	followers[0].WaitForStopped(t, 5*time.Second)
	t.Logf("stopped leader %d and follower %d", leader.config.ID, followers[0].config.ID)

	// Wait for a compaction cycle so the surviving node has compacted its log.
	t.Log("waiting for compaction on surviving node (~12 s)...")
	select {
	case <-time.After(12 * time.Second):
	case <-ctx.Done():
		t.Fatal("timed out waiting for compaction")
	}

	// Drain stale follower signals.
	for _, l := range []*testLeader{leader, followers[0]} {
		select {
		case <-l.follower:
		default:
		}
	}

	// Restart both nodes. They have fresh MemoryStorage (no snapshot).
	// One of them will likely win the next election before its compaction
	// timer fires. Without the initial snapshot seed, the new leader panics
	// when it tries to send a snapshot to bring a peer up to date.
	t.Log("restarting both nodes...")
	leader.Start(t, ctx)
	followers[0].Start(t, ctx)

	// Wait for a new leader to emerge — if we get here without a panic the
	// initial snapshot seed is working.
	newLeader, _ := waitForAnyLeader(t, 30*time.Second, leaders...)
	t.Logf("new leader %d elected without panic", newLeader.config.ID)
}

func TestLocker(t *testing.T) {
	for name, tt := range map[string]struct {
		nodes int
	}{
		"13 node quorum": {
			nodes: 13,
		},
	} {
		t.Run(name, func(t *testing.T) {
			minQuorum := (tt.nodes + 1) / 2

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			leaders := setupLockTest(t, ctx, tt.nodes)

			defer func() {
				// ensure all leaders are stopped before the test ends so we don't
				// accidentally use the test logger after the test has finished
				// and panic
				cancel()
				for _, leader := range leaders {
					leader.WaitForStopped(t, 10*time.Second)
				}
			}()

			stopped := []*testLeader{}

			var currentLeader *testLeader
			currentLeaders := leaders
			// scale down til we get to the min followers
			for len(currentLeaders) != (minQuorum - 1) {
				currentLeader, currentLeaders = waitForAnyLeader(t, 30*time.Second, currentLeaders...)
				currentLeader.Stop()
				stopped = append(stopped, currentLeader)
				t.Log("killing leader", currentLeader.config.ID)
			}

			// Wait for all stopped leaders to fully exit before restarting.
			// Without this, the old goroutine may still be shutting down
			// when Start spawns a new one, and the cleanup's WaitForStopped
			// may consume the old goroutine's onStop signal — causing the
			// test to "complete" while the new goroutine is still running,
			// which panics on tst.Log after the test ends.
			for _, leader := range stopped {
				leader.WaitForStopped(t, 10*time.Second)
			}

			// restart and make sure that they become followers again
			for _, leader := range stopped {
				leader.Start(t, ctx)
			}

			_, _ = waitForAnyLeader(t, 30*time.Second, leaders...)
		})
	}
}

type testLeader struct {
	config LockConfiguration

	cancel context.CancelFunc

	leader   chan struct{}
	follower chan struct{}
	onStop   chan struct{}
	err      chan error

	stopped     atomic.Bool
	isLeader    atomic.Bool
	initialized atomic.Bool
}

func newTestLeader(config LockConfiguration) *testLeader {
	return &testLeader{
		config:   config,
		leader:   make(chan struct{}, 1),
		follower: make(chan struct{}, 1),
		err:      make(chan error, 1),
		onStop:   make(chan struct{}, 1),
	}
}

func (t *testLeader) OnStartedLeading(ctx context.Context) {
	t.initialized.Store(true)
	t.isLeader.Store(true)
	select {
	case t.leader <- struct{}{}:
	default:
	}
}

func (t *testLeader) OnStoppedLeading() {
	t.initialized.Store(true)
	t.isLeader.Store(false)

	select {
	case t.follower <- struct{}{}:
	default:
	}
}

func (t *testLeader) IsLeader() bool {
	return t.initialized.Load() && t.isLeader.Load()
}

func (t *testLeader) IsFollower() bool {
	return t.initialized.Load() && !t.isLeader.Load()
}

func (t *testLeader) HandleError(err error) {
	if err == nil {
		return
	}

	select {
	case t.err <- err:
	default:
	}
}

func (t *testLeader) IsStopped() bool {
	return t.stopped.Load()
}

func (t *testLeader) Start(tst *testing.T, ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	t.stopped.Store(false)
	t.initialized.Store(false)
	t.isLeader.Store(false)

	go func() {
		defer func() {
			// Use the cancel captured at goroutine-creation time, NOT t.cancel.
			// If a subsequent Start() call has already overwritten t.cancel,
			// calling t.cancel() here would cancel the new goroutine's context,
			// causing an immediate "context canceled" on the freshly-restarted
			// node before it has a chance to rejoin the cluster.
			cancel()
			select {
			case t.onStop <- struct{}{}:
			default:
			}
			t.stopped.Store(true)
		}()
		defer tst.Log("leader election goroutine exiting")

		t.HandleError(Run(ctx, t.config, &LeaderCallbacks{
			OnStartedLeading: t.OnStartedLeading,
			OnStoppedLeading: t.OnStoppedLeading,
		}))
	}()
}

func (t *testLeader) Stop() {
	// Cancel the context. The goroutine signals onStop and sets stopped=true
	// when Run() actually returns, so WaitForStopped blocks until the goroutine
	// has fully exited — guaranteeing it is safe to call Start() afterwards.
	t.cancel()
}

func (t *testLeader) WaitForLeader(tst *testing.T, timeout time.Duration) {
	tst.Helper()

	select {
	case <-t.leader:
	case err := <-t.err:
		tst.Fatalf("leader election failed: %v", err)
	case <-time.After(timeout):
		tst.Fatalf("timed out waiting for leader election to start")
	}
}

func (t *testLeader) WaitForFollower(tst *testing.T, timeout time.Duration) {
	tst.Helper()

	select {
	case <-t.follower:
	case err := <-t.err:
		tst.Fatalf("leader election failed: %v", err)
	case <-time.After(timeout):
		tst.Fatalf("timed out waiting for leader election to stop")
	}
}

func (t *testLeader) WaitForStopped(tst *testing.T, timeout time.Duration) {
	tst.Helper()

	select {
	case <-t.onStop:
	case err := <-t.err:
		tst.Fatalf("leader election failed: %v", err)
	case <-time.After(timeout):
		tst.Fatalf("timed out waiting for leader election to stop")
	}
}

func (t *testLeader) Error() error {
	select {
	case err := <-t.err:
		return err
	default:
		return nil
	}
}

func waitForAnyLeader(t *testing.T, timeout time.Duration, leaders ...*testLeader) (*testLeader, []*testLeader) {
	cases := make([]reflect.SelectCase, len(leaders))
	for i, leader := range leaders {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(leader.leader)}
	}
	timeoutCh := time.After(timeout)
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timeoutCh)})
	chosen, _, _ := reflect.Select(cases)
	if chosen == len(cases)-1 {
		t.Fatalf("timed out waiting for leader to be elected")
	}

	chosenLeader := leaders[chosen]

	followers := []*testLeader{}
	pending := []*testLeader{}
	for i, leader := range leaders {
		if chosen == i {
			continue
		}
		followers = append(followers, leader)
		if leader.IsFollower() {
			continue
		}
		pending = append(pending, leader)
	}

	if len(pending) == 0 {
		return chosenLeader, followers
	}

	followed := 0
	cases = make([]reflect.SelectCase, len(pending))
	for i, leader := range pending {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(leader.follower)}
	}
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timeoutCh)})

	for {
		chosen, _, _ := reflect.Select(cases)
		if chosen == len(cases)-1 {
			t.Fatalf("timed out waiting for followers to become active")
		}
		followed++
		if followed >= len(pending) {
			return chosenLeader, followers
		}
	}
}

func setupLockTest(t *testing.T, ctx context.Context, n int) []*testLeader {
	if n <= 0 {
		t.Fatalf("at least one lock configuration is required")
	}

	ports := testutil.FreePorts(t, n)

	leaders := []*testLeader{}
	nodes := []LockerNode{}
	for i, port := range ports {
		nodes = append(nodes, LockerNode{
			ID:      uint64(i + 1),
			Address: fmt.Sprintf("127.0.0.1:%d", port),
		})
	}

	ca, err := bootstrap.GenerateCA("unit", "Root CA", nil)
	if err != nil {
		t.Fatalf("error generating ca: %v", err)
	}
	certificate, err := ca.Sign("127.0.0.1")
	if err != nil {
		t.Fatalf("error generating certificate: %v", err)
	}

	configs := []LockConfiguration{}
	for i, node := range nodes {
		peers := []LockerNode{}
		peers = append(peers, nodes...)

		configs = append(configs, LockConfiguration{
			ID:                uint64(i + 1),
			Address:           node.Address,
			Peers:             peers,
			CA:                ca.Bytes(),
			PrivateKey:        certificate.PrivateKeyBytes(),
			Certificate:       certificate.Bytes(),
			ElectionTimeout:   1 * time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			Logger:            testLogger(t),
		})
	}

	for _, config := range configs {
		leader := newTestLeader(config)
		leaders = append(leaders, leader)

		leader.Start(t, ctx)
	}

	return leaders
}

func testLogger(t *testing.T) raft.Logger {
	return &raft.DefaultLogger{Logger: log.New(t.Output(), t.Name()+" ", log.LUTC)}
}
