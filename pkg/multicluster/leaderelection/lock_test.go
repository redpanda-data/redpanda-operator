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

// TestFollowerRejoinsAfterRestart validates that a follower that restarts
// with fresh MemoryStorage can rejoin the cluster via normal log catch-up.
// The leader retains all log entries (no compaction) and catches the
// follower up via MsgApp.
func TestFollowerRejoinsAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	leaders := setupLockTest(t, ctx, 3)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	_, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Stop one follower to simulate a pod restart.
	laggingNode := followers[0]
	laggingNode.Stop()
	laggingNode.WaitForStopped(t, 5*time.Second)
	t.Logf("stopped follower %d", laggingNode.config.ID)

	// Drain the stale follower signal from initial startup.
	select {
	case <-laggingNode.follower:
	default:
	}

	// Restart. The fresh node has lastIndex=N (ConfChange entries) while
	// the leader's log has progressed. The leader catches it up via MsgApp.
	t.Logf("restarting follower %d", laggingNode.config.ID)
	laggingNode.Start(t, ctx)
	laggingNode.WaitForFollower(t, 30*time.Second)
	t.Logf("follower %d successfully rejoined the cluster", laggingNode.config.ID)
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

// TestHeartbeatCommitClamping verifies that the MsgHeartbeat guard in
// grpcTransport.Send clamps Commit to lastIndex, preventing the raft
// library from panicking in commitTo when a fresh follower's lastIndex
// is behind the leader's Commit.
func TestHeartbeatCommitClamping(t *testing.T) {
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

	// MsgHeartbeat with Commit > lastIndex returns Applied=false so the
	// leader can send a snapshot to catch up the follower.
	msgHB := raftpb.Message{
		Type:   raftpb.MsgHeartbeat,
		From:   2,
		To:     1,
		Commit: 5, // beyond our lastIndex(3)
	}
	data, err := msgHB.Marshal()
	require.NoError(t, err)

	resp, err := transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.False(t, resp.Applied, "MsgHeartbeat with Commit > lastIndex should be rejected")

	// MsgHeartbeat with Commit <= lastIndex should be accepted unchanged.
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

// TestMsgAppRejectedWhenBeyondLog verifies that the follower's Send handler
// returns Applied=false when it receives a MsgApp whose prev log index is
// beyond the follower's log. This prevents the infinite rejection loop
// caused by the leader's stale progress tracker (match+1 floor in
// MaybeDecrTo) and signals the leader to send a snapshot instead.
func TestMsgAppRejectedWhenBeyondLog(t *testing.T) {
	storage := raft.NewMemoryStorage()

	// Simulate a fresh follower: 3 ConfChange entries from StartNode.
	require.NoError(t, storage.Append([]raftpb.Entry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
	}))

	transport := &grpcTransport{}
	transport.setStorage(storage)
	transport.setNode(&stubNode{})

	ctx := context.Background()

	// MsgApp with Index > lastIndex(3): follower can't match, returns Applied=false.
	msgApp := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    2,
		To:      1,
		Index:   5, // beyond our log
		LogTerm: 2,
	}
	data, err := msgApp.Marshal()
	require.NoError(t, err)

	resp, err := transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.False(t, resp.Applied, "MsgApp with Index > lastIndex should be rejected")

	// MsgApp with Index <= lastIndex: accepted normally.
	msgAppOK := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    2,
		To:      1,
		Index:   3,
		LogTerm: 1,
	}
	data, err = msgAppOK.Marshal()
	require.NoError(t, err)

	resp, err = transport.Send(ctx, &transportv1.SendRequest{Payload: data})
	require.NoError(t, err)
	require.True(t, resp.Applied, "MsgApp with Index <= lastIndex should be accepted")
}

// TestSnapshotRecoveryAfterRestart validates the snapshot recovery path.
// After a follower restarts with empty storage, the leader's progress tracker
// has a stale match that prevents normal MsgApp catch-up (the match+1 floor
// in MaybeDecrTo). The fix: the follower returns Applied=false for MsgApp it
// can't match, the leader sends a snapshot, the follower applies it and its
// committed index catches up to the leader's.
func TestSnapshotRecoveryAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)

	// Attach TestHooks to all nodes so we can read committed indices.
	hooks := make([]*TestHooks, 3)
	for i := range hooks {
		hooks[i] = &TestHooks{}
	}

	leaders := setupLockTestWithHooks(t, ctx, 3, hooks)

	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	leader, followers := waitForAnyLeader(t, 30*time.Second, leaders...)

	// Wait for the leader's committed index to advance past the initial
	// ConfChange entries. The leader writes a no-op on election; once
	// committed, followers must replicate to reach this index.
	var leaderIdx int
	for i, l := range leaders {
		if l == leader {
			leaderIdx = i
			break
		}
	}
	confChangeCount := uint64(len(leaders)) // one ConfChange per peer
	require.Eventually(t, func() bool {
		ci := hooks[leaderIdx].CommittedIndex()
		return ci > confChangeCount
	}, 10*time.Second, 50*time.Millisecond,
		"leader committed index never advanced past ConfChange entries")
	leaderCommit := hooks[leaderIdx].CommittedIndex()
	t.Logf("leader %d committed index: %d (past %d ConfChanges)", leader.config.ID, leaderCommit, confChangeCount)

	lagging := followers[0]
	lagging.Stop()
	lagging.WaitForStopped(t, 5*time.Second)

	select {
	case <-lagging.follower:
	default:
	}

	t.Logf("restarting follower %d", lagging.config.ID)
	lagging.Start(t, ctx)

	// WaitForFollower only confirms the node initialized as a follower — it
	// does NOT mean log replication succeeded. The real assertion is that the
	// follower's committed index catches up to the leader's. Without the
	// snapshot fix, committed stays at 3 (ConfChanges only) because the
	// leader can't replicate past its stale match.
	lagging.WaitForFollower(t, 30*time.Second)

	// The follower's committed index must reach the leader's — this proves
	// the snapshot was applied and replication succeeded. Without the fix,
	// committed stays at the initial ConfChange count because the leader
	// can't replicate past its stale match.
	var laggingIdx int
	for i, l := range leaders {
		if l == lagging {
			laggingIdx = i
			break
		}
	}
	require.Eventually(t, func() bool {
		ci := hooks[laggingIdx].CommittedIndex()
		t.Logf("follower %d committed index: %d (want >= %d)", lagging.config.ID, ci, leaderCommit)
		return ci >= leaderCommit
	}, 10*time.Second, 100*time.Millisecond,
		"follower committed index never caught up to leader")
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
	return setupLockTestWithHooks(t, ctx, n, nil)
}

func setupLockTestWithHooks(t *testing.T, ctx context.Context, n int, hooks []*TestHooks) []*testLeader {
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

		cfg := LockConfiguration{
			ID:                uint64(i + 1),
			Address:           node.Address,
			Peers:             peers,
			CA:                ca.Bytes(),
			PrivateKey:        certificate.PrivateKeyBytes(),
			Certificate:       certificate.Bytes(),
			ElectionTimeout:   1 * time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			Logger:            testLogger(t),
		}
		if hooks != nil && i < len(hooks) {
			cfg.TestHooks = hooks[i]
		}
		configs = append(configs, cfg)
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
