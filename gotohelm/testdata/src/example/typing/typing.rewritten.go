//go:build rewrites

// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package typing

import (
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Typing(dot *helmette.Dot) map[string]any {
	return map[string]any{
		"zeros":             zeros(),
		"numbers":           numbers(),
		"embedding":         embedding(dot),
		"compileMe":         compileMe(),
		"typeAliases":       typeAliases(),
		"typeTesting":       typeTesting(dot),
		"typeAssertions":    typeSwitching(dot),
		"typeSwitching":     typeSwitching(dot),
		"nestedFieldAccess": nestedFieldAccess(),
	}
}

type Base struct {
	X int
}

type MyAlias Base

type OtherAlias = Base

func typeAliases() []Base {

	a := Base{X: 1}
	b := MyAlias{X: 2}
	c := OtherAlias{X: 3}

	return []Base{
		a,
		Base(MyAlias(a)),
		Base(OtherAlias(a)),
		Base(b),
		Base(c),
	}
}
