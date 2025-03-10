package helmette

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortedMap(t *testing.T) {
	var keys []string
	for key := range SortedMap(map[string]bool{
		"0": true,
		"z": true,
		"a": true,
	}) {
		keys = append(keys, key)
	}

	require.Equal(t, []string{"0", "a", "z"}, keys)
}
