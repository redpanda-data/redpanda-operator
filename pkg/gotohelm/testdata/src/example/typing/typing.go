// Copyright 2025 Redpanda Data, Inc.
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
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func Typing(dot *helmette.Dot) map[string]any {
	return map[string]any{
		"zeros":             zeros(),
		"numbers":           numbers(),
		"embedding":         embedding(dot),
		"compileMe":         compileMe(),
		"typeTesting":       typeTesting(dot),
		"typeAssertions":    typeSwitching(dot),
		"typeSwitching":     typeSwitching(dot),
		"nestedFieldAccess": nestedFieldAccess(),
	}
}
