// Copyright 2026 Redpanda Data, Inc.
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
		"blockScoping":   blockScoping(),
		"switches":       switches(dot),
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

func switches(dot *helmette.Dot) map[string]any {
	oneToFour, ok := helmette.AsIntegral[int](dot.Values["oneToFour"])
	if !ok {
		return map[string]any{}
	}

	return map[string]any{
		"tagged":       tagged(oneToFour),
		"tagless":      tagless(oneToFour),
		"defaultFirst": defaultFirst(oneToFour),
		"noDefault":    noDefault(oneToFour),
		"onlyDefault":  onlyDefault(),
		"nested":       nested(oneToFour),
		"initShadows":  initShadows(oneToFour),
		"switchInit":   switchInit(oneToFour),
		"inRange":      inRange(oneToFour),
		"returns":      returns(oneToFour),
		"onString":     onString(oneToFour),
		"commented":    commented(oneToFour),
	}
}

func tagged(x int) string {
	switch x {
	case 1, 2:
		return "low"
	case 3:
		return "three"
	default:
		return "high"
	}
}

func tagless(x int) string {
	switch {
	case x < 2:
		return "under"
	case x < 4:
		return "middle"
	}
	return "over"
}

func defaultFirst(x int) string {
	switch x {
	default:
		return "other"
	case 1:
		return "one"
	}
}

func noDefault(x int) string {
	out := "unset"
	switch x {
	case 1:
		out = "one"
	}
	return out
}

func onlyDefault() string {
	switch {
	default:
		return "only"
	}
}

// nested's inner switch reuses the outer's temporary name. The case after it
// still reads the outer one, so the inner must not clobber it.
func nested(x int) string {
	switch x {
	case 1, 2:
		switch x * 10 {
		case 10:
			return "1"
		default:
			return "2"
		}
	case 3:
		return "3"
	}
	return "many"
}

// initShadows' switch declares a name the enclosing scope already has. The
// init is scoped to the switch, so the outer x survives it.
func initShadows(x int) []any {
	switch x := x * 10; x {
	case 10:
	}
	return []any{x}
}

// switchInit's init is a multi-value assignment, so the switch has to be
// desugared before it can be unrolled.
func switchInit(x int) []any {
	m := map[string]int{"a": 1}

	switch v, ok := m["a"]; v {
	case x:
		return []any{"match", ok}
	default:
		return []any{"miss", ok}
	}
}

// inRange's continue belongs to the range, not the switch.
func inRange(x int) []int {
	var out []int
	for _, i := range []int{1, 2, 3, 4} {
		switch i {
		case x:
			continue
		}
		out = append(out, i)
	}
	return out
}

func returns(x int) string {
	switch x {
	case 1:
		return "one"
	}
	return "other"
}

func onString(x int) string {
	switch helmette.Printf("%d", x) {
	case "1":
		return "one"
	case "2", "3":
		return "few"
	}
	return "many"
}

// commented exists to pin down what the printer does with comments attached to
// a clause the rewrite relocates.
func commented(x int) string {
	// A comment before the first case.
	switch x {
	case 1: // A trailing comment on a case.
		// A comment inside a case body.
		return "one"
	// A comment on a default that isn't last, whose body moves past the cases
	// below it.
	default:
		return "other"
	case 2:
		return "two"
	}
}

// blockScoping asserts that a block's declarations aren't visible after it.
//
// Templates have no block construct, so this is the case that a naive
// transpilation gets wrong: emitting the block's statements inline leaves the
// shadowing declaration on the variable stack and every later read sees it.
func blockScoping() []any {
	x := 1

	{
		x := 2
		_ = x
	}

	// A second, sibling block reusing the same name must not collide with the
	// first now that each is emitted into its own scope.
	{
		x := 3
		_ = x
	}

	return []any{x}
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
