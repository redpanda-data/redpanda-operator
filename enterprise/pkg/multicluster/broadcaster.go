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
	"sync"
)

// restartBroadcaster delivers notifications to multiple concurrent consumers
// with two guarantees:
//
//  1. Fanout: every notify() wakes all goroutines currently waiting, not just
//     one (close-and-replace gives channel-close broadcast semantics).
//
//  2. No missed notifications: if notify() fires while a consumer's fn is
//     executing, fn will be called again after it returns.
type restartBroadcaster struct {
	mu sync.Mutex
	ch chan struct{}
}

func newRestartBroadcaster() *restartBroadcaster {
	return &restartBroadcaster{ch: make(chan struct{})}
}

// notify wakes all goroutines currently blocked in drainNotifications and
// replaces the channel so consumers can detect notifications that fired while
// their fn was executing.
func (b *restartBroadcaster) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.ch)
	b.ch = make(chan struct{})
}

// channel returns the current channel under the lock so callers get a
// consistent snapshot.
func (b *restartBroadcaster) channel() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

// drainNotifications runs fn each time b fires. It handles two concerns:
//
//   - Fanout: because notify() closes the channel, all goroutines running
//     drainNotifications for the same broadcaster wake simultaneously.
//
//   - No missed notifications: the channel reference is snapshotted before fn
//     runs. After fn returns, if the reference changed then at least one
//     notify() fired during fn, so fn is called again immediately. Multiple
//     concurrent notifies during a single fn execution collapse into one extra
//     fn call.
func drainNotifications(ctx context.Context, b *restartBroadcaster, fn func(context.Context)) {
	ch := b.channel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			// Snapshot the channel before fn runs. Any notify() during fn
			// replaces the channel, making the reference stale.
			ch = b.channel()
			fn(ctx)
			// Drain: if the channel changed while fn was running, a
			// notification was missed — call fn again and repeat.
			for next := b.channel(); next != ch; next = b.channel() {
				ch = next
				fn(ctx)
			}
		}
	}
}
