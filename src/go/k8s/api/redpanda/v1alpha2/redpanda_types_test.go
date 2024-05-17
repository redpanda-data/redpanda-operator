package v1alpha2_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/apiutil"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
)

// TestRedpanda_ValuesJSON asserts that .ValuesJSON appropriately coalesces the
// value of CloudStorageEnabled into a boolean.
// NOTE: This test is close to being a duplicate of apiutil.JSONBoolean's tests
// but this test assures us that ValuesJSON is appropriately utilizing
// JSONBoolean's marshaling. Can't be too careful at this point.
func TestRedpanda_ValuesJSON(t *testing.T) {
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
		rawValue, err := json.Marshal(tc.Value)
		require.NoError(t, err)

		t.Logf("%s", rawValue)

		rp := v1alpha2.Redpanda{
			Spec: v1alpha2.RedpandaSpec{
				ClusterSpec: &v1alpha2.RedpandaClusterSpec{
					Storage: &v1alpha2.Storage{
						Tiered: &v1alpha2.Tiered{
							Config: &v1alpha2.TieredConfig{
								CloudStorageEnabled: &apiutil.JSONBoolean{Raw: rawValue},
							},
						},
					},
				},
			},
		}

		require.NoError(t, json.Unmarshal(rawValue, &rp.Spec.ClusterSpec.Storage.Tiered.Config.CloudStorageEnabled))

		valuesJSON, err := rp.ValuesJSON()
		require.NoError(t, err)

		expected := fmt.Sprintf(`{"storage":{"tiered":{"config":{"cloud_storage_enabled":%v}}}}`, tc.Expected)
		require.JSONEq(t, expected, string(valuesJSON.Raw))
	}
}
