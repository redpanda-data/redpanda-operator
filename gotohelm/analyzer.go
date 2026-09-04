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
	"reflect"

	"golang.org/x/tools/go/analysis"
)

// filesType is the analyzer's ResultType: the templates a package transpiled
// to.
var filesType = reflect.TypeOf([]*File(nil))

// NewAnalyzer returns an analyzer that treats the packages at the given paths
// as chart source.
//
// deps is the set of packages that make up the chart: the package being
// transpiled plus any bundled packages and subcharts, exactly as [Transpile]
// computes it.
func NewAnalyzer(deps ...string) *analysis.Analyzer {
	chart := map[string]struct{}{}
	for _, dep := range deps {
		chart[dep] = struct{}{}
	}

	analyzer := &analysis.Analyzer{
		Name:       "gotohelm",
		Doc:        "transpiles go to helm templates, reporting anything it can't",
		ResultType: filesType,
		FactTypes: []analysis.Fact{
			(*funcDirectives)(nil),
			(*namespaceDirective)(nil),
		},
	}

	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		t := newTranspiler(pass, chart)

		// Directives are recorded for every package in the graph, not just
		// the chart's, because a chart may call into any of them and their
		// directives change how it does so.
		t.exportDirectives()

		if _, ok := chart[pass.Pkg.Path()]; !ok {
			return []*File(nil), nil
		}

		files := t.Transpile()

		for _, diagnostic := range t.diagnostics {
			pass.Report(diagnostic)
		}

		return files, nil
	}

	return analyzer
}
