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

// A linter is one registry entry: the name .golangci.yml enables it by and
// //nolint suppresses it by -- golangci-lint's name wherever one exists --
// and a constructor for the analyzers that run under it. Construction is
// deferred so a disabled linter is never built and never reads settings.
type linter struct {
	name      string
	analyzers func(*Config) []*analysis.Analyzer
}

// registry is everything this binary can run. To add an off-the-shelf
// analyzer: import it, add an entry, list the name under linters.enable in
// .golangci.yml. One that needs settings gets a constructor that reads them
// with settings(); newDepguard in tools.go is the shortest example. To write your own,
// copy analyzers/todo. README.md walks through both.
func registry() []linter {
	return []linter{
		{"govet", fixed(vetDefaults...)},
		{"staticcheck", staticcheckAnalyzers},
		{"unused", fixed(newUnused())},
		{"gosec", one(newGosec)},
		{"unparam", one(newUnparam)},
		{"misspell", one(newMisspell)},
		{"ineffassign", fixed(ineffassign.Analyzer)},
		{"importas", one(newImportas)},
		{"depguard", one(newDepguard)},
		{"gofumpt", fixed(newGofumpt())},
		{"gci", one(newGci)},
		{laconiccomments.Name, one(newLaconiccomments)},

		// House-written analyzers live under analyzers/. Registered here means
		// available; .golangci.yml decides whether they run.
		{todo.Analyzer.Name, fixed(todo.Analyzer)},
	}
}

func main() {
	// nil for a process that must not need a config; see load. Everything
	// downstream takes it as a parameter and tolerates nil.
	cfg := load()

	all := registry()
	if cfg != nil {
		cfg.check(all)
	}

	var analyzers []*analysis.Analyzer

	for _, l := range all {
		if cfg != nil && !cfg.enabled[l.name] {
			continue
		}

		for _, a := range l.analyzers(cfg) {
			analyzers = append(analyzers, wrap(a, l.name, cfg))
		}
	}

	multichecker.Main(analyzers...)
}

// one adapts the common single-analyzer constructor shape.
func one(ctor func(*Config) *analysis.Analyzer) func(*Config) []*analysis.Analyzer {
	return func(cfg *Config) []*analysis.Analyzer { return []*analysis.Analyzer{ctor(cfg)} }
}

// fixed registers analyzers whose construction needs no settings.
func fixed(as ...*analysis.Analyzer) func(*Config) []*analysis.Analyzer {
	return func(*Config) []*analysis.Analyzer { return as }
}
