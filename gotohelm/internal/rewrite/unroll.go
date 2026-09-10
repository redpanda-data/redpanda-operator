// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package rewrite

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// unrollAssignments splits a multi-value assignment into one assignment per
// pair, which is all the transpiler knows how to emit.
//
//	x, y := 1, 2
//
// becomes
//
//	x := 1
//	y := 2
//
// Go evaluates every right hand side before assigning any of them, so a naive
// split changes the meaning of a statement that reads a variable it also
// writes. `a, b = b, a` swaps in go, but unrolled in order it assigns b to a
// and then a -- now also b -- back to b, leaving both holding the original b.
// When that can happen every value is captured into a temporary first, which
// restores go's evaluate-then-assign order:
//
//	a, b = b, a
//
// becomes
//
//	_tmp_0 := b
//	_tmp_1 := a
//	a = _tmp_0
//	b = _tmp_1
//
// NB: This used to be done inline by the transpiler, where it silently got the
// swap wrong for years. Expressing it as a rewrite puts the result in a golden
// file where it can be read.
func unrollAssignments(pkg *packages.Package, f *ast.File) (*ast.File, bool) {
	var changed bool

	names := &temporaries{pkg: pkg.Types, used: map[*types.Scope]int{}}

	result := astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
		stmt, ok := c.Node().(*ast.AssignStmt)
		if !ok || len(stmt.Lhs) != len(stmt.Rhs) || len(stmt.Lhs) < 2 {
			return true
		}

		// InsertBefore panics unless the statement is an element of a slice,
		// which rules out an if or for statement's init. HoistIfs has already
		// lifted the if case out into the enclosing block; a for init is left
		// alone and rejected by the analysis package instead.
		if c.Index() < 0 {
			return true
		}

		for _, replacement := range unroll(pkg, names, stmt) {
			c.InsertBefore(replacement)
		}

		c.Delete()

		changed = true

		return true
	})

	return result.(*ast.File), changed
}

// unroll returns the statements that replace a multi-value assignment.
func unroll(pkg *packages.Package, names *temporaries, stmt *ast.AssignStmt) []ast.Stmt {
	values := stmt.Rhs

	// If any value reads a variable an earlier pair overwrites, capture them
	// all up front so that every value still sees the pre-assignment state.
	//
	// NB: The captures are emitted one per statement rather than as a single
	// multi-value assignment. astutil.Apply does not revisit the nodes a
	// rewrite inserts, so a multi-value capture would survive to the
	// transpiler, which no longer knows how to split one.
	var out []ast.Stmt
	if hazardous(pkg, stmt) {
		captured := make([]ast.Expr, len(stmt.Rhs))

		for i, rhs := range stmt.Rhs {
			// A value that can't observe anything can't observe an earlier
			// assignment either, so leave it in place. That keeps the output
			// smaller and, for `nil`, is required: `_tmp := nil` doesn't
			// compile, as untyped nil has no default type.
			if isConstant(pkg, rhs) {
				captured[i] = rhs
				continue
			}

			captured[i] = ast.NewIdent(names.next(stmt.Pos()))

			out = append(out, &ast.AssignStmt{
				Lhs: []ast.Expr{captured[i]},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{rhs},
			})
		}

		values = captured
	}

	for i, lhs := range stmt.Lhs {
		out = append(out, &ast.AssignStmt{
			Lhs: []ast.Expr{lhs},
			Tok: assignToken(pkg, stmt, lhs),
			Rhs: []ast.Expr{values[i]},
		})
	}

	return out
}

// assignToken returns the token a single unrolled pair should use.
//
// A `:=` may declare some of its names and assign to others, and `_` is never
// declared, so the token has to be decided per name: splitting `x, y := 1, 2`
// where x already exists into two `:=` statements would be a redeclaration.
func assignToken(pkg *packages.Package, stmt *ast.AssignStmt, lhs ast.Expr) token.Token {
	if stmt.Tok != token.DEFINE {
		return stmt.Tok
	}

	ident, ok := lhs.(*ast.Ident)
	if !ok || ident.Name == "_" {
		return token.ASSIGN
	}

	if pkg.TypesInfo.Defs[ident] == nil {
		return token.ASSIGN
	}

	return token.DEFINE
}

// hazardous reports whether unrolling stmt in order would change its meaning,
// i.e. whether any value reads something an earlier pair assigns.
func hazardous(pkg *packages.Package, stmt *ast.AssignStmt) bool {
	for i, lhs := range stmt.Lhs {
		// A target that isn't the last one has values evaluated after it, so
		// it's only safe if we can prove none of them read it.
		if i == len(stmt.Lhs)-1 {
			continue
		}

		switch lhs := lhs.(type) {
		case *ast.Ident:
			if lhs.Name == "_" {
				continue
			}

			for _, rhs := range stmt.Rhs[i+1:] {
				if readsVariable(pkg, rhs, lhs.Name) {
					return true
				}
			}

		default:
			// Anything else -- p.a, m[k], *ptr -- writes through a target this
			// can't track to the expressions that might read it, so assume the
			// worst. `p.a, p.b = p.b, p.a` swaps just like plain variables do.
			return true
		}
	}

	return false
}

// readsVariable reports whether expr reads a variable called name.
//
// NB: It compares names rather than objects because a `:=` redeclares its left
// hand side, so the variable being written and the one being read are distinct
// objects. Struct fields are excluded by their nil parent scope, which keeps a
// field selector like `s.a` from looking like a read of `a`.
func readsVariable(pkg *packages.Package, expr ast.Expr, name string) bool {
	var found bool

	ast.Inspect(expr, func(node ast.Node) bool {
		if found {
			return false
		}

		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		if v, ok := pkg.TypesInfo.Uses[ident].(*types.Var); ok && v.Parent() != nil && v.Name() == name {
			found = true
		}

		return !found
	})

	return found
}

// isConstant reports whether expr always evaluates to the same thing, which
// makes it insensitive to when it's evaluated.
func isConstant(pkg *packages.Package, expr ast.Expr) bool {
	tv, ok := pkg.TypesInfo.Types[expr]
	if !ok {
		return false
	}

	return tv.IsNil() || tv.Value != nil
}

// temporaries hands out names for captured values.
//
// Names are numbered per scope rather than derived from the source position,
// so inserting a line somewhere else in the file doesn't renumber every
// temporary below it. That keeps the rewritten goldens, and the templates
// transpiled from them, stable under unrelated edits.
type temporaries struct {
	pkg  *types.Package
	used map[*types.Scope]int
}

// next returns a name that is free at pos.
func (t *temporaries) next(pos token.Pos) string {
	scope := t.pkg.Scope().Innermost(pos)

	for {
		name := fmt.Sprintf("_tmp_%d", t.used[scope])
		t.used[scope]++

		// A name already visible here would be shadowed by the temporary,
		// changing what the surrounding code refers to.
		if scope == nil {
			return name
		}

		if obj, _ := scope.LookupParent(name, pos); obj == nil {
			return name
		}
	}
}
