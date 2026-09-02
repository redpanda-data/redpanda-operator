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
