// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package lint enforces the enterprise module's dependency boundary: this
// module must not depend on any other module in the redpanda-operator
// monorepo (only common-go/* and third-party). See ../README.md. These tests
// catch violations even in workspace mode, where the compiler would happily
// resolve a sibling import that go.mod alone would reject.
package lint

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

const (
	monorepoPrefix = "github.com/redpanda-data/redpanda-operator/"
	selfModule     = "github.com/redpanda-data/redpanda-operator/enterprise"
)

// moduleRoot returns the enterprise module's root directory (the parent of
// this package's directory).
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolving caller path")
	return filepath.Dir(filepath.Dir(file))
}

// TestGoModBoundary asserts that go.mod declares the expected module path and
// contains no require or replace directives referencing monorepo siblings —
// including filesystem replaces, which could smuggle in a sibling module
// without a monorepo import path.
func TestGoModBoundary(t *testing.T) {
	root := moduleRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)

	mod, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err)
	require.Equal(t, selfModule, mod.Module.Mod.Path)

	for _, req := range mod.Require {
		require.False(t, strings.HasPrefix(req.Mod.Path, monorepoPrefix),
			"go.mod requires monorepo sibling %q; the enterprise module may only depend on common-go/* and third-party modules", req.Mod.Path)
	}

	for _, rep := range mod.Replace {
		require.False(t, strings.HasPrefix(rep.Old.Path, monorepoPrefix) || strings.HasPrefix(rep.New.Path, monorepoPrefix),
			"go.mod replaces monorepo sibling %q => %q", rep.Old.Path, rep.New.Path)
		require.False(t, strings.HasPrefix(rep.New.Path, "./") || strings.HasPrefix(rep.New.Path, "../"),
			"go.mod has filesystem replace %q => %q; filesystem replaces can bypass the module boundary", rep.Old.Path, rep.New.Path)
	}
}

// TestNoMonorepoImports walks every .go file in the module and asserts none
// imports a monorepo package outside this module.
func TestNoMonorepoImports(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata trees may hold intentionally broken or templated Go
			// sources; hidden directories aren't part of the build.
			if d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(pkg, monorepoPrefix) && pkg != selfModule && !strings.HasPrefix(pkg, selfModule+"/") {
				t.Errorf("%s imports monorepo sibling %q; the enterprise module may only depend on common-go/* and third-party modules", path, pkg)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, checked, "no Go files checked; is the walk rooted correctly?")
}
