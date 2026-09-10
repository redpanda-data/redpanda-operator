// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package rewrite holds the source to source transformations gotohelm applies
// before transpiling.
//
// Certain go constructs are difficult to transpile directly but have an
// equivalent that isn't. Rather than teach the transpiler about both forms,
// gotohelm rewrites the awkward one into the simple one and transpiles that.
//
// Rewrites run inside gotohelm.LoadPackages, which formats the result and
// re-runs packages.Load over it so type information matches the rewritten
// tree. Two consequences follow, and both matter:
//
//   - A rewrite must produce valid, type correct go. It is real source that
//     gets re-parsed, not an internal representation.
//   - The result is golden filed by TestLoadPackages as `<name>.rewritten.go`,
//     so every transformation is visible on the page and diffable. That's the
//     main reason to express a transformation here rather than inline in the
//     transpiler, where it can't be seen or tested independently.
package rewrite

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// A rewrite transforms a file into an equivalent one that gotohelm can
// transpile more simply, reporting whether it changed anything.
type rewrite func(*packages.Package, *ast.File) (_ *ast.File, changed bool)

// all is every rewrite, in the order they are applied.
//
// NB: Ordering here is important. Rewrites build on each other.
//   - desugarSwitches generates if else chains.
//   - hoistIfs generates assignments.
//   - unrollAssignments makes multivalue assignments work.
var all = []rewrite{
	desugarSwitches,
	hoistIfs,
	unrollAssignments,
}

// Rewrite applies every rewrite to f, returning the result and whether
// anything changed.
//
// This is the package's only entry point. The individual rewrites are
// deliberately unexported: they have ordering constraints between them, and a
// caller applying a subset would produce a tree the transpiler doesn't expect.
func Rewrite(pkg *packages.Package, f *ast.File) (*ast.File, bool) {
	var changed bool

	for _, apply := range all {
		var didChange bool

		f, didChange = apply(pkg, f)
		changed = changed || didChange
	}

	return f, changed
}

// hoistIfs "hoists" all assignments within an if else chain to be above said
// chain. It munges the variable names to ensure that variable shadowing
// doesn't become an issues.
// NOTE: All assignments within if-else chains MUST expect to be called as if
// hoisting nullifies the capabilities of short-circuiting.
//
//	if x, ok := m[k1]; ok {
//	} y, ok := m[k2]; ok {
//	}
//
// Will get rewritten to:
//
//	x, ok_1 := m[k1]
//	y, ok_2 := m[k2]
//
//	if ok_1 {
//	} else if ok_2 {
//	}
func hoistIfs(pkg *packages.Package, f *ast.File) (*ast.File, bool) {
	count := 0
	info := pkg.TypesInfo
	renames := map[types.Object]*ast.Ident{}

	return astutil.Apply(f, func(c *astutil.Cursor) bool {
		node, ok := c.Node().(*ast.IfStmt)
		if !ok || !hoistable(node) {
			return true
		}

		for _, v := range node.Init.(*ast.AssignStmt).Lhs {
			old := v.(*ast.Ident)
			if old.Name == "_" {
				continue
			}

			count++
			new := ast.NewIdent(fmt.Sprintf("%s_%d", old.Name, count))
			new.Obj = old.Obj

			renames[info.ObjectOf(old)] = new

			info.Defs[new] = info.Defs[old]
			info.Instances[new] = info.Instances[old]
		}

		return true
	}, func(c *astutil.Cursor) bool {
		switch node := c.Node().(type) {
		case *ast.Ident:
			if rename, ok := renames[info.ObjectOf(node)]; ok {
				c.Replace(rename)
			}

		case *ast.IfStmt:
			// Don't process if-else statements as c.InsertBefore will panic.
			// Instead, we loop through the first if and hoist all child
			// assignments.
			if _, ok := c.Parent().(*ast.IfStmt); ok {
				return true
			}

			// If any link in the chain can't be hoisted, leave the whole chain
			// alone. Its inits are emitted inline by the transpiler, which is
			// correct (and actually preserves the short-circuiting that
			// hoisting gives up); anything that can't be transpiled that way
			// is reported by the analysis package.
			for n := node; n != nil; n, _ = n.Else.(*ast.IfStmt) {
				if n.Init != nil && !hoistable(n) {
					return true
				}
			}

			for n := node; n != nil; {
				if n.Init != nil {
					c.InsertBefore(n.Init)
					n.Init = nil
				}

				n, _ = n.Else.(*ast.IfStmt)
			}
		}

		return true
	}).(*ast.File), count > 0
}

// hoistable reports whether stmt's init is an assignment to plain identifiers,
// which is the only shape hoistIfs knows how to rename and relocate.
func hoistable(stmt *ast.IfStmt) bool {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok {
		return false
	}

	for _, lhs := range init.Lhs {
		if _, ok := lhs.(*ast.Ident); !ok {
			return false
		}
	}

	return true
}
