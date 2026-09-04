// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package gotohelm_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/redpanda-data/redpanda-operator/gotohelm"
)

// module is the import path prefix of the fixtures. They live in the gotohelm
// module rather than one of their own: the go tool ignores testdata when
// resolving wildcards, so they're never built as part of the package, but an
// explicit pattern loads them with gotohelm's own dependencies. That means no
// second go.mod to keep tidy every time gotohelm's requirements move.
const module = "github.com/redpanda-data/redpanda-operator/gotohelm/testdata/analyzer/"

// fixtures are the packages that stand in for chart source. The transpiler
// only reports on packages it's told make up the chart, so these are handed to
// [gotohelm.NewAnalyzer] as well as to analysistest.
var fixtures = []string{
	// The constructs the transpiler reports on rather than panicking over.
	//
	// The much larger corpus in testdata/analyzer/unsupported is not wired up
	// yet: most of it still reaches a raw panic or an unchecked type
	// assertion, which crashes the driver rather than producing a diagnostic.
	// Those packages join this list as the remaining panics are converted.
	"reported",
}

// TestAnalyzer drives the transpiler through go/analysis over a corpus of
// untranspilable go.
//
// The fixtures assert on messages rather than on transpiled output, which is
// what makes them useful: they pin down what a chart author is told, and they
// outlive any particular arrangement of the checks that produce it.
func TestAnalyzer(t *testing.T) {
	var deps, patterns []string
	for _, fixture := range fixtures {
		deps = append(deps, module+fixture)
		patterns = append(patterns, "./testdata/analyzer/"+fixture)
	}

	// NB: analysistest picks module mode when the directory it's given holds a
	// go.mod, so it's pointed at the module root rather than at testdata.
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, root, gotohelm.NewAnalyzer(deps...), patterns...)
}
