// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// watch wraps an analyzer so its diagnostics pass through what golangci-lint
// applied after a linter reported, and which a bare analysis driver has none
// of: nolint directives, the exclusions from .golangci.yml, and generated
// files. The analyzer gets a copy of the pass whose Report is ours; that is
// the one interception point in the go/analysis contract.
func watch(a *analysis.Analyzer, linter string) *analysis.Analyzer {
	run := a.Run

	a.Run = func(pass *analysis.Pass) (any, error) {
		filtered := *pass
		filtered.Report = func(d analysis.Diagnostic) {
			if !suppressed(pass, linter, d) {
				pass.Report(d)
			}
		}

		return run(&filtered)
	}

	return a
}

func suppressed(pass *analysis.Pass, linter string, d analysis.Diagnostic) bool {
	f := fileFor(pass, d.Pos)
	if f == nil || cfg == nil {
		return false
	}

	if isGenerated(f) || isPackageDocHeight(pass.Fset, f, linter, d) {
		return true
	}

	pos := pass.Fset.Position(d.Pos)

	for _, e := range cfg.exclusions {
		if e.matches(linter, pos.Filename, d.Message) {
			return true
		}
	}

	return nolinted(pass.Fset, f, linter, pos.Line)
}

// exclusion is one entry of linters.exclusions: a `paths` pattern (no linter
// list, so every linter) or a `rules` entry.
type exclusion struct {
	linters []string
	path    *regexp.Regexp
	text    *regexp.Regexp
}

func (e exclusion) matches(linter, path, message string) bool {
	switch {
	case len(e.linters) > 0 && !slices.Contains(e.linters, linter):
		return false
	case e.path != nil && !e.path.MatchString(path):
		return false
	default:
		return e.text == nil || e.text.MatchString(message)
	}
}

// isPackageDocHeight exempts a package doc from the comment-height rule. A
// package doc is the package's manual, not a code comment, and a directive
// cannot say so narrowly: one placed in a package doc reaches the package
// clause below it and so covers the entire file. Line length still applies.
func isPackageDocHeight(fset *token.FileSet, f *ast.File, linter string, d analysis.Diagnostic) bool {
	return linter == "laconiccomments" && f.Doc != nil &&
		strings.HasPrefix(d.Message, "comment spans ") &&
		fset.Position(d.Pos).Line == fset.Position(f.Doc.Pos()).Line
}

// isGenerated is the strict marker from https://go.dev/s/generatedcode, which
// is what exclusions.generated: strict means.
func isGenerated(f *ast.File) bool {
	for _, group := range f.Comments {
		if group.Pos() > f.Package {
			return false
		}

		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "// Code generated ") && strings.HasSuffix(c.Text, " DO NOT EDIT.") {
				return true
			}
		}
	}

	return false
}

func fileFor(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, f := range pass.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}

	return nil
}

// nolinted reports whether a //nolint directive covers linter at line, with
// golangci-lint's semantics (pkg/result/processors/nolint_filter.go): a
// directive anywhere in a comment group covers the whole group, and the range
// extends over the node that starts on the group's next line in the group's
// own column -- which is what separates "covers the function below" from a
// directive trailing a line of code.
//
// Everything is recomputed per diagnostic. Diagnostics are rare.
func nolinted(fset *token.FileSet, f *ast.File, linter string, line int) bool {
	spans := outermostNodes(fset, f)

	for _, group := range f.Comments {
		from := fset.Position(group.Pos())
		to := fset.Position(group.End()).Line

		if n, ok := spans[to+1]; ok && n.column == from.Column {
			to = n.end
		}

		if line < from.Line || line > to {
			continue
		}

		for _, c := range group.List {
			if names, ok := parseNolint(c.Text); ok && (names == nil || names[linter]) {
				return true
			}
		}
	}

	return false
}

type span struct {
	pos    token.Pos
	column int
	end    int
}

// outermostNodes maps a line to the node that starts earliest on it, which is
// the one reaching furthest down the file: a directive above a func covers the
// whole func, not its first statement.
func outermostNodes(fset *token.FileSet, f *ast.File) map[int]span {
	spans := map[int]span{}

	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}

		pos := fset.Position(n.Pos())
		if s, ok := spans[pos.Line]; !ok || n.Pos() < s.pos {
			spans[pos.Line] = span{n.Pos(), pos.Column, fset.Position(n.End()).Line}
		}

		return true
	})

	return spans
}

// parseNolint parses a comment by golangci-lint's rule, ^nolint( |:|$): the
// linters named, or nil for all of them (a bare //nolint, or //nolint:all).
// An explanation may follow after a second //.
func parseNolint(text string) (names map[string]bool, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimLeft(strings.TrimPrefix(text, "//"), " \t"), "nolint")
	if !ok {
		return nil, false
	}

	switch {
	case rest == "" || rest[0] == ' ' || rest[0] == '\t':
		return nil, true
	case rest[0] != ':':
		return nil, false
	}

	list, _, _ := strings.Cut(rest[1:], "//")
	names = map[string]bool{}

	for _, name := range strings.Split(list, ",") {
		switch name = strings.ToLower(strings.TrimSpace(name)); name {
		case "":
		case "all":
			return nil, true
		default:
			names[name] = true
		}
	}

	if len(names) == 0 {
		return nil, true
	}

	return names, true
}
