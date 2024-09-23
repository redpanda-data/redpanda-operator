// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package collections

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testSetImplementation(t *testing.T, set Set[string]) {
	// Test Add, HasAny, HasAll, Size
	require.Equal(t, 0, set.Size())

	set.Add("1", "2", "3")

	require.Equal(t, 3, set.Size())
	require.True(t, set.HasAll("1", "2", "3"))
	require.False(t, set.HasAll("1", "2", "3", "4"))
	require.True(t, set.HasAny("1", "2", "3", "4"))
	require.False(t, set.HasAny("4"))

	// Test Delete
	set.Delete("3")

	require.Equal(t, 2, set.Size())
	require.False(t, set.HasAny("3"))

	// Test Clone and Values
	other := set.Clone()

	require.Equal(t, other.Size(), set.Size())
	require.True(t, other.HasAll(set.Values()...))
	require.True(t, set.HasAll(other.Values()...))

	// Test Clear
	set.Clear()

	require.Equal(t, 0, set.Size())
	require.False(t, set.HasAny("1"))

	// Test Merging
	other.Clear()
	set.Add("1")
	other.Add("2")
	set.Merge(other)

	require.Equal(t, 2, set.Size())
	require.True(t, set.HasAll("1", "2"))

	// Test *Disjoint, Union, and Insersection
	other.Clear()
	set.Clear()

	set.Add("1", "2", "3")
	other.Add("3", "4", "5")

	left := set.LeftDisjoint(other)
	right := set.RightDisjoint(other)
	intersect := set.Intersection(other)
	union := set.Union(other)

	require.Equal(t, 2, left.Size())
	require.True(t, left.HasAll("1", "2"), "Set did not contain 1, 2 as expected %+v", left.Values())

	require.Equal(t, 2, right.Size())
	require.True(t, right.HasAll("4", "5"), "Set did not contain 4, 5 as expected %+v", right.Values())

	require.Equal(t, 1, intersect.Size())
	require.True(t, intersect.HasAll("3"), "Set did not contain 3 as expected %+v", intersect.Values())

	require.Equal(t, 5, union.Size())
	require.True(t, union.HasAll("1", "2", "3", "4", "5"), "Set did not contain 1, 2, 3, 4, 5 as expected %+v", union.Values())
}

func TestSets(t *testing.T) {
	for name, set := range map[string]Set[string]{
		"SingleThreaded": NewSet[string](),
		"Concurrent":     NewConcurrentSet[string](),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testSetImplementation(t, set)
		})
	}
}
