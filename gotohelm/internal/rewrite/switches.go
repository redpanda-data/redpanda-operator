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
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// desugarSwitches turns a switch expression into the if else chain it's
// shorthand for, which the transpiler already knows how to emit.
//
//	switch init; tag {
//	case a, b:
//		X
//	default:
//		Y
//	case c:
//		Z
//	}
//
// becomes
//
//	{
//		init
//		_tmp_0 := tag
//		if _tmp_0 == a || _tmp_0 == b {
//			X
//		} else if _tmp_0 == c {
//			Z
//		} else {
//			Y
//		}
//	}
//
// The default becomes the final else wherever it appeared; go only reaches it
// once every case has failed.
//
// The block scopes init and the temporary the way the switch did. Switches
// [UnsupportedSwitch] rejects are left alone for the transpiler to report.
func desugarSwitches(pkg *packages.Package, f *ast.File) (*ast.File, bool) {
	var changed bool

	result := astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
		stmt, ok := c.Node().(*ast.SwitchStmt)
		if !ok {
			return true
		}

		// The only position that isn't a statement list is a labeled
		// statement's, and `L: switch { case 1: break L }` would become
		// `L: { break L }`, which doesn't compile: a label on a block isn't a
		// valid break target. Leave it for the transpiler to report.
		if c.Index() < 0 {
			return true
		}

		// A switch that can't be desugared is left in place. The transpiler
		// reaches it and reports the same rejections as diagnostics.
		if len(UnsupportedSwitch(stmt)) > 0 {
			return true
		}

		c.Replace(desugar(pkg.Types, stmt))

		changed = true

		return true
	})

	return result.(*ast.File), changed
}

// UnsupportedSwitch reports every construct that keeps stmt from being
// desugared into an if else chain. An empty result means stmt was desugared.
func UnsupportedSwitch(stmt *ast.SwitchStmt) []analysis.Diagnostic {
	var diagnostics []analysis.Diagnostic

	reject := func(n ast.Node, message string) {
		diagnostics = append(diagnostics, analysis.Diagnostic{
			Pos:      n.Pos(),
			End:      n.End(),
			Category: "unsupported",
			Message:  message,
		})
	}

	for _, clause := range stmt.Body.List {
		for _, s := range clause.(*ast.CaseClause).Body {
			ast.Inspect(s, func(n ast.Node) bool {
				switch n := n.(type) {
				// break and fallthrough bind to the innermost enclosing
				// statement that can own them, so anything nested that can own
				// one hides it from this switch. A label or goto in there is
				// the transpiler's to report: desugaring can't invalidate one,
				// as a case body is already an implicit block.
				case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
					*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
					return false

				case *ast.LabeledStmt:
					reject(n.Label, "labeled statements are not supported")

				case *ast.BranchStmt:
					switch {
					case n.Label != nil:
						// A label on the switch itself is rejected by
						// desugarSwitches, so this targets something outside
						// it.
						reject(n, "labeled "+n.Tok.String()+" is not supported")
					case n.Tok == token.BREAK:
						reject(n, "break inside a switch case is not supported")
					case n.Tok == token.FALLTHROUGH:
						reject(n, "fallthrough is not supported")
					case n.Tok == token.GOTO:
						reject(n, "goto statements are not supported")
					}
				}

				return true
			})
		}
	}

	return diagnostics
}

// desugar returns the block that replaces a switch statement. stmt must have
// no [UnsupportedSwitch] rejections.
func desugar(pkg *types.Package, stmt *ast.SwitchStmt) ast.Stmt {
	// Temporaries are numbered per scope and a switch is its own, so one built
	// here numbers the same as one shared across the file would.
	names := &temporaries{pkg: pkg, used: map[*types.Scope]int{}}

	var out []ast.Stmt
	if stmt.Init != nil {
		out = append(out, stmt.Init)
	}

	var cases []*ast.CaseClause
	var _default *ast.CaseClause

	for _, clause := range stmt.Body.List {
		clause := clause.(*ast.CaseClause)

		if clause.List == nil {
			_default = clause
			continue
		}

		cases = append(cases, clause)
	}

	// With no cases there's no chain to build, but the tag is still evaluated
	// exactly once.
	//
	//	switch tag {
	//	default:
	//		Y
	//	}
	//
	// becomes
	//
	//	{
	//		_ = tag
	//		{
	//			Y
	//		}
	//	}
	//
	// NB: Blank rather than a temporary. Nothing would read it, and an unused
	// variable doesn't compile.
	if len(cases) == 0 {
		if stmt.Tag != nil {
			out = append(out, &ast.AssignStmt{
				Lhs: []ast.Expr{ast.NewIdent("_")},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{stmt.Tag},
			})
		}

		if _default != nil {
			out = append(out, &ast.BlockStmt{List: _default.Body})
		}

		return &ast.BlockStmt{List: out}
	}

	// A tag is evaluated once, before any case, so it's captured even when it
	// looks like it couldn't change. Repeating the expression would place one
	// ast.Node at several positions in the tree, which go/printer isn't built
	// for, and it would compare against the untyped constant rather than its
	// default type, which is what go actually switches on.
	var tag string
	if stmt.Tag != nil {
		tag = names.next(stmt.Pos())

		out = append(out, &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(tag)},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{stmt.Tag},
		})
	}

	var chain *ast.IfStmt
	for i := len(cases) - 1; i >= 0; i-- {
		link := &ast.IfStmt{
			Cond: condition(tag, cases[i].List),
			Body: &ast.BlockStmt{List: cases[i].Body},
		}

		switch {
		case chain != nil:
			link.Else = chain
		case _default != nil && len(_default.Body) > 0:
			link.Else = &ast.BlockStmt{List: _default.Body}
		}

		chain = link
	}

	return &ast.BlockStmt{List: append(out, chain)}
}

// condition returns the expression that selects a case.
//
//	switch tag {
//	case a:     // tag == a
//	case a, b:  // tag == a || tag == b
//	}
//
//	switch {
//	case x > 1: // x > 1
//	}
func condition(tag string, exprs []ast.Expr) ast.Expr {
	var out ast.Expr

	for _, expr := range exprs {
		test := expr

		if tag != "" {
			// NB: Each comparison gets its own ident for the tag, positioned
			// on the case expression it's compared against. A shared one would
			// put a single ast.Node at several positions in the tree, and
			// leaving it unpositioned makes go/printer read the gap to the
			// case expression as a line break and split the comparison across
			// lines.
			ident := ast.NewIdent(tag)
			ident.NamePos = expr.Pos()

			test = &ast.BinaryExpr{
				X:     ident,
				OpPos: expr.Pos(),
				Op:    token.EQL,
				Y:     parenthesize(expr),
			}
		}

		if out == nil {
			out = test
			continue
		}

		out = &ast.BinaryExpr{X: out, OpPos: expr.Pos(), Op: token.LOR, Y: test}
	}

	return out
}

// parenthesize wraps expr if it could regroup when spliced into an `==`.
//
//	case a || b: // tag == (a || b)
//
// Without the parentheses that reads as `(tag == a) || b`, which compiles and
// quietly means something else. Only a binary expression binds looser than
// `==`, so that's the only thing wrapped.
func parenthesize(expr ast.Expr) ast.Expr {
	if _, ok := expr.(*ast.BinaryExpr); !ok {
		return expr
	}

	return &ast.ParenExpr{X: expr}
}
