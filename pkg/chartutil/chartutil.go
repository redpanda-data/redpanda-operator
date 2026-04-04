// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// package chartutil is a collection of gotohelm friendly utility functions.
// These functions help provide consistent behaviors and experiences across the
// charts we maintains.
package chartutil

import (
	"regexp"
	"strings"
)

// ParseFlags parses a list of CLI flags into a map of flag name -> value. It
// should be used to merge user provided flags with chart defaults.
//
// e.g. []string{`--foo=""`, "--bar=1", "-t"} -> map[string]string{"--foo": `""`, "--bar": "1", "-t": ""}
//
// It does not distinguish between flags with values of an empty string and
// flags with no values. e.g. `--value` and `--value=` -> map[string]string{}
//
// Values without preceding -'s are considered invalid and skipped. e.g.
// `[]string{"true"}` -> `map[string]string{}`.
func ParseFlags(args []string) map[string]string {
	parsed := map[string]string{}

	// NB: templates/gotohelm don't supported c style for loops (or ++) which
	// is the ideal for this situation. The janky code you see is a rough
	// equivalent for the following:
	// for i := 0; i < len(args); i++ {
	i := -1          // Start at -1 so our increment can be at the start of the loop.
	for range args { // Range needs to range over something and we'll always have < len(args) iterations.
		i = i + 1
		if i >= len(args) {
			break
		}

		// All flags should start with - or --.
		// If not present, skip this value.
		if !strings.HasPrefix(args[i], "-") {
			continue
		}

		flag := args[i]

		// Handle values like: `--flag value` or `--flag=value`
		// There's no strings.Index in sprig, so RegexSplit is the next best
		// option.
		spl := regexSplit(" |=", flag, 2)
		if len(spl) == 2 {
			parsed[spl[0]] = spl[1]
			continue
		}

		// If no ' ' or =, consume the next value if it's not formatted like a
		// flag: `--flag`, `value`
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			parsed[flag] = args[i+1]
			i = i + 1
			continue
		}

		// Otherwise, assume this is a bare flag and assign it an empty string.
		parsed[flag] = ""
	}

	return parsed
}

// Rather than depend on helmette, which would cause a circular dependency, we
// provide our own sprig stubs.

// +gotohelm:builtin=mustRegexSplit
func regexSplit(pattern, s string, n int) []string {
	r := regexp.MustCompile(pattern)
	return r.Split(s, n)
}
