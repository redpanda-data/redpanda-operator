// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/console"
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/operator/api/apiutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/fuzzing"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestFullNameOverride(t *testing.T) {
	tcs := []struct {
		name             string
		rp               redpandav1alpha2.Redpanda
		expectedFullname string
		expectedJSON     []byte
	}{
		{
			name: "Deprecated full name only",
			rp: redpandav1alpha2.Redpanda{
				Spec: redpandav1alpha2.RedpandaSpec{
					ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
						DeprecatedFullNameOverride: "deprecated",
						FullnameOverride:           nil,
					},
				},
			},
			expectedFullname: "deprecated",
			expectedJSON:     []byte("{\"fullnameOverride\":\"deprecated\"}"),
		},
		{
			name: "Full name only",
			rp: redpandav1alpha2.Redpanda{
				Spec: redpandav1alpha2.RedpandaSpec{
					ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
						DeprecatedFullNameOverride: "",
						FullnameOverride:           ptr.To("fullname"),
					},
				},
			},
			expectedFullname: "fullname",
			expectedJSON:     []byte("{\"fullnameOverride\":\"fullname\"}"),
		},
		{
			name: "Both full name set",
			rp: redpandav1alpha2.Redpanda{
				Spec: redpandav1alpha2.RedpandaSpec{
					ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
						DeprecatedFullNameOverride: "deprecated",
						FullnameOverride:           ptr.To("fullname-wins"),
					},
				},
			},
			expectedFullname: "fullname-wins",
			expectedJSON:     []byte("{\"fullnameOverride\":\"fullname-wins\"}"),
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.rp.Spec.ClusterSpec.DeepCopy())
			require.NoError(t, err)
			require.Equal(t, tc.expectedJSON, b)

			dot, err := tc.rp.GetDot(&rest.Config{})
			require.NoError(t, err)
			require.Equal(t, tc.expectedFullname, dot.Values["fullnameOverride"].(string))
		})
	}
}

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

		rp := redpandav1alpha2.Redpanda{
			Spec: redpandav1alpha2.RedpandaSpec{
				ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
					Storage: &redpandav1alpha2.Storage{
						Tiered: &redpandav1alpha2.Tiered{
							Config: &redpandav1alpha2.TieredConfig{
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

// TestHelmValuesCompat asserts that the JSON representation of the redpanda
// cluster spec is byte of byte compatible with the values that the helm chart
// accepts.
func TestHelmValuesCompat(t *testing.T) {
	t.Run("clusterSpec", rapid.MakeCheck(func(t *rapid.T) {
		AssertJSONCompat[redpanda.PartialValues, redpandav1alpha2.RedpandaClusterSpec](t, fuzzing.ClusterSpecConfig(), func(from *redpanda.PartialValues) {
			if from.Storage != nil && from.Storage.Tiered != nil && from.Storage.Tiered.PersistentVolume != nil {
				// Incorrect type (should be a *resource.Quantity) on an anonymous struct in Partial Values.
				from.Storage.Tiered.PersistentVolume.Size = nil
			}
		})
	}))

	t.Run("console", rapid.MakeCheck(func(t *rapid.T) {
		AssertJSONCompat[console.PartialValues, redpandav1alpha2.RedpandaConsole](t, cfg, nil)
	}))
}

func TestClusterSpecBackwardsCompat(t *testing.T) {
	env := testutils.RedpandaTestEnv{}
	cfg, err := env.StartRedpandaTestEnv(false)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, env.Stop())
	})

	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	cr := unstructured.Unstructured{Object: make(map[string]interface{})}
	cr.SetNamespace("default")
	cr.SetName("namespace-selector")
	cr.SetGroupVersionKind(redpandav1alpha2.SchemeGroupVersion.WithKind("Redpanda"))

	// Create a minimal redpanda CR with an extranious field in the namespace
	// selector (which was previously mistyped as a map[string]any).
	require.NoError(t, unstructured.SetNestedMap(cr.Object, map[string]any{
		"any":        true,
		"not a real": "field",
	}, "spec", "clusterSpec", "connectors", "monitoring", "namespaceSelector"))

	// Assert that our CR is considered valid by the K8s API.
	require.NoError(t, c.Create(context.Background(), &cr))

	// Assert that kube APIServer did NOT remove the extra field, showcasing
	// that preserve unknown fields is respected.
	// NB: It's unlikely that this behavior will be observable within a "real"
	// cluster as the operator will very likely strip off the extra fields when
	// unmarshaling the field. This is acceptable as we're looking for
	// compatibility at the API Server level and any removed fields would have
	// never been read anyways.
	rtd, _, err := unstructured.NestedFieldCopy(cr.Object, "spec", "clusterSpec", "connectors", "monitoring", "namespaceSelector")
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"any":        true,
		"not a real": "field",
	}, rtd)
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

func TestNoMarkdownLinks(t *testing.T) {
	data, err := os.ReadFile("testdata/crd-docs.adoc")
	require.NoError(t, err)

	re := regexp.MustCompile(`\[(.+)\]\((.+)\)`)

	matches := re.FindAllSubmatch(data, -1)
	for _, match := range matches {
		t.Errorf("public CRD docs use Ascii doc but found markdown link: %s\nDid you mean: %s[%s]\n(Or do you need to run task generate?)", match[0], match[2], match[1])
	}
}

func TestGetValues(t *testing.T) {
	rp := redpandav1alpha2.Redpanda{
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Auth: &redpandav1alpha2.Auth{
					SASL: &redpandav1alpha2.SASL{
						Enabled: ptr.To(true),
					},
				},
			},
		},
	}

	values, err := rp.GetValues()
	require.NoError(t, err)

	require.Equal(t, values.Auth.SASL.Enabled, true)
}
