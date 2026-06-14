// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package rpkprofile keeps the rpk configuration inside a running broker pod
// in sync with the operator-managed ConfigMap it was originally rendered
// from (K8S-755).
//
// In both the v1 (Cluster CRD) and v2 (Redpanda CRD / chart) operators, the
// pod-local rpk configuration is rendered once at pod startup by an init
// container and never refreshed, while seed-server changes are deliberately
// excluded from the restart-triggering configuration hash. The result is
// that after any node join/leave — most visibly a NodePool blue/green
// migration — in-pod rpk invocations see a stale broker list until the pod
// restarts. The kubelet keeps ConfigMap volume mounts up to date in running
// pods, so watching the mount and re-rendering on change closes the gap
// without a restart.
package rpkprofile

import (
	"context"
	"fmt"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

const (
	// DefaultResyncPeriod is the fallback re-render cadence. fsnotify events
	// from the kubelet's atomic ConfigMap symlink swap are the fast path;
	// the ticker guarantees convergence if an event is missed.
	DefaultResyncPeriod = 10 * time.Minute

	// debounceWindow coalesces the burst of fsnotify events produced by a
	// single atomic ConfigMap update (the kubelet rewrites ..data and
	// several symlinks).
	debounceWindow = time.Second
)

// WatchAndRender renders once immediately, then re-renders whenever the
// watched directory changes (debounced) or the resync ticker fires, until
// ctx is cancelled. Render errors are logged and retried on the next
// trigger — never fatal, since a transiently half-propagated ConfigMap
// mount must not crashloop the broker pod. It returns nil on context
// cancellation, so it slots into a controller-runtime manager.Runnable.
func WatchAndRender(ctx context.Context, log logr.Logger, sourceDir string, resync time.Duration, render func(context.Context) error) error {
	if resync <= 0 {
		resync = DefaultResyncPeriod
	}

	doRender := func() {
		if err := render(ctx); err != nil {
			log.Error(err, "failed to render rpk configuration; will retry on next change or resync")
			return
		}
		log.V(1).Info("rendered rpk configuration", "source", sourceDir)
	}

	doRender()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating filesystem watcher: %w", err)
	}
	defer watcher.Close()

	// Watch the directory: the kubelet updates ConfigMap volumes atomically
	// by swapping the ..data symlink, which surfaces as Create/Remove events
	// on entries within the directory rather than Write events on the files.
	if err := watcher.Add(sourceDir); err != nil {
		return fmt.Errorf("watching %q: %w", sourceDir, err)
	}

	ticker := time.NewTicker(resync)
	defer ticker.Stop()

	var debounce *time.Timer
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()
	var debounceC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			doRender()
		case <-debounceC:
			debounceC = nil
			doRender()
		case _, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if debounce == nil {
				debounce = time.NewTimer(debounceWindow)
			} else {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(debounceWindow)
			}
			debounceC = debounce.C
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Error(err, "filesystem watcher error")
		}
	}
}

// Watcher adapts WatchAndRender to the controller-runtime manager.Runnable
// interface so the v2 sidecar can register it alongside its other
// components.
type Watcher struct {
	Log       logr.Logger
	SourceDir string
	Resync    time.Duration
	Render    func(context.Context) error
}

// Start implements manager.Runnable.
func (w *Watcher) Start(ctx context.Context) error {
	return WatchAndRender(ctx, w.Log, w.SourceDir, w.Resync, w.Render)
}
