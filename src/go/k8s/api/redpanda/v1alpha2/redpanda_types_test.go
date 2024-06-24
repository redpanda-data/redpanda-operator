package v1alpha2_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/apiutil"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
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

func AssertJSONCompat[From, To any](t *testing.T, fuzzer *fuzz.Fuzzer) {
	for i := 0; i < 10; i++ {
		var from From
		fuzzer.Fuzz(&from)

		original, err := json.Marshal(from)
		require.NoError(t, err)

		through, err := MarshalThrough[To](original)
		require.NoError(t, err)

		require.JSONEq(t, string(original), string(through))
	}
}

// TestHelmValuesCompat asserts that the JSON representation of the redpanda
// cluster spec is byte of byte compatible with the values that the helm chart
// accepts.
func TestHelmValuesCompat(t *testing.T) {
	// There are integer sizing mismatches, so clamp all ints to 32
	// bits.
	intTruncation := func(n *int, c fuzz.Continue) { //nolint:staticcheck // fuzzing is weird
		v := int(c.Int31())
		*n = v
	}

	// Makes strings easier to read.
	asciiStrs := func(s *string, c fuzz.Continue) {
		var x []byte
		for i := 0; i < c.Intn(20); i++ {
			// Ascii range for printable characters is [32,126].
			x = append(x, byte(32+c.Intn(127-32)))
		}
		*s = string(x)
	}

	t.Run("helm2crd", func(t *testing.T) {
		t.Skipf("Too many issues to currently be useful")

		fuzzer := fuzz.New().NilChance(0.5).Funcs(
			asciiStrs,
			intTruncation,
			func(a *any, c fuzz.Continue) {},
		)
		AssertJSONCompat[redpanda.PartialValues, v1alpha2.RedpandaClusterSpec](t, fuzzer)
	})

	t.Run("crd2helm", func(t *testing.T) {
		disabledFields := []string{
			"Connectors",       // Untyped in the CRD.
			"Console",          // Untyped in the CRD.
			"External",         // .Type is missing omitempty due to issues in genpartial.
			"Force",            // Missing from Helm
			"FullNameOverride", // Incorrectly cased in the CRD's JSON tag (Should be fullnameOverride). Would be a breaking change to fix.
			"ImagePullSecrets", // Missing from Helm.
			"Listeners",        // CRD uses homogeneous types for all listeners which can cause divergences.
			"Logging",          // Disabled due to issues in genpartial.
			"Monitoring",       // Disabled due to issues in genpartial.
			"PostInstallJob",   // Incorrectly typed in Helm.
			"PostUpgradeJob",   // Incorrectly typed in Helm.
			"Resources",        // Multiple issues, at least one due to genpartial.
			"Service",          // Disabled due to issues in genpartial.
			"Statefulset",      // Many divergences from helm. Needs further inspection.
			"Storage",          // Helm is missing nameOverwrite
			"Tuning",           // Disabled due to extraVolumeMounts being typed as a string in helm.

			// Deprecated fields (IE fields that shouldn't be in the CRD but
			// aren't removed for backwards compat).
			"Organization", // UsageStats
		}

		fuzzer := fuzz.New().NilChance(0.5).SkipFieldsWithPattern(
			regexp.MustCompile("^("+strings.Join(disabledFields, "|")+"$)"),
		).Funcs(
			asciiStrs,
			intTruncation,
			// There are some untyped values as runtime.RawExtension's within
			// the CRD. We can't really generate those, so skip over them for
			// now. Omitting this function will call .Fuzz to panic.
			func(e **runtime.RawExtension, c fuzz.Continue) {},
			// Ensure that certificates are never nil. There's a mild divergence
			// in how the values unmarshal which is irrelevant to this test.
			func(cert *v1alpha2.Certificate, c fuzz.Continue) {
				c.Fuzz(cert)
			},
		)
		AssertJSONCompat[v1alpha2.RedpandaClusterSpec, redpanda.PartialValues](t, fuzzer)
	})
}
