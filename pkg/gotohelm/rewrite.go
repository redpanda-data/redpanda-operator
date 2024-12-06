// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package gotohelm

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"

	"github.com/cockroachdb/errors"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

type astRewrite func(*packages.Package, *ast.File) (_ *ast.File, changed bool)

var rewrites = []astRewrite{
	hoistIfs,
}

// LoadPackages is a wrapper around [packages.Load] that performs a handful of
// AST rewrites followed by a second invocation of [packages.Load] to
// appropriately populate the AST.
// AST rewriting is done to keep the transpilation process to be as simple as
// possible. Any unsupported or non-trivially supported expressions/statements
// will be rewritten to supported equivalents instead.
// If need be, the rewritten files can also be dumped to disk and have assertions made
func LoadPackages(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
	// Ensure we're getting all the values we need (which is pretty much everything...)
	cfg.Mode |= packages.NeedName | packages.NeedFiles | packages.NeedImports |
		packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
		packages.NeedDeps

	// Add in the gotohelm build tag for any package that wants to either
	// include or exclude specific files.
	// internal/bootstrap uses this feature to speed up transpilation time as
	// sprig's functions can be stubbed out.
	cfg.BuildFlags = append(cfg.BuildFlags, "-tags=gotohelm")

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return pkgs, err
	}

	if cfg.Overlay == nil {
		cfg.Overlay = map[string][]byte{}
	}

	for _, pkg := range pkgs {
		var errs []error
		for i := range pkg.Errors {
			e := pkg.Errors[i]
			errs = append(errs, e)
		}

		for i := range pkg.TypeErrors {
			e := pkg.Errors[i]
			errs = append(errs, e)
		}

		if len(errs) > 0 {
			return nil, errors.Wrapf(errors.Join(errs...), "package %s", pkg.Name)
		}

		for _, parsed := range pkg.Syntax {
			filename := pkg.Fset.File(parsed.Pos()).Name()

			var changed bool
			for _, rewrite := range rewrites {
				var didChange bool
				parsed, didChange = rewrite(pkg, parsed)
				changed = changed || didChange
			}

			if !changed {
				continue
			}

			var buf bytes.Buffer
			if err := format.Node(&buf, pkg.Fset, parsed); err != nil {
				return nil, err
			}

			cfg.Overlay[filename] = buf.Bytes()
		}
	}

	pkgs, err = packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}

	for _, pkg := range pkgs {
		var errs []error
		for _, e := range pkg.Errors {
			errs = append(errs, e)
		}

		for _, e := range pkg.TypeErrors {
			errs = append(errs, e)
		}

		if len(errs) > 0 {
			return nil, errors.Wrapf(errors.Join(errs...), "package %s", pkg.Name)
		}
	}

	return pkgs, nil
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
		if !ok || node.Init == nil {
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
