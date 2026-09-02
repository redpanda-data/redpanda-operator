// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package gotohelm

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// TestLoadPackages golden files what the rewrites in internal/rewrite do to
// the transpiler's own corpus.
//
// TestRewritesPreserveBehavior proves the rewrites don't change what a program
// means; this shows what they actually produce, over far more input than that
// one fixture, and confirms the result still type checks -- LoadPackages
// re-runs packages.Load over it.
//
// NB: A golden is only kept for a file a rewrite touched. Most of the corpus
// isn't rewritten at all, and 18 byte-for-byte copies of their own source said
// nothing. A file with no golden is asserted to come back unchanged, so an
// over-eager rewrite still fails the test.
func TestLoadPackages(t *testing.T) {
	td, err := filepath.Abs("testdata")
	require.NoError(t, err)

	pkgs, err := LoadPackages(&packages.Config{
		Dir: filepath.Join(td, "src/example"),
	}, "./...")
	require.NoError(t, err)

	for _, pkg := range pkgs {
		t.Run(pkg.Name, func(t *testing.T) {
			for _, f := range pkg.Syntax {
				var rewritten bytes.Buffer
				require.NoError(t, format.Node(&rewritten, pkg.Fset, f))

				source := pkg.Fset.File(f.Pos()).Name()
				golden := strings.TrimSuffix(source, ".go") + ".rewritten.go"

				original, err := os.ReadFile(source)
				require.NoError(t, err)

				if bytes.Equal(original, rewritten.Bytes()) {
					require.NoFileExists(t, golden, "no rewrite applied to %s, so it should have no golden file", filepath.Base(source))
					continue
				}

				var buf bytes.Buffer

				// Inject a build tag into the golden files so they don't get
				// picked up by LoadPackages.
				buf.WriteString("//go:build rewrites\n\n")
				buf.Write(rewritten.Bytes())

				testutil.AssertGolden(t, testutil.Text, golden, buf.Bytes())
			}
		})
	}
}
