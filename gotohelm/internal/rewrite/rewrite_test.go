// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package rewrite_test

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/gotohelm/internal/rewrite"
)

// TestRewritesPreserveBehavior runs testdata/behavior twice -- once as
// written, once with every rewrite applied -- and requires the two to print
// the same thing.
//
// The golden files in gotohelm's own testdata show what the rewrites produce.
// Nothing there says the output still *means* the same thing, which is exactly
// how UnrollAssignments' predecessor shipped a broken swap: `a, b = b, a`
// unrolled in order leaves both holding the original b, and the golden file
// looked perfectly reasonable.
//
// NB: This compiles and runs a program twice, so it's deliberately a single
// test over one fixture that exercises every rewrite, rather than one test per
// case.
func TestRewritesPreserveBehavior(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("testdata", "behavior"))
	require.NoError(t, err)

	before := run(t, source)

	rewritten := t.TempDir()
	writeRewritten(t, source, rewritten)

	after := run(t, rewritten)

	require.NotEmpty(t, before)
	require.Equal(t, before, after, "the rewrites changed the program's behavior")
}

// writeRewritten loads the package at source, applies every rewrite, and
// writes the result to dir along with the go.mod needed to run it.
func writeRewritten(t *testing.T, source, dir string) {
	t.Helper()

	pkgs, err := packages.Load(&packages.Config{
		Dir: source,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}, ".")
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]
	require.Empty(t, pkg.Errors)
	require.NotEmpty(t, pkg.Syntax)

	var changed bool
	for _, file := range pkg.Syntax {
		name := filepath.Base(pkg.Fset.File(file.Pos()).Name())

		file, didChange := rewrite.Rewrite(pkg, file)
		changed = changed || didChange

		var buf bytes.Buffer
		require.NoError(t, format.Node(&buf, pkg.Fset, file))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o600))
	}

	// A fixture that no rewrite touches would make this test vacuous.
	require.True(t, changed, "no rewrite applied to the fixture")

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module behavior\n\ngo 1.26.6\n"),
		0o600,
	))
}

// run executes the program in dir and returns everything it printed.
func run(t *testing.T, dir string) string {
	t.Helper()

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	// The rewritten copy lives outside the repository, so it must not inherit
	// the workspace.
	cmd.Env = append(os.Environ(), "GOWORK=off")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go run in %s failed:\n%s", dir, out)

	return string(out)
}
