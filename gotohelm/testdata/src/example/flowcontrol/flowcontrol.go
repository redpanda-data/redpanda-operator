// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package flowcontrol

import (
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func FlowControl(dot *helmette.Dot) map[string]any {
	return map[string]any{
		"earlyReturn":    earlyReturn(dot),
		"ifElse":         ifElse(dot),
		"sliceRanges":    sliceRanges(dot),
		"mapRanges":      mapRanges(dot),
		"intBinaryExprs": intBinaryExprs(),
	}
}

func earlyReturn(dot *helmette.Dot) string {
	// This is trickily written on purpose.
	if b, ok := dot.Values["boolean"]; ok && b.(bool) {
		return "Early Returns work!"
	}
	return "Should have returned early"
}

func ifElse(dot *helmette.Dot) string {
	oneToFour, ok := helmette.AsIntegral[int](dot.Values["oneToFour"])
	if !ok {
		return "oneToFour not specified!"
	}

	if oneToFour == 1 {
		return "It's 1"
	} else if oneToFour == 2 {
		return "It's 2"
	} else if oneToFour == 3 {
		return "It's 3"
	} else {
		return "It's 4"
	}
	return "unreachable"
}

func sliceRanges(dot *helmette.Dot) []any {
	intsAny, ok := dot.Values["ints"]
	if !ok {
		intsAny = []any{}
	}

	ints := intsAny.([]any)

	sumOfIndexes := 0
	for i := range ints {
		sumOfIndexes = sumOfIndexes + i
	}

	continuesWork := true
	for range ints {
		continue
		continuesWork = false
	}

	breaksWork := true
	for range ints {
		break
		breaksWork = false
	}

	return []any{
		sumOfIndexes,
		continuesWork,
		breaksWork,
	}
}

func mapRanges(dot *helmette.Dot) []any {
	m := map[string]int{"1": 1, "2": 2, "3": 3}

	// NOTE: Ranges of maps are not technically equivalent. In go, they are
	// non-deterministic but range nodes with templates are deterministic.
	for k := range m {
		_ = k
	}

	sum := 0
	for _, v := range m {
		sum = sum + v
	}

	return []any{sum}
}

func intBinaryExprs() []int {
	x := 1
	y := 2
	z := 3

	// Not currently supported.
	// z += x
	// z -= y
	// z *= y
	// z /= y

	return []int{
		z,
		x - y,
		x + y,
		x / y,
		x * y,
	}
}
