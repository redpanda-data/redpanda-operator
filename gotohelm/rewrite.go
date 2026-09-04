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
	"bytes"
	"go/format"

	"github.com/cockroachdb/errors"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/gotohelm/internal/rewrite"
)

// LoadPackages is a wrapper around [packages.Load] that performs a handful of
// AST rewrites followed by a second invocation of [packages.Load] to
// appropriately populate the AST.
// AST rewriting is done to keep the transpilation process to be as simple as
// possible. Any unsupported or non-trivially supported expressions/statements
// will be rewritten to supported equivalents instead.
// If need be, the rewritten files can also be dumped to disk and have assertions made
func LoadPackages(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
	// The first load exists only to give the rewrites enough type information
	// to work with, and they only ever touch the identifiers of the packages
	// they're rewriting. That needs LoadSyntax for the roots; their imports
	// can come from export data, which is markedly cheaper than type checking
	// the whole graph from source.
	//
	// The second load widens to NeedDeps, which the transpiler does need: the
	// analysis driver runs over every package in the graph to collect
	// `+gotohelm:` directives as facts, and that requires their syntax.
	cfg.Mode |= packages.LoadSyntax

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

			parsed, changed := rewrite.Rewrite(pkg, parsed)
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

	cfg.Mode |= packages.NeedDeps

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
