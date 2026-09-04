// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Command houselint is every linter and formatter from .golangci.yml, running
// in one go/analysis driver we own, plus the house-written ones under
// analyzers/. It reads .golangci.yml itself. README.md has the measurements
// against golangci-lint and how to add to it.
package main

import (
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/hidalgopl/laconiccomments"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/redpanda-data/redpanda-operator/lint/analyzers/todo"
)

// cfg is .golangci.yml, or nil for a process that must not need one; see load.
var cfg *Config

func main() {
	cfg = load()

	all := registry()
	if cfg != nil {
		cfg.check(all)
	}

	var analyzers []*analysis.Analyzer

	for _, l := range all {
		if cfg != nil && !cfg.enabled[l.name] {
			continue
		}

		for _, a := range l.analyzers {
			analyzers = append(analyzers, watch(a, l.name))
		}
	}

	multichecker.Main(analyzers...)
}

// A linter is one registry entry: the name .golangci.yml enables it by and
// //nolint suppresses it by -- golangci-lint's name wherever one exists --
// and the analyzers that run under it.
type linter struct {
	name      string
	analyzers []*analysis.Analyzer
}

// registry is everything this binary can run. To add an off-the-shelf
// analyzer: import it, add an entry, list the name under linters.enable in
// .golangci.yml. One that needs settings gets a constructor that reads them
// with settings(); newDepguard in tools.go is the shortest example. To write your own,
// copy analyzers/todo. README.md walks through both.
func registry() []linter {
	return []linter{
		{"govet", vetDefaults},
		{"staticcheck", staticcheckAnalyzers()},
		{"unused", []*analysis.Analyzer{newUnused()}},
		{"gosec", []*analysis.Analyzer{newGosec()}},
		{"unparam", []*analysis.Analyzer{newUnparam()}},
		{"misspell", []*analysis.Analyzer{newMisspell()}},
		{"ineffassign", []*analysis.Analyzer{ineffassign.Analyzer}},
		{"importas", []*analysis.Analyzer{newImportas()}},
		{"depguard", []*analysis.Analyzer{newDepguard()}},
		{"gofumpt", []*analysis.Analyzer{newGofumpt()}},
		{"gci", []*analysis.Analyzer{newGci()}},
		{laconiccomments.Name, []*analysis.Analyzer{newLaconiccomments()}},

		// House-written analyzers live under analyzers/. Registered here means
		// available; .golangci.yml decides whether they run.
		{todo.Analyzer.Name, []*analysis.Analyzer{todo.Analyzer}},
	}
}
