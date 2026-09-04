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
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// Facts carry the `+gotohelm:` directives of a package that another package
// depends on.
//
// Transpiling a call needs the directives on the *callee*, which lives in a
// package the transpiler may not be looking at: a `+gotohelm:builtin` changes
// the call into a sprig builtin, a `+gotohelm:name` changes the template it
// includes, and a `+gotohelm:namespace` changes that template's prefix. The
// transpiler used to reach them by carrying a flattened map of every loaded
// package and searching it by source position.
//
// A go/analysis pass only ever sees one package's syntax, so that doesn't
// work. Facts do: the driver runs over the import graph in dependency order,
// so by the time a chart is transpiled every directive it needs has been
// recorded. Routing the direct entry point through the same driver is what
// lets there be one way in rather than two.
//
// They're split by scope, because go/analysis facts are:
//
//   - A directive on a function belongs to a [types.Func], so it's an object
//     fact. See [funcDirectives].
//   - A directive on a file describes no object at all, so those become a
//     package fact. Only `namespace` is meaningful to another package, and a
//     package may declare it once, so that's all this carries. See
//     [namespaceDirective].

// funcDirectives records the `+gotohelm:` directives written on a function.
//
// Only functions that carry one get a fact, which keeps the graph empty for
// the overwhelming majority of packages.
type funcDirectives struct {
	Values map[string]string
}

func (*funcDirectives) AFact() {}

func (d *funcDirectives) String() string {
	keys := make([]string, 0, len(d.Values))
	for key := range d.Values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+d.Values[key])
	}

	return "gotohelm:" + strings.Join(parts, ",")
}

// GobEncode marshals the directives as JSON.
//
// Facts are gob encoded, and drivers that cache them compare the encoding byte
// for byte. gob writes a map in iteration order, which go randomizes, so a
// fact holding a map of more than one entry encodes differently every time. Go
// has no ordered map, so no choice of field type avoids that; [encoding/json]
// already sorts map keys, so it does the marshalling and gob carries the bytes.
func (d *funcDirectives) GobEncode() ([]byte, error) { return json.Marshal(d.Values) }

// GobDecode unmarshals the JSON written by [funcDirectives.GobEncode].
func (d *funcDirectives) GobDecode(data []byte) error { return json.Unmarshal(data, &d.Values) }

// namespaceDirective records a package's `+gotohelm:namespace`.
//
// Only the override needs a fact. The default is derived from the package's
// name and import path, both of which [types.Package] carries without any
// syntax.
type namespaceDirective struct {
	Name string
}

func (*namespaceDirective) AFact() {}

func (d *namespaceDirective) String() string { return "namespace=" + d.Name }

// exportDirectives records every directive in the package.
//
// This runs for every package in the import graph, not just the chart's,
// because a chart may call into any of them.
func (t *Transpiler) exportDirectives() {
	var namespace string

	for _, file := range t.Files {
		if declared, ok := fileDirectives(file)["namespace"]; ok {
			if namespace != "" && namespace != declared {
				panic(fmt.Sprintf("multiple namespace directives encountered in %q: %q and %q", t.Types.Path(), declared, namespace))
			}

			namespace = declared
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			if !mentionsDirective(fn.Doc) {
				continue
			}

			values := parseDirectives(fn.Doc.Text())
			if len(values) == 0 {
				continue
			}

			if obj, ok := t.TypesInfo.ObjectOf(fn.Name).(*types.Func); ok {
				t.pass.ExportObjectFact(obj, &funcDirectives{Values: values})
			}
		}
	}

	if namespace != "" {
		t.pass.ExportPackageFact(&namespaceDirective{Name: namespace})
	}
}

// fileDirectives returns the gotohelm directives on a file's doc comment.
func fileDirectives(file *ast.File) map[string]string {
	if file.Doc == nil || !mentionsDirective(file.Doc) {
		return nil
	}

	return parseDirectives(file.Doc.Text())
}

// mentionsDirective reports whether a comment group contains a gotohelm
// directive.
//
// It scans the raw comment text rather than calling [ast.CommentGroup.Text],
// which allocates a cleaned copy of the whole group. Transpiling a chart walks
// every doc comment in its import graph -- around 57,000 of them for the
// redpanda chart -- to find the 70 or so that carry a directive, so the
// allocation is the bulk of the work.
func mentionsDirective(doc *ast.CommentGroup) bool {
	for _, comment := range doc.List {
		if strings.Contains(comment.Text, "+gotohelm:") {
			return true
		}
	}

	return false
}

// directivesOf returns the `+gotohelm:` directives on fn, wherever it's
// declared.
func (t *Transpiler) directivesOf(fn types.Object) map[string]string {
	var fact funcDirectives
	t.pass.ImportObjectFact(fn, &fact)

	return fact.Values
}

// namespaceOverride returns a package's `+gotohelm:namespace`, if it declared
// one.
func (t *Transpiler) namespaceOverride(pkg *types.Package) string {
	var fact namespaceDirective
	t.pass.ImportPackageFact(pkg, &fact)

	return fact.Name
}
