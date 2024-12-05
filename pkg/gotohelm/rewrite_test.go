// Copyright 2024 Redpanda Data, Inc.
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
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestLoadPackages(t *testing.T) {
	t.Skipf("not working post repo merger")

	td, err := filepath.Abs("testdata")
	require.NoError(t, err)

	pkgs, err := LoadPackages(&packages.Config{
		Dir: filepath.Join(td, "src/example"),
		Env: append(
			os.Environ(),
			"GOPATH="+td,
			"GO111MODULE=on",
		),
	}, "./...")
	require.NoError(t, err)

	for _, pkg := range pkgs {
		pkg := pkg
		t.Run(pkg.Name, func(t *testing.T) {
			for _, f := range pkg.Syntax {
				var buf bytes.Buffer

				// Inject a built tag into the golden files so they don't get picked up
				// by LoadPackages.
				buf.WriteString("//go:build rewrites\n")

				require.NoError(t, format.Node(&buf, pkg.Fset, f))

				filename := pkg.Fset.File(f.Pos()).Name()
				filename = filename[:len(filename)-len(".go")] + ".rewritten.go"

				testutil.AssertGolden(t, testutil.Text, filename, buf.Bytes())
			}
		})
	}
}
