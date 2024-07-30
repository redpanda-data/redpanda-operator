package v1alpha2_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/apiutil"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"
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

func MarshalThrough[T any](data []byte) ([]byte, error) {
	var through T
	if err := json.Unmarshal(data, &through); err != nil {
		return nil, err
	}
	return json.Marshal(through)
}

func AssertJSONCompat[From, To any](t *rapid.T, cfg rapid.MakeConfig, fn func(*From)) {
	var to To
	from := rapid.MakeCustom[From](cfg).Draw(t, "from")

	if fn != nil {
		fn(&from)
	}

	original, err := json.Marshal(from)
	require.NoError(t, err)

	through, err := MarshalThrough[To](original)
	require.NoError(t, err, "failed to marshal %s (%T) through %T", original, from, to)

	require.JSONEq(t, string(original), string(through), "%s (%T) should have serialized to %s (%T)", through, to, original, from)
}

var (
	Quantity = rapid.Custom(func(t *rapid.T) *resource.Quantity {
		return resource.NewQuantity(rapid.Int64().Draw(t, "Quantity"), resource.DecimalSI)
	})

	Duration = rapid.Custom(func(t *rapid.T) metav1.Duration {
		dur := rapid.Int64().Draw(t, "Duration")
		return metav1.Duration{Duration: time.Duration(dur)}
	})
)

// TestHelmValuesCompat asserts that the JSON representation of the redpanda
// cluster spec is byte of byte compatible with the values that the helm chart
// accepts.
func TestHelmValuesCompat(t *testing.T) {
	cfg := rapid.MakeConfig{
		Types: map[reflect.Type]*rapid.Generator[any]{
			reflect.TypeOf(&resource.Quantity{}):       Quantity.AsAny(),
			reflect.TypeOf(metav1.Duration{}):          Duration.AsAny(),
			reflect.TypeOf([]interface{}{}).Elem():     rapid.Just[any](nil), // Return nil for all untyped (any, interface{}) fields.
			reflect.TypeOf(&redpanda.PartialPodSpec{}): rapid.Just[any](nil), // PodSpec's serialization intentionally diverges from PartialPodSpec's so we can leverage builtin types and their validation.
		},
		Fields: map[reflect.Type]map[string]*rapid.Generator[any]{
			reflect.TypeOf(redpanda.PartialValues{}): {
				"Console":           rapid.Just[any](nil), // TODO assert that console's typings are correct.
				"Connectors":        rapid.Just[any](nil), // TODO assert that connectors' typings are correct.
				"CommonAnnotations": rapid.Just[any](nil), // This was accidentally added and shouldn't exist.
			},
			reflect.TypeOf(redpanda.PartialStorage{}): {
				"TieredStorageHostPath":         rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"TieredStoragePersistentVolume": rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeOf(redpanda.PartialStatefulset{}): {
				"SecurityContext":    rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"PodSecurityContext": rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeOf(redpanda.PartialTieredStorageCredentials{}): {
				"ConfigurationKey": rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"Key":              rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"Name":             rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeOf(redpanda.PartialTLSCert{}): {
				// Duration is incorrectly typed as a *string. Ensure it's a valid [metav1.]
				"Duration": rapid.Custom(func(t *rapid.T) *string {
					dur := rapid.Ptr(rapid.Int64(), true).Draw(t, "Duration")
					if dur == nil {
						return nil
					}
					return ptr.To(time.Duration(*dur).String())
				}).AsAny(),
			},
		},
	}

	rapid.Check(t, func(t *rapid.T) {
		AssertJSONCompat[redpanda.PartialValues, v1alpha2.RedpandaClusterSpec](t, cfg, func(from *redpanda.PartialValues) {
			if from.Storage != nil && from.Storage.Tiered != nil && from.Storage.Tiered.PersistentVolume != nil {
				// Incorrect type (should be a *resource.Quantity) on an anonymous struct in Partial Values.
				from.Storage.Tiered.PersistentVolume.Size = nil
			}
		})
	})
}
