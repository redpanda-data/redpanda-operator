// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutil

import (
	"context"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/goldenfile"
)

var (
	retain = flag.Bool("retain", false, "if true, no clean up will be performed.")
	// note that for golden files the -update flag is now -update-golden due to externalizing the library
	update = flag.Bool("update", false, "if true, golden assertions will update the expected file instead of performing an assertion")
)

// TestType represents the type of test being run, i.e. unit, integration, or acceptance
type TestType int

const (
	TestTypeUnit TestType = iota
	TestTypeIntegration
	TestTypeAcceptance
	TestTypeMulticluster
)

// String returns the human readable name of the test type.
func (t TestType) String() string {
	switch t {
	case TestTypeUnit:
		return "unit"
	case TestTypeIntegration:
		return "integration"
	case TestTypeAcceptance:
		return "acceptance"
	case TestTypeMulticluster:
		return "multicluster"
	default:
		return "unknown"
	}
}

// Type returns the type of test being run.
func Type() TestType {
	if IsAcceptance() {
		return TestTypeAcceptance
	}
	if IsIntegration() {
		return TestTypeIntegration
	}
	if IsMulticluster() {
		return TestTypeMulticluster
	}
	return TestTypeUnit
}

// IsMulticluster returns true if this is a multicluster test
func IsMulticluster() bool {
	return skipMulticlusterTests == false
}

// IsAcceptance returns true if this is an acceptance test
func IsAcceptance() bool {
	return skipAcceptanceTests == false
}

// IsIntegration returns true if this is an integration test
func IsIntegration() bool {
	return skipIntegrationTests == false
}

// IsUnit returns true if this is a unit test
func IsUnit() bool {
	return skipIntegrationTests == true && skipAcceptanceTests == true
}

// Retain returns the value of the -retain CLI flag. A value of true indicates
// that cleanup actions should be SKIPPED.
func Retain() bool {
	return *retain
}

// Update returns value of the -update CLI flag. A value of true indicates that
// computed files should be updated instead of asserted against.
func Update() bool {
	return *update || goldenfile.Update()
}

// TempDir is wrapper around [testing.T.TempDir] that respects [Retain].
func TempDir(t *testing.T) string {
	t.Helper()
	if !Retain() {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp(os.TempDir(), t.Name())
	if err != nil {
		t.Fatalf("%+v", err)
	}
	return dir
}

// MaybeCleanup is helper to invoke `fn` within a [testing.T.Cleanup] closure
// only if [Retain] returns false.
func MaybeCleanup(t *testing.T, fn func()) {
	t.Cleanup(func() {
		if Retain() {
			return
		}
		fn()
	})
}

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

func SkipIfNotMulticluster(t *testing.T) {
	const prefix = "TestMulticluster"

	// NB: This check is performed regardless of the build tags because we want
	// to catch naming issues as soon as possible.
	if !strings.HasPrefix(t.Name(), prefix) {
		t.Fatalf("tests calling SkipIfNotMulticluster must be prefixed with %q; got: %s", prefix, t.Name())
	}

	if skipMulticlusterTests {
		t.Skipf("multicluster build flag not set; skipping multicluster test")
	} else if testing.Short() {
		t.Skipf("-short specified; skipping multicluster test")
	} else {
		RequireTimeout(t, 60*time.Minute)
	}
}

func SkipIfNotAcceptance(t *testing.T) {
	const prefix = "TestAcceptance"

	// NB: This check is performed regardless of the build tags because we want
	// to catch naming issues as soon as possible.
	if !strings.HasPrefix(t.Name(), prefix) {
		t.Fatalf("tests calling SkipIfNotAcceptance must be prefixed with %q; got: %s", prefix, t.Name())
	}

	if skipAcceptanceTests {
		t.Skipf("acceptance build flag not set; skipping acceptance test")
	} else if testing.Short() {
		t.Skipf("-short specified; skipping acceptance test")
	} else {
		RequireTimeout(t, 20*time.Minute)
	}
}

// RequireTimeout asserts that the `-timeout` flag is at least `minimum`.
// Usage:
//
//	func TestLogThing(t *testing.T) {
//		RequireTimeout(t, time.Hour)
//	}
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

// Aliases for extracted library

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
