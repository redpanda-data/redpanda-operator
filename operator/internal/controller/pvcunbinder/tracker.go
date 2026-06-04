// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pvcunbinder

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// SettledVerifier is an optional callback used by [InFlightTracker.IsHeld]
// to confirm whether the tracker should drop an entry that has aged
// past the TTL. The tracker calls it for each PVC name in the entry;
// if any returns false (still missing from the API or stuck
// Terminating), the entry is kept and the TTL window is reset.
//
// This bridges the rare case where the cache and the API both fail to
// settle a deleted PVC within the TTL — e.g., the PVC has a finalizer
// holding it in Terminating. Without this check, the TTL would drop
// the tracker entry and the next reconcile would unbind a sibling pod
// despite the original deletion still being in flight.
type SettledVerifier func(ctx context.Context, name string) (settled bool, err error)

// DefaultTrackerTTL is the upper bound on how long a tracker entry can
// hold up subsequent reconciles for the same cluster. It's a safety net:
// in practice an entry is expunged the moment the cache observes that
// every deleted PVC has been recreated with a new UID. The TTL only
// matters if something unusual keeps that signal from arriving (e.g.,
// PVC stuck in deletion, controller restart leaving stale state).
const DefaultTrackerTTL = time.Minute

// InFlightTracker bridges the informer cache-staleness window between
// when a reconcile deletes a Pod's PVCs and when the controller-runtime
// cache observes the recreated PVCs.
//
// Why this is needed: the Gate 3 cache check ("any PVC has empty
// volumeName") has two failure modes when read through a stale cache:
//
//  1. Cache hasn't observed the delete yet → old PVC still visible with
//     volumeName set → check returns false even though an unbind is in
//     flight.
//  2. Cache has observed the delete but not the StatefulSet's
//     recreation → the PVC is missing from the List entirely → loop
//     finds nothing with empty volumeName → check returns false.
//
// The tracker resolves both by remembering, at deletion time, the UID
// of each PVC the reconcile just deleted. A subsequent reconcile's
// IsHeld call defers as long as either the old UID is still visible
// (case 1) OR a PVC with that name is missing from the cache (case 2).
// Once every deleted PVC has been observably recreated (same name,
// different UID), the entry is expunged and reconciles can proceed.
type InFlightTracker struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]markedEntry
}

type markedEntry struct {
	markedAt time.Time
	// deletedByName maps each PVC name we deleted to the UID we
	// observed at deletion time. The tracker is "settled" only when,
	// for every name in this map, the cache shows a PVC with that name
	// AND a different UID.
	deletedByName map[string]types.UID
}

// NewInFlightTracker returns a tracker with the given TTL. Use
// DefaultTrackerTTL unless a test needs a shorter duration.
func NewInFlightTracker(ttl time.Duration) *InFlightTracker {
	if ttl <= 0 {
		ttl = DefaultTrackerTTL
	}
	return &InFlightTracker{ttl: ttl, entries: make(map[string]markedEntry)}
}

// IsHeld returns true if a Mark for `key` is still in effect — meaning
// the cache has not yet observed every deleted PVC being replaced with
// a new UID. `visible` is a snapshot of the cluster's current PVCs
// (name → UID) from the cache.
//
// Auto-expires entries past the TTL and auto-expunges entries that the
// cache shows have settled. Safe on a nil receiver (returns false).
func (t *InFlightTracker) IsHeld(key string, visible map[string]types.UID) bool {
	held, _ := t.IsHeldWithVerify(context.Background(), key, visible, nil)
	return held
}

// IsHeldWithVerify is the TTL-fallback-aware variant of [IsHeld]. When
// the entry's TTL has expired, instead of unconditionally dropping it,
// the tracker calls `verify` on each PVC name. If any returns false
// (still missing from the API or stuck Terminating), the entry's
// `markedAt` is reset to now and the call returns held=true. This
// closes the rare window where the informer cache and the API both
// fail to settle a deleted PVC within the TTL (e.g., a finalizer
// holds it in Terminating).
//
// Verify is invoked at most once per name per IsHeldWithVerify call,
// and only on TTL expiry — the cache-based settled path remains the
// hot, allocation-free check. Pass `verify == nil` (or use [IsHeld])
// to keep the old TTL-drop behavior.
//
// Safe on a nil receiver (returns held=false, err=nil).
func (t *InFlightTracker) IsHeldWithVerify(ctx context.Context, key string, visible map[string]types.UID, verify SettledVerifier) (bool, error) {
	if t == nil {
		return false, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok {
		return false, nil
	}
	// TTL safety net. Before dropping the entry, give the caller a
	// chance to re-affirm via a live API check.
	if time.Since(e.markedAt) >= t.ttl {
		if verify == nil {
			delete(t.entries, key)
			return false, nil
		}
		for name := range e.deletedByName {
			settled, err := verify(ctx, name)
			if err != nil {
				return false, err
			}
			if !settled {
				e.markedAt = time.Now()
				t.entries[key] = e
				return true, nil
			}
		}
		delete(t.entries, key)
		return false, nil
	}
	// Settled iff every deleted PVC name now resolves to a different
	// UID in the cache. If the name is missing entirely, we're in the
	// "deleted but not yet recreated" window — defer.
	for name, oldUID := range e.deletedByName {
		currentUID, exists := visible[name]
		if !exists || currentUID == oldUID {
			return true, nil
		}
	}
	// Cache has caught up past every delete + recreate. Expunge.
	delete(t.entries, key)
	return false, nil
}

// Mark records that a reconcile has just deleted PVCs from the
// cluster. `deletedByName` maps each deleted PVC's name to the UID it
// had at deletion time. Subsequent IsHeld calls for the same key will
// defer until the cache shows new UIDs for every name in the map.
// Safe on a nil receiver (no-op).
//
// Mark also opportunistically sweeps any TTL-expired entries: it's the
// only operation that grows the map, so cleaning at growth time keeps
// the size bounded without adding overhead to the per-reconcile IsHeld
// path. Without this, entries for clusters that get deleted (or stop
// reconciling for any reason) after Mark would linger until process
// restart.
func (t *InFlightTracker) Mark(key string, deletedByName map[string]types.UID) {
	if t == nil {
		return
	}
	if len(deletedByName) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for k, e := range t.entries {
		if now.Sub(e.markedAt) >= t.ttl {
			delete(t.entries, k)
		}
	}
	t.entries[key] = markedEntry{
		markedAt:      now,
		deletedByName: deletedByName,
	}
}
