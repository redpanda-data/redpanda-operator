// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configurator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

func writeRPKTemplate(t *testing.T, dir, brokers string) {
	t.Helper()
	profile := `version: 4
current_profile: rpk-default-profile
profiles:
  - name: rpk-default-profile
    kafka_api:
      brokers:
        - ` + brokers + `
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, clusterconfiguration.RPKProfileYamlFile), []byte(profile), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, clusterconfiguration.RPKProfileYamlFixupFile), []byte(`[]`), 0o644))
}

func readRendered(t *testing.T, path string) string {
	t.Helper()
	buf, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(buf)
}

// TestWatchRPKProfileRendersOnChange verifies the K8S-755 fix: the watcher
// renders the profile at startup and re-renders it when the source template
// changes, without any restart.
func TestWatchRPKProfileRendersOnChange(t *testing.T) {
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	dest := filepath.Join(destDir, "rpk.yaml")

	writeRPKTemplate(t, sourceDir, "cluster-green-0.cluster.default.svc.cluster.local:9092")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- watchRPKProfile(ctx, sourceDir, dest, 50*time.Millisecond) }()

	// Initial render happens before the watch loop starts.
	require.Eventually(t, func() bool {
		buf, err := os.ReadFile(dest)
		return err == nil && len(buf) > 0
	}, 5*time.Second, 10*time.Millisecond, "initial render did not happen")
	require.Contains(t, readRendered(t, dest), "cluster-green-0")

	// Simulate the operator updating seed brokers in the ConfigMap (e.g. a
	// blue/green NodePool migration) propagated by the kubelet into the
	// mounted directory.
	writeRPKTemplate(t, sourceDir, "cluster-blue-0.cluster.default.svc.cluster.local:9092")

	require.Eventually(t, func() bool {
		buf, err := os.ReadFile(dest)
		return err == nil && !strings.Contains(string(buf), "cluster-green-0") && strings.Contains(string(buf), "cluster-blue-0")
	}, 10*time.Second, 50*time.Millisecond, "profile was not re-rendered after template change")

	cancel()
	require.NoError(t, <-done)
}

// TestWatchRPKProfileSurvivesRenderErrors verifies a broken template doesn't
// crash the watcher (a transient half-propagated ConfigMap mount must not
// crashloop the broker pod) and that it recovers on the next change.
func TestWatchRPKProfileSurvivesRenderErrors(t *testing.T) {
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	dest := filepath.Join(destDir, "rpk.yaml")

	// Invalid YAML template: initial render fails, watcher must keep going.
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, clusterconfiguration.RPKProfileYamlFile), []byte("{not yaml"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, clusterconfiguration.RPKProfileYamlFixupFile), []byte(`[]`), 0o644))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- watchRPKProfile(ctx, sourceDir, dest, 50*time.Millisecond) }()

	// Fix the template; the watcher should recover and render.
	writeRPKTemplate(t, sourceDir, "cluster-blue-0.cluster.default.svc.cluster.local:9092")

	require.Eventually(t, func() bool {
		buf, err := os.ReadFile(dest)
		return err == nil && strings.Contains(string(buf), "cluster-blue-0")
	}, 10*time.Second, 50*time.Millisecond, "watcher did not recover from render error")

	cancel()
	require.NoError(t, <-done)
}
