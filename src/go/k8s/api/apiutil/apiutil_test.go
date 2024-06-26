package apiutil_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/apiutil"
	"github.com/stretchr/testify/require"
)

func TestJSONBoolean(t *testing.T) {
	for _, tc := range []struct {
		Value    any
		Expected bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"false", false},
		{"invalid", false},
		{map[string]any{}, false},
		{[]int{}, false},
	} {
		raw, err := json.Marshal(tc.Value)
		require.NoError(t, err)

		marshaled, err := json.Marshal(&apiutil.JSONBoolean{Raw: raw})
		require.NoError(t, err)

		require.JSONEq(t, fmt.Sprintf("%v", tc.Expected), string(marshaled))

		// Assert that we can unmarshal into JSONBoolean.
		var b *apiutil.JSONBoolean
		require.NoError(t, json.Unmarshal(marshaled, &b))

		// JSONBoolean's marshal implementation will have coalesced this value
		// into an actual boolean. Assert that we're re-marshaled it as
		// expected.
		require.JSONEq(t, fmt.Sprintf("%v", tc.Expected), string(b.Raw))
	}
}
