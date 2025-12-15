// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"crypto/rand"
	"math/big"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestLocking(t *testing.T) {
	for name, tt := range map[string]struct {
		config LockConfiguration
	}{
		"single lock": {
			config: LockConfiguration{
				Name: "single",
				Configs: []ClusterNamespaceConfig{{
					Namespace: "single",
				}},
			},
		},
		"multi lock": {
			config: LockConfiguration{
				Name: "multi",
				Configs: []ClusterNamespaceConfig{{
					Namespace: "namespace-1",
				}, {
					Namespace: "namespace-2",
				}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Run("controlled", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()

				for i := range tt.config.Configs {
					tt.config.Configs[i].Config = runTestServer(t)
				}

				leaderOne := setupLockTest(t, ctx, "one", tt.config)
				leaderOne.WaitForLeader(t, 15*time.Second)

				leaderTwo := setupLockTest(t, ctx, "two", tt.config)
				leaderTwo.WaitForFollower(t, 15*time.Second)

				leaderOne.Stop()

				leaderTwo.WaitForLeader(t, 15*time.Second)

				leaderTwo.Stop()

				leaderOne.WaitForStopped(t, 15*time.Second)
				leaderTwo.WaitForStopped(t, 15*time.Second)

				if err := leaderOne.Error(); err != nil {
					t.Fatalf("leader one encountered error: %v", err)
				}
				if err := leaderTwo.Error(); err != nil {
					t.Fatalf("leader two encountered error: %v", err)
				}
			})
			t.Run("random", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				n, err := rand.Int(rand.Reader, big.NewInt(5))
				if err != nil {
					t.Fatal(err)
				}

				tt.config.Name = tt.config.Name + "-random"
				for i := range tt.config.Configs {
					tt.config.Configs[i].Config = runTestServer(t)
				}

				leaders := []*testLeader{}
				for i := 0; i < int(n.Int64())+5; i++ {
					leaders = append(leaders, setupLockTest(t, ctx, strconv.Itoa(i), tt.config))
				}

				var currentLeader *testLeader
				currentLeaders := leaders
				for len(currentLeaders) > 0 {
					currentLeader, currentLeaders = waitForAnyLeader(t, 15*time.Second, currentLeaders...)
					currentLeader.Stop()
				}

				for i, leader := range leaders {
					leader.WaitForStopped(t, 15*time.Second)
					if err := leader.Error(); err != nil {
						t.Fatalf("leader %d encountered error: %v", i, leader)
					}
				}
			})
		})
	}
}

func ensureNamespace(t *testing.T, cfg *rest.Config, namespace string) {
	t.Helper()

	k8sClient, err := corev1client.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create kube client: %v", err)
	}

	_, err = k8sClient.Namespaces().Get(t.Context(), namespace, metav1.GetOptions{})
	if err == nil {
		return
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("failed to get namespace %q: %v", t.Name(), err)
	}

	_, err = k8sClient.Namespaces().Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace %q: %v", namespace, err)
	}
}

func runTestServer(t *testing.T) *rest.Config {
	t.Helper()

	var environment envtest.Environment

	t.Cleanup(func() {
		err := environment.Stop()
		if err != nil {
			t.Fatalf("failed to stop test environment: %v", err)
		}
	})

	cfg, err := environment.Start()
	if err != nil {
		t.Fatalf("failed to start test environment: %v", err)
	}

	return cfg
}

type testLeader struct {
	id string

	leader   chan struct{}
	follower chan struct{}
	onStop   chan struct{}
	err      chan error

	cancel      context.CancelFunc
	stopped     atomic.Bool
	isLeader    atomic.Bool
	initialized atomic.Bool
}

func newTestLeader(id string, cancel context.CancelFunc) *testLeader {
	return &testLeader{
		id:       id,
		leader:   make(chan struct{}, 1),
		follower: make(chan struct{}, 1),
		err:      make(chan error, 1),
		onStop:   make(chan struct{}, 1),
		cancel:   cancel,
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

func setupLockTest(t *testing.T, ctx context.Context, id string, config LockConfiguration) *testLeader {
	config.ID = id
	config.RenewDeadline = 1 * time.Second
	config.LeaseDuration = 2 * time.Second
	config.RetryPeriod = 10 * time.Millisecond
	for _, config := range config.Configs {
		ensureNamespace(t, config.Config, config.Namespace)
	}

	ctx, cancel := context.WithCancel(ctx)

	leader := newTestLeader(id, cancel)
	callbacks := &LeaderCallbacks{
		OnStartedLeading: leader.OnStartedLeading,
		OnStoppedLeading: leader.OnStoppedLeading,
	}

	go func() {
		defer leader.Stop()
		defer t.Log("leader election goroutine exiting")

		leader.HandleError(Run(ctx, config, callbacks))
	}()

	return leader
}
