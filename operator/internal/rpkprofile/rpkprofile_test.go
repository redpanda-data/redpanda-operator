// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package rpkprofile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// TestWatchAndRenderTriggersOnChange covers the shared loop: an immediate
// initial render, a re-render on directory change, and clean exit on ctx
// cancellation.
func TestWatchAndRenderTriggersOnChange(t *testing.T) {
	dir := t.TempDir()
	var renders int32

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		// NB: a short resync, not time.Hour — the initial render happens
		// before the fsnotify watch is registered, so a write landing in
		// that gap is only picked up by the resync ticker (the same
		// missed-event backstop production relies on).
		done <- WatchAndRender(ctx, logr.Discard(), dir, 500*time.Millisecond, func(context.Context) error {
			atomic.AddInt32(&renders, 1)
			return nil
		})
	}()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&renders) >= 1 },
		5*time.Second, 10*time.Millisecond, "initial render did not happen")

	// A directory change (kubelet symlink swap analogue) re-renders.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644))
	require.Eventually(t, func() bool { return atomic.LoadInt32(&renders) >= 2 },
		10*time.Second, 50*time.Millisecond, "change did not trigger a re-render")

	cancel()
	require.NoError(t, <-done)
}

// TestWatchAndRenderSurvivesRenderErrors verifies render errors never stop
// the loop — a half-propagated mount must not crashloop the broker pod.
func TestWatchAndRenderSurvivesRenderErrors(t *testing.T) {
	dir := t.TempDir()
	var renders int32

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- WatchAndRender(ctx, logr.Discard(), dir, 50*time.Millisecond, func(context.Context) error {
			if atomic.AddInt32(&renders, 1) == 1 {
				return errors.New("boom")
			}
			return nil
		})
	}()

	// The failed initial render is retried by the resync ticker.
	require.Eventually(t, func() bool { return atomic.LoadInt32(&renders) >= 2 },
		10*time.Second, 10*time.Millisecond, "loop did not survive a render error")

	cancel()
	require.NoError(t, <-done)
}

// TestSyncRPKStanza covers the v2 render: graft the rpk stanza from the
// chart-rendered source while preserving the per-pod mutations the
// configurator init script applied to the destination.
func TestSyncRPKStanza(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	dest := filepath.Join(dir, "dest.yaml")

	// Source: fresh ConfigMap content after a blue/green migration.
	require.NoError(t, os.WriteFile(source, []byte(`
redpanda:
  empty_seed_starts_cluster: false
rpk:
  kafka_api:
    brokers:
      - cluster-blue-0.cluster.svc:9093
`), 0o644))

	// Destination: rendered at startup with old brokers AND per-pod
	// mutations (advertised address, rack) that must be preserved.
	require.NoError(t, os.WriteFile(dest, []byte(`
redpanda:
  advertised_kafka_api:
    - address: cluster-green-0.cluster.svc
      port: 9093
  rack: us-east-1a
rpk:
  kafka_api:
    brokers:
      - cluster-green-0.cluster.svc:9093
`), 0o644))

	require.NoError(t, SyncRPKStanza(source, dest))

	buf, err := os.ReadFile(dest)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, yaml.Unmarshal(buf, &got))

	// rpk stanza replaced with the fresh broker list.
	rpk := got["rpk"].(map[string]any)
	brokers := rpk["kafka_api"].(map[string]any)["brokers"].([]any)
	require.Equal(t, []any{"cluster-blue-0.cluster.svc:9093"}, brokers)

	// Per-pod redpanda mutations preserved.
	rp := got["redpanda"].(map[string]any)
	assert.Equal(t, "us-east-1a", rp["rack"])
	advertised := rp["advertised_kafka_api"].([]any)[0].(map[string]any)
	assert.Equal(t, "cluster-green-0.cluster.svc", advertised["address"])
}

// TestSyncRPKStanzaNoopWithoutRPKKey ensures a source without an rpk stanza
// never clobbers the destination.
func TestSyncRPKStanzaNoopWithoutRPKKey(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	dest := filepath.Join(dir, "dest.yaml")

	require.NoError(t, os.WriteFile(source, []byte("redpanda: {}\n"), 0o644))
	original := "rpk:\n  kafka_api:\n    brokers:\n      - keep-me:9093\n"
	require.NoError(t, os.WriteFile(dest, []byte(original), 0o644))

	require.NoError(t, SyncRPKStanza(source, dest))

	buf, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, original, string(buf))
}

// TestSyncRPKStanzaBadSource ensures unparseable source content errors
// without touching the destination (the watcher retries on the next event).
func TestSyncRPKStanzaBadSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	dest := filepath.Join(dir, "dest.yaml")

	require.NoError(t, os.WriteFile(source, []byte("{not yaml"), 0o644))
	original := "rpk: {}\n"
	require.NoError(t, os.WriteFile(dest, []byte(original), 0o644))

	require.Error(t, SyncRPKStanza(source, dest))

	buf, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, original, string(buf))
}
