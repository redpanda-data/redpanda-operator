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
			defer cancel()

			leaders := setupLockTest(t, ctx, tt.nodes)

			stopped := []*testLeader{}

			var currentLeader *testLeader
			currentLeaders := leaders
			// scale down til we get to the min followers
			for len(currentLeaders) != (minQuorum - 1) {
				currentLeader, currentLeaders = waitForAnyLeader(t, 15*time.Second, currentLeaders...)
				currentLeader.Stop()
				stopped = append(stopped, currentLeader)
				t.Log("killing leader", currentLeader.config.ID)
			}

			// restart and make sure that they become followers again
			for _, leader := range stopped {
				leader.Start(t, ctx)
			}

			_, _ = waitForAnyLeader(t, 15*time.Second, leaders...)
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
		defer t.Stop()
		defer tst.Log("leader election goroutine exiting")

		t.HandleError(Run(ctx, t.config, &LeaderCallbacks{
			OnStartedLeading: t.OnStartedLeading,
			OnStoppedLeading: t.OnStoppedLeading,
		}))
	}()
}

func (t *testLeader) Stop() {
	t.cancel()

	select {
	case t.onStop <- struct{}{}:
	default:
	}
	t.stopped.Store(true)
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
