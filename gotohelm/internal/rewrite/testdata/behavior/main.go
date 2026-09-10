// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package main exercises every rewrite and prints the result.
//
// TestRewritesPreserveBehavior runs this file as written and again after the
// rewrites have been applied, and requires the two to print the same thing.
// It's the only check that the rewrites are semantics preserving rather than
// merely plausible looking; the golden files show what they produce, not
// whether it's right.
package main

import "fmt"

func main() {
	fmt.Println("swap:", swap("a", "b"))
	fmt.Println("threeWaySwap:", threeWaySwap(1, 2, 3))
	fmt.Println("redeclare:", redeclare(7))
	fmt.Println("independent:", independent("a", "b"))
	fmt.Println("sideEffects:", sideEffects())
	fmt.Println("mixedNewAndExisting:", mixedNewAndExisting(1))
	fmt.Println("blanks:", blanks())
	fmt.Println("selectors:", selectors())
	fmt.Println("hoistedIf:", hoistedIf(map[string]int{"a": 1}))
	fmt.Println("hoistedChain:", hoistedChain(map[string]int{"b": 2}))
	fmt.Println("swapInLoop:", swapInLoop())
	fmt.Println("nilAndLiterals:", nilAndLiterals())
	fmt.Println("tagOnce:", tagOnce())
	fmt.Println("tagged:", tagged(1), tagged(3), tagged(9))
	fmt.Println("tagless:", tagless(0), tagless(5), tagless(50))
	fmt.Println("defaultFirst:", defaultFirst(1), defaultFirst(9))
	fmt.Println("defaultMiddle:", defaultMiddle(1), defaultMiddle(2), defaultMiddle(9))
	fmt.Println("noDefault:", noDefault(1), noDefault(9))
	fmt.Println("onlyDefault:", onlyDefault())
	fmt.Println("noClauses:", noClauses())
	fmt.Println("emptyCase:", emptyCase(1), emptyCase(2))
	fmt.Println("switchInit:", switchInit(map[string]int{"a": 1}), switchInit(map[string]int{}))
	fmt.Println("initShadows:", initShadows())
	fmt.Println("caseShadows:", caseShadows(1), caseShadows(2))
	fmt.Println("nested:", nested(1, 1), nested(1, 2), nested(2, 1))
	fmt.Println("continueInCase:", continueInCase())
	fmt.Println("breakInNestedLoop:", breakInNestedLoop())
	fmt.Println("returnInCase:", returnInCase(1), returnInCase(9))
	fmt.Println("orInCase:", orInCase(true, false), orInCase(false, false))
	fmt.Println("namedType:", namedType(kindA), namedType(kindB))
	fmt.Println("shadowedTemp:", shadowedTemp(9), shadowedTemp(1))
	fmt.Println("declinedFallthrough:", declinedFallthrough(1))
	fmt.Println("declinedBreak:", declinedBreak(1))
}

// tagOnce pins down that the tag is evaluated exactly once and that case
// expressions short circuit at the first match.
func tagOnce() []string {
	order = nil
	switch note("tag", 3) {
	case note("c1", 1), note("c2", 2):
	case note("c3", 3):
	case note("c4", 4):
	}
	return order
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
	case x < 1:
		return "under"
	case x < 10:
		return "single"
	}
	return "many"
}

func defaultFirst(x int) string {
	switch x {
	default:
		return "other"
	case 1:
		return "one"
	}
}

// defaultMiddle has cases on both sides of the default, which must still run
// last.
func defaultMiddle(x int) string {
	switch x {
	case 1:
		return "one"
	default:
		return "other"
	case 2:
		return "two"
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

// noClauses still has to evaluate its tag.
func noClauses() []string {
	order = nil
	switch note("tag", 1) {
	}
	return order
}

func emptyCase(x int) string {
	out := "fell through"
	switch x {
	case 1:
	case 2:
		out = "two"
	}
	return out
}

func switchInit(m map[string]int) []any {
	switch v, ok := m["a"]; v {
	case 1:
		return []any{"one", ok}
	default:
		return []any{"other", ok}
	}
}

// initShadows declares a name the enclosing scope already has. The switch's
// init is scoped to the switch, so the outer x is untouched after it.
func initShadows() []int {
	x := 1
	switch x := 2; x {
	case 2:
	}
	return []int{x}
}

func caseShadows(x int) []int {
	y := 1
	switch x {
	case 1:
		y := 2
		_ = y
	case 2:
		y := 3
		_ = y
	}
	return []int{y}
}

func nested(x, y int) string {
	switch x {
	case 1:
		switch y {
		case 1:
			return "1-1"
		default:
			return "1-*"
		}
	}
	return "*"
}

// continueInCase's continue belongs to the range, not the switch, so it must
// survive desugaring.
func continueInCase() []int {
	var out []int
	for _, x := range []int{1, 2, 3, 4} {
		switch x {
		case 2:
			continue
		}
		out = append(out, x)
	}
	return out
}

// breakInNestedLoop's break belongs to the inner loop, which owns it, so the
// switch is still rewritable.
func breakInNestedLoop() []int {
	var out []int
	switch 1 {
	case 1:
		for _, x := range []int{1, 2, 3} {
			if x == 2 {
				break
			}
			out = append(out, x)
		}
	}
	return out
}

func returnInCase(x int) string {
	switch x {
	case 1:
		return "one"
	}
	return "other"
}

// orInCase's case expression binds looser than the == it's spliced into, so it
// has to be parenthesized or it regroups.
func orInCase(a, b bool) string {
	switch true {
	case a || b:
		return "either"
	}
	return "neither"
}

type kind int

const (
	kindA kind = iota
	kindB
)

func namedType(k kind) string {
	switch k {
	case kindA:
		return "a"
	}
	return "b"
}

// shadowedTemp has a variable named like the temporary the rewrite mints, so
// the temporary has to pick a different one.
func shadowedTemp(x int) []int {
	_tmp_0 := 9
	switch x {
	case _tmp_0:
		return []int{1, _tmp_0}
	}
	return []int{0, _tmp_0}
}

// declinedFallthrough is left as a switch by the rewrite. It's here so that a
// switch it won't touch is still known to behave the same.
func declinedFallthrough(x int) []string {
	var out []string
	switch x {
	case 1:
		out = append(out, "one")
		fallthrough
	case 2:
		out = append(out, "two")
	case 3:
		out = append(out, "three")
	}
	return out
}

func declinedBreak(x int) []string {
	var out []string
	switch x {
	case 1:
		out = append(out, "before")
		if x == 1 {
			break
		}
		out = append(out, "after")
	}
	return out
}

// swap is the case the transpiler got wrong: unrolled in order it leaves both
// variables holding the original b.
func swap(a, b string) []string {
	a, b = b, a
	return []string{a, b}
}

func threeWaySwap(a, b, c int) []int {
	a, b, c = c, a, b
	return []int{a, b, c}
}

// redeclare assigns to the new x while reading the old one.
func redeclare(x int) []int {
	x, y := 1, x
	return []int{x, y}
}

func independent(a, b string) []string {
	a, b = b+"!", "z"
	return []string{a, b}
}

var order []string

func note(s string, v int) int {
	order = append(order, s)
	return v
}

// sideEffects pins down evaluation order, which unrolling must not disturb.
func sideEffects() []string {
	order = nil
	x, y := note("first", 1), note("second", 2)
	_, _ = x, y
	return order
}

func mixedNewAndExisting(existing int) []int {
	existing, fresh := existing+1, 10
	return []int{existing, fresh}
}

func blanks() []int {
	a, _, c := 1, 2, 3
	_, d := 4, 5
	return []int{a, c, d}
}

type pair struct{ a, b string }

// selectors assigns through field selectors, which have no identifier for the
// hazard check to key on.
func selectors() []string {
	p := pair{a: "x", b: "y"}
	p.a, p.b = p.b, p.a
	return []string{p.a, p.b}
}

func hoistedIf(m map[string]int) []int {
	out := []int{}
	if v, ok := m["a"]; ok {
		out = append(out, v)
	}
	return out
}

func hoistedChain(m map[string]int) []int {
	out := []int{}
	if v, ok := m["a"]; ok {
		out = append(out, v, 100)
	} else if v, ok := m["b"]; ok {
		out = append(out, v, 200)
	} else {
		out = append(out, 300)
	}
	return out
}

type box struct {
	m map[string]int
	p *box
}

// nilAndLiterals mixes a hazardous target with values that can't be captured
// into a temporary: `_tmp := nil` doesn't compile.
func nilAndLiterals() []any {
	b := box{m: map[string]int{"k": 1}}
	b.m, b.p = map[string]int{"copied": b.m["k"]}, nil
	return []any{b.m["copied"], b.p == nil}
}

// swapInLoop repeats a swap so a rewrite that reused a temporary across
// iterations would show up.
func swapInLoop() []string {
	a, b := "l", "r"
	for range 3 {
		a, b = b, a
	}
	return []string{a, b}
}
