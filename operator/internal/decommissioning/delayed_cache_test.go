// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/operator/internal/decommissioning"
)

func TestCategorizedDelayedCache(t *testing.T) {
	// we need to be seen 10 times, with at least 1 millisecond between each viewing
	cache := decommissioning.NewCategorizedDelayedCache[int, int](10, time.Millisecond)

	category := 0
	for second := 0; second < 9; second++ {
		for entry := 0; entry < 10; entry++ {
			cache.Mark(category, entry)
			// call Process to make sure nothing is actually getting processed
			processed, err := cache.Process(category, entry, func() error {
				return errors.New("this shouldn't be called")
			})
			require.NoError(t, err)
			require.False(t, processed)
		}
		require.Equal(t, cache.Size(category), 10)
		// sleep 2 milliseconds so that we trip the cache interval
		time.Sleep(2 * time.Millisecond)
	}

	time.Sleep(2 * time.Millisecond)
	for entry := 0; entry < 10; entry++ {
		even := entry%2 == 0
		if even {
			cache.Mark(category, entry)
		}

		processed, err := cache.Process(category, entry, func() error {
			return errors.New("this should error")
		})
		if even {
			require.True(t, processed)
			require.Error(t, err)
		} else {
			require.False(t, processed)
			require.NoError(t, err)
		}

		// doing this should eject the entry from the cache
		processed, err = cache.Process(category, entry, func() error {
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, even, processed)
	}

	require.ElementsMatch(t, []int{1, 3, 5, 7, 9}, slices.Collect(cache.Entries(category)))
	cache.Filter(category, 1, 3, 5)
	require.ElementsMatch(t, []int{1, 3, 5}, slices.Collect(cache.Entries(category)))
	cache.Prune(category, 3, 5)
	require.ElementsMatch(t, []int{1}, slices.Collect(cache.Entries(category)))
	cache.Clean(category)
	require.Equal(t, 0, cache.Size(category))
}
