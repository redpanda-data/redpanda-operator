package v1alpha1_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
)

func TestCloudStorageEnabledBool(t *testing.T) {
	testCases := []struct {
		In  []byte
		Out *string
	}{
		{In: []byte(`true`), Out: ptr.To("true")},
		{In: []byte(`false`), Out: ptr.To("false")},
		{In: []byte(`"true"`), Out: ptr.To("true")},
		{In: []byte(`"false"`), Out: ptr.To("false")},
		{In: []byte(`null`), Out: nil},
	}

	for _, tc := range testCases {
		var b *v1alpha1.BoolString

		require.NoError(t, json.Unmarshal(tc.In, &b))
		require.Equal(t, (*v1alpha1.BoolString)(tc.Out), b, "%q did not unmarshal to %s", tc.In, tc.Out)
	}

	// Also assert that invalid values result in errors.
	var b *v1alpha1.BoolString
	require.Error(t, json.Unmarshal([]byte(`"notabool"`), &b))
}

func TestConvertFromRoundTrips(t *testing.T) {
	// Ideally we'd use quick check here but a for loop is a lot cleaner and
	// provides the same result.

	for i := 0; i < 50; i++ {
		var hub v1alpha2.Redpanda

		CRDFuzzer().Fuzz(&hub)

		var spoke v1alpha1.Redpanda
		require.NoError(t, spoke.ConvertFrom(&hub))

		var roundTripped v1alpha2.Redpanda
		require.NoError(t, spoke.ConvertTo(&roundTripped))

		// Assert that the JSON respresentation of hub (v1alpha2) is the same
		// after being convered to and from a spoke (v1alpha1).
		// NB: JSONEq is used here because resource.Quantity's struct
		// representation is not stable.
		require.JSONEq(t, string(MustMarshal(t, hub)), string(MustMarshal(t, roundTripped)))
	}
}

func TestConvertToRoundTrips(t *testing.T) {
	// Ideally we'd use quick check here but a for loop is a lot cleaner and
	// provides the same result.
	for i := 0; i < 50; i++ {
		var spoke v1alpha1.Redpanda

		CRDFuzzer().Fuzz(&spoke)

		var hub v1alpha2.Redpanda
		require.NoError(t, spoke.ConvertTo(&hub))

		var roundTripped v1alpha1.Redpanda
		require.NoError(t, roundTripped.ConvertFrom(&hub))

		// Assert that the JSON respresentation of spoke (v1alpha1) is the same
		// after being convered to and from the hub (v1alpha2).
		// NB: JSONEq is used here because resource.Quantity's struct
		// representation is not stable.
		require.JSONEq(t, string(MustMarshal(t, spoke)), string(MustMarshal(t, roundTripped)))
	}
}

func TestConvertFrom(t *testing.T) {
	for _, value := range []*v1alpha2.CloudStorageEnabledBool{
		nil,
		ptr.To(v1alpha2.CloudStorageEnabledBool(true)),
		ptr.To(v1alpha2.CloudStorageEnabledBool(false)),
	} {
		t.Run(fmt.Sprintf("cloud-enabled-%v", value), func(t *testing.T) {
			var spoke v1alpha1.Redpanda
			hub := newV1alpha2(value)

			require.NoError(t, spoke.ConvertFrom(hub))

			newRJSON, err := json.Marshal(hub)
			require.NoError(t, err)

			oldRJSON, err := json.Marshal(spoke)
			require.NoError(t, err)

			oldRJSONStr := strings.ReplaceAll(string(oldRJSON), "\"cloud_storage_enabled\":\"true\"", "\"cloud_storage_enabled\":true")
			oldRJSONStr = strings.ReplaceAll(oldRJSONStr, "\"cloud_storage_enabled\":\"false\"", "\"cloud_storage_enabled\":false")

			require.JSONEq(t, string(newRJSON), oldRJSONStr)
		})
	}
}

func TestConvertTo(t *testing.T) {
	for _, value := range []*v1alpha1.BoolString{
		nil,
		ptr.To(v1alpha1.BoolString("true")),
		ptr.To(v1alpha1.BoolString("false")),
	} {
		t.Run(fmt.Sprintf("cloud-enabled-%v", value), func(t *testing.T) {
			newR := &v1alpha2.Redpanda{}

			oldR := newV1alpha1(value)
			err := oldR.ConvertTo(newR)
			require.NoError(t, err)

			oldRJSON, err := json.Marshal(oldR)
			require.NoError(t, err)

			newRJSON, err := json.Marshal(newR)
			require.NoError(t, err)

			oldRJSONStr := strings.ReplaceAll(string(oldRJSON), "\"cloud_storage_enabled\":\"true\"", "\"cloud_storage_enabled\":true")
			oldRJSONStr = strings.ReplaceAll(oldRJSONStr, "\"cloud_storage_enabled\":\"false\"", "\"cloud_storage_enabled\":false")
			if value != nil {
				oldRJSONStr = strings.ReplaceAll(oldRJSONStr, fmt.Sprintf("\"cloud_storage_enabled\":\"%s\"", *value), "\"cloud_storage_enabled\":false")
			}

			require.JSONEq(t, string(newRJSON), oldRJSONStr)
		})
	}
}

func newV1alpha2(flag *v1alpha2.CloudStorageEnabledBool) *v1alpha2.Redpanda { // nolint:dupl // v1alpha1 is not the same as v1alpha2
	var r v1alpha2.Redpanda

	CRDFuzzer().Fuzz(&r)

	if r.Spec.ClusterSpec == nil {
		r.Spec.ClusterSpec = &v1alpha2.RedpandaClusterSpec{}
	}

	if r.Spec.ClusterSpec.Storage == nil {
		r.Spec.ClusterSpec.Storage = &v1alpha2.Storage{}
	}

	if r.Spec.ClusterSpec.Storage.Tiered == nil {
		r.Spec.ClusterSpec.Storage.Tiered = &v1alpha2.Tiered{}
	}

	if r.Spec.ClusterSpec.Storage.Tiered.Config == nil {
		r.Spec.ClusterSpec.Storage.Tiered.Config = &v1alpha2.TieredConfig{}
	}

	r.Spec.ClusterSpec.Storage.Tiered.Config.CloudStorageEnabled = flag

	return &r
}

func newV1alpha1(flag *v1alpha1.BoolString) *v1alpha1.Redpanda { // nolint:dupl // v1alpha1 is not the same as v1alpha2
	var r v1alpha1.Redpanda

	CRDFuzzer().Fuzz(&r)

	if r.Spec.ClusterSpec == nil {
		r.Spec.ClusterSpec = &v1alpha1.RedpandaClusterSpec{}
	}

	if r.Spec.ClusterSpec.Storage == nil {
		r.Spec.ClusterSpec.Storage = &v1alpha1.Storage{}
	}

	if r.Spec.ClusterSpec.Storage.Tiered == nil {
		r.Spec.ClusterSpec.Storage.Tiered = &v1alpha1.Tiered{}
	}

	if r.Spec.ClusterSpec.Storage.Tiered.Config == nil {
		r.Spec.ClusterSpec.Storage.Tiered.Config = &v1alpha1.TieredConfig{}
	}

	r.Spec.ClusterSpec.Storage.Tiered.Config.CloudStorageEnabled = flag

	return &r
}

func MustMarshal(t *testing.T, obj any) []byte {
	out, err := json.Marshal(obj)
	require.NoError(t, err)
	return out
}

func CRDFuzzer() *fuzz.Fuzzer {
	return fuzz.New().Funcs(
		// Custom fuzzer for BoolString to ensure it's always a boolean.
		func(boolStr *v1alpha1.BoolString, c fuzz.Continue) { // nolint:staticcheck // Fuzzing is weird, we're assigning to a pointer to pass the value out.
			switch c.Intn(3) {
			case 0:
				boolStr = nil // nolint:ineffassign,staticcheck // Fuzzing is weird, we're assigning to a pointer to pass the value out.
			case 1:
				boolStr = ptr.To(v1alpha1.BoolString("true")) // nolint:ineffassign,staticcheck // Fuzzing is weird, we're assigning to a pointer to pass the value out.
			case 3:
				boolStr = ptr.To(v1alpha1.BoolString("false")) // nolint:ineffassign,staticcheck // Fuzzing is weird, we're assigning to a pointer to pass the value out.
			default:
				panic("Unknown case!")
			}
		},
		// Custom fuzzer for resource.Quantity as it's internal representation
		// is... Scary.
		func(q *resource.Quantity, c fuzz.Continue) { // nolint:ineffassign,staticcheck // Fuzzing is weird, we're assigning to a pointer to pass the value out.
			q = resource.NewQuantity(c.Int63(), resource.DecimalSI)
		},
	).SkipFieldsWithPattern(regexp.MustCompile("Console|Config|Connectors|FieldsV1"))
}
