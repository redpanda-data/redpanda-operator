// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutil

import (
	"bytes"
	"context"
	"flag"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
)

var (
	retain = flag.Bool("retain", false, "if true, no clean up will be performed.")
	update = flag.Bool("update", false, "if true, golden assertions will update the expected file instead of performing an assertion")
)

// Retain returns the value of the -retain CLI flag. A value of true indicates
// that cleanup actions should be SKIPPED.
func Retain() bool {
	return *retain
}

// Update returns value of the -update CLI flag. A value of true indicates that
// computed files should be updated instead of asserted against.
func Update() bool {
	return *update
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

// Writer wraps a [testing.T] to implement [io.Writer] by utilizing
// [testing.T.Log].
type Writer struct {
	T *testing.T
}

func (w Writer) Write(p []byte) (int, error) {
	w.T.Log(string(p))
	return len(p), nil
}

type GoldenAssertion int

const (
	YAML GoldenAssertion = iota
	JSON
	Text
	Bytes
)

func assertGolden(t *testing.T, assertionType GoldenAssertion, path string, expected, actual []byte, update func(string, []byte) error) {
	t.Helper()

	if Update() {
		require.NoError(t, update(path, actual))
		return
	}

	const msg = "Divergence from snapshot at %q. If this change is expected re-run this test with -update."

	switch assertionType {
	case Text:
		assert.Equal(t, string(expected), string(actual), msg, path)
	case Bytes:
		assert.Equal(t, expected, actual, msg, path)
	case JSON:
		assert.JSONEq(t, string(expected), string(actual), msg, path)
	case YAML:
		actualDocuments, err := ytbx.LoadDocuments(actual)
		require.NoError(t, err)

		expectedDocuments, err := ytbx.LoadDocuments(expected)
		require.NoError(t, err)

		report, err := dyff.CompareInputFiles(
			ytbx.InputFile{Documents: expectedDocuments},
			ytbx.InputFile{Documents: actualDocuments},
		)
		if err != nil {
			require.NoError(t, err)
		}

		if len(report.Diffs) > 0 {
			hr := dyff.HumanReport{Report: report, OmitHeader: true}

			var buf bytes.Buffer
			require.NoError(t, hr.WriteReport(&buf))

			require.Fail(t, buf.String())
		}

	default:
		require.Fail(t, "unknown assertion type: %#v", assertionType)
	}
}

// AssertGolden is a helper for "golden" or "snapshot" testing. It asserts
// that `actual`, a serialized YAML document, is equal to the one at `path`. If
// `-update` has been passed to `go test`, `actual` will be written to `path`.
func AssertGolden(t *testing.T, assertionType GoldenAssertion, path string, actual []byte) {
	expected, err := os.ReadFile(path)
	if !os.IsNotExist(err) {
		require.NoError(t, err)
	}

	assertGolden(t, assertionType, path, expected, actual, func(s string, b []byte) error {
		return os.WriteFile(path, actual, 0o644)
	})
}

type TxTarGolden struct {
	mu      sync.Mutex
	archive *txtar.Archive
}

func NewTxTar(t *testing.T, path string) *TxTarGolden {
	archive, err := txtar.ParseFile(path)
	if os.IsNotExist(err) {
		archive = &txtar.Archive{}
	} else if err != nil {
		require.NoError(t, err)
	}

	g := &TxTarGolden{archive: archive}

	if Update() {
		t.Cleanup(func() {
			require.NoError(t, g.update(path))
		})
	}

	return g
}

func (g *TxTarGolden) update(path string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	slices.SortFunc(g.archive.Files, func(a, b txtar.File) int {
		return strings.Compare(a.Name, b.Name)
	})

	return os.WriteFile(path, txtar.Format(g.archive), 0o644)
}

func (g *TxTarGolden) getFile(path string) *txtar.File {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, file := range g.archive.Files {
		if file.Name == path {
			return &g.archive.Files[i]
		}
	}
	g.archive.Files = append(g.archive.Files, txtar.File{
		Name: path,
		Data: []byte{},
	})
	return &g.archive.Files[len(g.archive.Files)-1]
}

func (g *TxTarGolden) AssertGolden(t *testing.T, assertionType GoldenAssertion, path string, actual []byte) {
	t.Helper()

	file := g.getFile(path)

	assertGolden(t, assertionType, path, file.Data, actual, func(s string, b []byte) error {
		file.Data = b
		return nil
	})
}
