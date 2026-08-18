// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// TestDrainNotificationsNoConcurrentMiss verifies that drainNotifications
// never drops a notification that fires while fn is already executing.
//
// The scenario that previously caused a missed engage:
//
//  1. Goroutine is waiting on channel ch_A.
//  2. notify() fires → ch_A closes, ch_B created.
//  3. Goroutine wakes, snapshots ch = ch_B, calls fn (slow).
//  4. A second notify() fires while fn is running → ch_B closes, ch_C created.
//  5. fn returns.
//  6. Old code: goroutine calls channel() → gets ch_C (open), waits forever.
//     New code: ch_C != ch_B → goroutine detects the missed notification and
//     calls fn again before waiting on ch_C.
//
// synctest.Wait() is used to advance all goroutines to their next durable
// blocking point, making the steps above fully deterministic without sleeps.
func TestDrainNotificationsNoConcurrentMiss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newRestartBroadcaster()

		var count atomic.Int32

		// gate controls when fn returns, simulating a slow doEngage.
		gate := make(chan struct{})
		fn := func(_ context.Context) {
			count.Add(1)
			<-gate
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go drainNotifications(ctx, b, fn)

		// Ensure the goroutine has started and is blocked in the select before
		// we fire any notification. Without this, the goroutine might start
		// AFTER notify() replaces the channel and miss the first notification.
		synctest.Wait()

		// ── Step 1: first notify ─────────────────────────────────────────────
		// The goroutine wakes, snapshots the new channel reference (ch_B), and
		// calls fn. fn increments count then blocks on <-gate.
		b.notify()
		synctest.Wait() // goroutine is now blocked inside fn on <-gate

		if count.Load() != 1 {
			t.Fatalf("after first notify: expected count=1, got %d", count.Load())
		}

		// ── Step 2: second notify while fn is still running ──────────────────
		// The broadcaster closes ch_B and creates ch_C. Because fn has not
		// returned yet, the goroutine cannot observe this change.
		b.notify()

		// ── Step 3: release fn ───────────────────────────────────────────────
		// fn returns. The drain check sees ch_C != ch_B and calls fn again.
		// fn increments count then blocks on <-gate again.
		gate <- struct{}{}
		synctest.Wait() // goroutine is blocked inside the second fn call

		if count.Load() != 2 {
			t.Fatalf("after second notify: expected count=2, got %d (notification was dropped)", count.Load())
		}

		// ── Step 4: release the second fn and shut down ──────────────────────
		gate <- struct{}{}
		cancel()
		synctest.Wait() // goroutine exits via ctx.Done()
	})
}

// TestDrainNotificationsSingleNotify is a basic sanity check: a single notify
// calls fn exactly once and the goroutine then waits for the next notification.
func TestDrainNotificationsSingleNotify(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newRestartBroadcaster()

		var count atomic.Int32
		fn := func(_ context.Context) { count.Add(1) }

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go drainNotifications(ctx, b, fn)
		synctest.Wait() // goroutine subscribed and blocking in select

		b.notify()
		synctest.Wait() // fn ran and goroutine is back waiting for next notify

		if count.Load() != 1 {
			t.Fatalf("expected count=1, got %d", count.Load())
		}

		cancel()
		synctest.Wait()
	})
}

// TestDrainNotificationsThreeRapidNotifies verifies that three back-to-back
// notify() calls issued while fn is blocked result in fn being called at
// least twice: once for the first notification and at least once more for the
// concurrent ones (which collapse into a single drain pass since each
// subsequent notify() replaces the previous replacement channel).
func TestDrainNotificationsThreeRapidNotifies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newRestartBroadcaster()

		var count atomic.Int32
		gate := make(chan struct{})
		fn := func(_ context.Context) {
			count.Add(1)
			<-gate
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go drainNotifications(ctx, b, fn)
		synctest.Wait() // goroutine subscribed

		// First notify starts fn.
		b.notify()
		synctest.Wait() // goroutine blocked on <-gate in fn

		if count.Load() != 1 {
			t.Fatalf("after first notify: expected count=1, got %d", count.Load())
		}

		// Two more notifies while fn is blocked. They replace the channel
		// twice; the third notify's channel is what the drain check will see.
		// A single drain pass is sufficient to call fn once more.
		b.notify()
		b.notify()

		// Release first fn; drain detects missed notifications and re-runs fn.
		gate <- struct{}{}
		synctest.Wait() // goroutine blocked on <-gate in drain-pass fn call

		if count.Load() < 2 {
			t.Fatalf("after two concurrent notifies: expected count>=2, got %d", count.Load())
		}

		// Release remaining fn calls until the goroutine is back in the select.
		for {
			select {
			case gate <- struct{}{}:
				synctest.Wait()
			default:
				// goroutine is not blocked on gate — drain complete
				cancel()
				synctest.Wait()
				return
			}
		}
	})
}
