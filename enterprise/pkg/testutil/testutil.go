// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package testutil is the enterprise module's minimal test-helper set,
// mirroring the handful of helpers its tests need from the monorepo's
// pkg/testutil (which this module must not import). Keep it small: anything
// heavier (golden files, k3d, vcluster) belongs to the OSS-hosted suites.
package testutil

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/goldenfile"
)

// Aliases for the extracted goldenfile library, mirroring the monorepo's
// pkg/testutil so test files can move between the modules without edits.

type Writer = goldenfile.Writer

const (
	YAML  = goldenfile.YAML
	JSON  = goldenfile.JSON
	Text  = goldenfile.Text
	Bytes = goldenfile.Bytes
)

var (
	AssertGolden = goldenfile.AssertGolden
	NewTxTar     = goldenfile.NewTxTar
)

// Context returns a [context.Context] that will cancel 1s before the t's
// deadline.
func Context(t *testing.T) context.Context {
	ctx := context.Background()
	if timeout, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, timeout.Add(-time.Second))
		t.Cleanup(cancel)
	}
	return ctx
}

// RequireTimeout asserts that go test was invoked with a -timeout of at least
// minimum.
func RequireTimeout(t *testing.T, minimum time.Duration) {
	deadline, ok := t.Deadline()
	if !ok {
		return
	}

	timeout := time.Until(deadline).Round(time.Minute)

	if timeout < minimum {
		t.Fatalf("-timeout is too low. needed at least %s; got: %s", minimum, timeout)
	}
}

// SkipIfNotIntegration skips t if the integration build tag has not be
// specified or -short has been specified. It additionally asserts that callers
// are appropriately prefixed with `TestIntegration` and that an appropriate
// `-timeout` value has been specified. To run integration tests, invoke go
// test as:
// `go test ./... --tags integration -run '^TestIntegration' -timeout 10m`
// Usage:
//
//	func TestIntegrationSomeIntegrationTest(t *testing.T) {
//		SkipIfNotIntegration(t, time.Hour)
//	}
func SkipIfNotIntegration(t *testing.T) {
	const prefix = "TestIntegration"

	// NB: This check is performed regardless of the build tags because we want
	// to catch naming issues as soon as possible.
	if !strings.HasPrefix(t.Name(), prefix) {
		t.Fatalf("tests calling SkipIfNotIntegration must be prefixed with %q; got: %s", prefix, t.Name())
	}

	if skipIntegrationTests {
		t.Skipf("integration build flag not set; skipping integration test")
	} else if testing.Short() {
		t.Skipf("-short specified; skipping integration test")
	} else {
		RequireTimeout(t, 20*time.Minute)
	}
}

// FreePorts allocates n free TCP ports on localhost by briefly binding to
// port 0 and returning the assigned ports. All listeners are closed before
// returning so the ports can be reused by the caller.
func FreePorts(t *testing.T, n int) []int {
	t.Helper()
	ports := make([]int, 0, n)
	listeners := make([]net.Listener, 0, n)
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("error getting free port: %v", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range listeners {
		l.Close() //nolint:gosec // best-effort cleanup in test helper; error is not actionable
	}
	return ports
}
