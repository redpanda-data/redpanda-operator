// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package todo is a house-written analyzer, and the template for the next one:
// a plain go/analysis Analyzer with a test. It reports TODO and FIXME comments
// that name neither an owner nor an issue, since those are the ones nobody
// comes back for.
//
// Register a new analyzer in registry() in lint/main.go and enable it in
// .golangci.yml under linters.enable; //nolint:<name> suppresses it like any other.
package todo

import (
	"go/token"
	"regexp"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "todo",
	Doc:  "reports TODO and FIXME comments that name neither an owner nor an issue",
	Run:  run,
}

// marker matches the keyword and, in the second group, an optional "(owner)"
// or "(#123)" directly after it.
var marker = regexp.MustCompile(`\b(TODO|FIXME)\b(\([^)]+\))?`)

// run reads comments straight off the files. Comments are not part of the
// AST body, so no inspector is needed and no type information either, which
// keeps this analyzer cheap.
func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, group := range f.Comments {
			for _, c := range group.List {
				for _, m := range marker.FindAllStringSubmatchIndex(c.Text, -1) {
					if m[4] >= 0 {
						continue // has an owner or issue
					}

					keyword := c.Text[m[2]:m[3]]
					pass.Reportf(c.Slash+token.Pos(m[0]),
						"%s without an owner or issue: write %s(name) or %s(#1234)", keyword, keyword, keyword)
				}
			}
		}
	}

	return nil, nil
}
