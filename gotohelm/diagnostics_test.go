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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// TestTranspileReportsEveryProblem covers the point of reporting rather than
// panicking: a single pass surfaces everything it couldn't transpile.
//
// The old Unsupported panic unwound the whole walk, so a file with seven
// problems took seven fix-and-rerun cycles to get through, each one revealing
// exactly one more.
func TestTranspileReportsEveryProblem(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "analyzer"))
	require.NoError(t, err)

	pkgs, err := LoadPackages(&packages.Config{
		Dir: dir,
		Env: append(os.Environ(), "GOWORK=off"),
	}, "./reported")
	require.NoError(t, err)

	chart, err := Transpile(pkgs)
	require.Nil(t, chart, "a chart must not be returned alongside diagnostics")

	var diagnostics *DiagnosticsError
	require.ErrorAs(t, err, &diagnostics)

	messages := make([]string, 0, len(diagnostics.Diagnostics))
	for _, d := range diagnostics.Diagnostics {
		messages = append(messages, d.Message)
	}

	require.ElementsMatch(t, []string{
		"fallthrough is not supported",
		"break inside a switch case is not supported",
		"break inside a switch case is not supported",
		"fallthrough is not supported",
		"type switch statements are not supported",
		"select statements are not supported",
		"Unsupported assignment token",
		"Unsupported assignment token",
		"*ast.BinaryExpr of != is not supported in for condition",
		"type assertions on numeric types are unreliable due to JSON casting all numbers to float64's. Instead use `helmette.IsNumeric` or `helmette.AsIntegral`",
		"unsupported type cast to uint",
		"No matching *ast.BinaryExpr signature for [[int & int] [_ & int] [int & _] [_ & _]]",
	}, messages)

	// Every diagnostic must be placed; a positionless one is what the panics
	// used to give, and it's the main thing that made them hard to act on.
	for _, d := range diagnostics.Diagnostics {
		require.True(t, d.Pos.IsValid(), "diagnostic %q has no position", d.Message)
	}

	// And they arrive in source order rather than traversal order.
	require.IsIncreasing(t, func() []int {
		out := make([]int, 0, len(diagnostics.Diagnostics))
		for _, d := range diagnostics.Diagnostics {
			out = append(out, int(d.Pos))
		}
		return out
	}())
}
