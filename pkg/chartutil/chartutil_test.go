// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chartutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func TestParseFlags(t *testing.T) {
	tcs := []struct {
		In  []string
		Out map[string]string
	}{
		{In: nil, Out: map[string]string{}},
		{In: []string{}, Out: map[string]string{}},
		{In: []string{"-t"}, Out: map[string]string{"-t": ""}},
		// Invalid values are skipped.
		{In: []string{"t"}, Out: map[string]string{}},
		{
			In: []string{"-t", "--v", "1", "--foo=bar", "-hello='world'"},
			Out: map[string]string{
				"-t":     "",
				"--v":    "1",
				"--foo":  "bar",
				"-hello": "'world'",
			},
		},
		{
			In: []string{"-v 1", "invalid", "--foo", "baz"},
			Out: map[string]string{
				"-v":    "1",
				"--foo": "baz",
			},
		},
		{
			In: []string{
				"-foo=bar",
				"-baz 1",
				"--help", "topic",
			},
			Out: map[string]string{
				"-foo":   "bar",
				"-baz":   "1",
				"--help": "topic",
			},
		},
		{
			In: []string{
				"invalid",
				"-valid=bar",
				"--trailing spaces ",
				"--bare=",
				"ignored-perhaps-confusingly",
				"--final",
			},
			Out: map[string]string{
				"-valid":     "bar",
				"--trailing": "spaces ",
				"--bare":     "",
				"--final":    "",
			},
		},
	}

	for _, tc := range tcs {
		actual := ParseFlags(tc.In)
		assert.Equal(t, tc.Out, actual, "%#v parsed incorrect", tc.In)
	}

	t.Run("NotPanics", rapid.MakeCheck(func(t *rapid.T) {
		// We could certainly be more inventive with
		// the inputs here but this is more of a
		// fuzz test than a property test.
		in := rapid.SliceOf(rapid.String()).Draw(t, "input")

		assert.NotPanics(t, func() {
			ParseFlags(in)
		})
	}))
}
