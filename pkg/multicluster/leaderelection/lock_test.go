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
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/raft/v3"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
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

	ports := getFreePorts(t, n)

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

func getFreePorts(t *testing.T, n int) []int {
	ports := make([]int, 0, n)
	listeners := make([]net.Listener, 0, n)

	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("error getting free port: %v", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}

	for _, l := range listeners {
		l.Close()
	}

	return ports
}

func testLogger(t *testing.T) raft.Logger {
	return &raft.DefaultLogger{Logger: log.New(t.Output(), t.Name()+" ", log.LUTC)}
}
