// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapLifecycleHook_TimeoutSurfaced executes the actual rendered
// bash wrapper against:
//
//  1. a hook body that exceeds the time budget — the new PIPESTATUS
//     check must surface a "TIMEOUT after Ns" marker line and
//  2. a hook body that completes in time — no TIMEOUT line and no
//     spurious diagnostics.
//
// This is the behavior the wrapper was changed to guarantee in
// PR #1548. The chart template golden file pins the *shape* of the
// rendered string; this test pins the *runtime behavior* of that string
// so that a future refactor (e.g. someone rewrites the wrapper but
// silently drops the PIPESTATUS branch) gets caught.
//
// Skipped when:
//   - GOOS != linux  — the wrapper teas to /proc/1/fd/1 which is
//     Linux-only. We test-substitute /dev/stdout but only on linux to
//     stay close to the production execution path.
//   - timeout(1) is not on PATH — GNU coreutils utility; CI's nix
//     devshell provides it.
func TestWrapLifecycleHook_TimeoutSurfaced(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: wrapper hardcodes /proc/1/fd/1; only meaningful on linux (got %s)", runtime.GOOS)
	}
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("skipping: timeout(1) not on PATH")
	}

	t.Run("timeout fires emits TIMEOUT marker", func(t *testing.T) {
		// 2s budget vs 8s sleep — timeout MUST fire.
		cmd := wrapLifecycleHook("pre-stop", 2, []string{"bash", "-c", `echo hook-started; sleep 8; echo should-not-reach`})

		// Substitute /proc/1/fd/1 → /dev/stdout so we observe the
		// teed output here. The original path is meaningful only
		// inside a pod; behavior under test (PIPESTATUS surfacing
		// 124 via the TIMEOUT line) is unchanged by this swap.
		script := strings.ReplaceAll(cmd[2], "/proc/1/fd/1", "/dev/stdout")

		var stdout, stderr bytes.Buffer
		c := exec.Command("bash", "-c", script)
		c.Stdout = &stdout
		c.Stderr = &stderr
		err := c.Run()

		require.NoError(t, err, "wrapper must exit 0 (the trailing `true` preserves the safety property)\nstderr: %s", stderr.String())
		assert.Contains(t, stdout.String(), "hook-started", "hook body should have started")
		assert.NotContains(t, stdout.String(), "should-not-reach", "hook must have been killed before completion")
		assert.Contains(t, stdout.String(), "TIMEOUT after 2s", "PR #1548 marker line must appear when timeout fires")
		assert.Contains(t, stdout.String(), "exit 124", "marker must include the exit code from PIPESTATUS")
	})

	t.Run("fast hook does not emit TIMEOUT marker", func(t *testing.T) {
		cmd := wrapLifecycleHook("pre-stop", 5, []string{"bash", "-c", `echo fast-hook`})
		script := strings.ReplaceAll(cmd[2], "/proc/1/fd/1", "/dev/stdout")

		var stdout, stderr bytes.Buffer
		c := exec.Command("bash", "-c", script)
		c.Stdout = &stdout
		c.Stderr = &stderr
		err := c.Run()

		require.NoError(t, err, "stderr: %s", stderr.String())
		assert.Contains(t, stdout.String(), "fast-hook")
		assert.NotContains(t, stdout.String(), "TIMEOUT", "no false-positive: marker must not appear when the hook completed within budget")
	})

	t.Run("hook exits non-zero (not timeout) is still swallowed by trailing true", func(t *testing.T) {
		// Hook that fails fast with exit 42 — should NOT trigger the
		// TIMEOUT branch (exit code is neither 124 nor 137) and the
		// trailing `true` should still mask the failure exit.
		cmd := wrapLifecycleHook("pre-stop", 5, []string{"bash", "-c", `echo about-to-fail; exit 42`})
		script := strings.ReplaceAll(cmd[2], "/proc/1/fd/1", "/dev/stdout")

		var stdout, stderr bytes.Buffer
		c := exec.Command("bash", "-c", script)
		c.Stdout = &stdout
		c.Stderr = &stderr
		err := c.Run()

		require.NoError(t, err, "trailing `true` must swallow non-timeout non-zero exits to avoid container-kill on transient hook bugs\nstderr: %s", stderr.String())
		assert.Contains(t, stdout.String(), "about-to-fail")
		assert.NotContains(t, stdout.String(), "TIMEOUT", "marker fires only on 124/137, not arbitrary non-zero")
	})
}
