// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package feature

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollGrantRoundTrip(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(RollGrantTTL).Truncate(time.Second)
	grant := FormatRollGrant("abc123", deadline)

	checksum, parsed, ok := ParseRollGrant(grant)
	require.True(t, ok)
	assert.Equal(t, "abc123", checksum)
	assert.True(t, parsed.Equal(deadline))
}

func TestParseRollGrantMalformed(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",             // empty
		"abc123",       // no separator
		"/1234",        // empty checksum
		"abc123/",      // empty deadline
		"abc123/later", // non-numeric deadline
	}
	for _, in := range cases {
		_, _, ok := ParseRollGrant(in)
		assert.Falsef(t, ok, "expected %q to be rejected", in)
	}
}

func TestParseRollGrantChecksumWithSlash(t *testing.T) {
	t.Parallel()

	// The checksum must not contain "/" for the format to round-trip. Hex
	// checksums never do; anything else fails the deadline parse and is
	// rejected rather than silently truncated.
	_, _, ok := ParseRollGrant("abc/def/1234")
	assert.False(t, ok)
}
