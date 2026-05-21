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
	"os"
	"os/exec"
	"path/filepath"
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
// wrapLifecycleHook expects its cmd slice to be space-joinable into a
// single valid shell command (e.g. ["bash", "-x", "/var/lifecycle/preStop.sh"]
// in production). The tests follow that contract by writing the hook
// body to a temp script and invoking it via `bash -x <path>`.
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

	// writeHook writes body to a tempfile and returns a cmd slice
	// suitable for wrapLifecycleHook — argv-style, no shell quoting.
	//
	// The hook path is rendered into the wrapper *unquoted* (matching
	// the production usage with /var/lifecycle/preStop.sh), so it must
	// be free of shell-special characters. t.TempDir() bakes the
	// subtest name into the path, and Go's testing harness can emit
	// names containing parentheses (e.g. "non-zero_(not_timeout)") —
	// so use a process-local temp directory and a deterministic
	// filename instead.
	hookDir, err := os.MkdirTemp("", "wraplifecyclehook")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(hookDir) })

	writeHook := func(t *testing.T, name, body string) []string {
		t.Helper()
		path := filepath.Join(hookDir, name+".sh")
		require.NoError(t, os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0o755))
		return []string{"bash", "-x", path}
	}

	// run renders the wrapper, substitutes /proc/1/fd/1 → /dev/stdout
	// (so we observe the teed output here; behavior under test is
	// unchanged), executes via bash and returns combined stdout.
	run := func(t *testing.T, cmd []string, budget int64) (stdout string, exitErr error) {
		t.Helper()
		wrapped := wrapLifecycleHook("pre-stop", budget, cmd)
		script := strings.ReplaceAll(wrapped[2], "/proc/1/fd/1", "/dev/stdout")

		var out, errOut bytes.Buffer
		c := exec.Command("bash", "-c", script)
		c.Stdout = &out
		c.Stderr = &errOut
		err := c.Run()
		if err != nil {
			t.Logf("stderr:\n%s", errOut.String())
		}
		return out.String(), err
	}

	t.Run("timeout fires emits TIMEOUT marker", func(t *testing.T) {
		// Hook sleeps 8s, budget is 2s → timeout MUST fire.
		cmd := writeHook(t, "timeout", `echo hook-started
sleep 8
echo should-not-reach
`)
		stdout, err := run(t, cmd, 2)
		require.NoError(t, err, "wrapper must exit 0 (the trailing `true` preserves the safety property)")
		assert.Contains(t, stdout, "hook-started", "hook body should have started")
		assert.NotContains(t, stdout, "should-not-reach", "hook must have been killed before completion")
		assert.Contains(t, stdout, "TIMEOUT after 2s", "PR #1548 marker line must appear when timeout fires")
		assert.Contains(t, stdout, "exit 124", "marker must include the exit code from PIPESTATUS")
	})

	t.Run("fast hook does not emit TIMEOUT marker", func(t *testing.T) {
		cmd := writeHook(t, "fast", `echo fast-hook
`)
		stdout, err := run(t, cmd, 5)
		require.NoError(t, err)
		assert.Contains(t, stdout, "fast-hook")
		assert.NotContains(t, stdout, "TIMEOUT", "no false-positive: marker must not appear when the hook completed within budget")
	})

	t.Run("hook exits non-zero (not timeout) is still swallowed by trailing true", func(t *testing.T) {
		// Hook fails fast with exit 42 — should NOT trigger the TIMEOUT
		// branch (exit code is neither 124 nor 137) and the trailing
		// `true` should still mask the failure exit.
		cmd := writeHook(t, "fail", `echo about-to-fail
exit 42
`)
		stdout, err := run(t, cmd, 5)
		require.NoError(t, err, "trailing `true` must swallow non-timeout non-zero exits to avoid container-kill on transient hook bugs")
		assert.Contains(t, stdout, "about-to-fail")
		assert.NotContains(t, stdout, "TIMEOUT", "marker fires only on 124/137, not arbitrary non-zero")
	})
}
