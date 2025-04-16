// Copyright 2025 Redpanda Data, Inc.
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
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/connectors"
	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/operator/api/apiutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

var (
	Quantity = rapid.Custom(func(t *rapid.T) *resource.Quantity {
		return resource.NewQuantity(rapid.Int64().Draw(t, "Quantity"), resource.DecimalSI)
	})

	Duration = rapid.Custom(func(t *rapid.T) metav1.Duration {
		dur := rapid.Int64().Draw(t, "Duration")
		return metav1.Duration{Duration: time.Duration(dur)}
	})

	IntOrString = rapid.Custom(func(t *rapid.T) intstr.IntOrString {
		if rapid.Bool().Draw(t, "intorstr") {
			return intstr.FromInt32(rapid.Int32().Draw(t, "FromInt32"))
		} else {
			return intstr.FromString(rapid.StringN(0, 10, 10).Draw(t, "FromString"))
		}
	})

	Probe = rapid.Custom(func(t *rapid.T) corev1.Probe {
		return corev1.Probe{
			InitialDelaySeconds: rapid.Int32Min(1).Draw(t, "InitialDelaySeconds"),
			FailureThreshold:    rapid.Int32Min(1).Draw(t, "FailureThreshold"),
			PeriodSeconds:       rapid.Int32Min(1).Draw(t, "PeriodSeconds"),
			TimeoutSeconds:      rapid.Int32Min(1).Draw(t, "TimeoutSeconds"),
			SuccessThreshold:    rapid.Int32Min(1).Draw(t, "SuccessThreshold"),
		}
	})

	Time = rapid.Custom(func(t *rapid.T) metav1.Time {
		// As metav1.Time will unmarshal 0 into "null", we need to ensure that
		// we never generate a 0 time here otherwise JSON serialization will be
		// non-idempotent and break out tests.
		nsec := rapid.Int64Min(1).Draw(t, "Time")
		return metav1.Time{Time: time.Unix(0, nsec)}
	})
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
			helmChartValues := (*redpandav1alpha2.RedpandaClusterSpecAlt)(tc.rp.Spec.ClusterSpec.DeepCopy())
			b, err := json.Marshal(helmChartValues)
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
	t.Skip(`
	Tbis is currently skipped due to the mismatch with the CRD and the
	movement in the Helm chart to leveraging a top-level PodTemplate. Please unskip
	the test once CRD marshaling takes into account the top-level PodTemplate, which
	should happen before 25.1.1 is marked as a GA.
	`)
	cfg := rapid.MakeConfig{
		Types: map[reflect.Type]*rapid.Generator[any]{
			reflect.TypeFor[intstr.IntOrString]():        IntOrString.AsAny(),
			reflect.TypeFor[*resource.Quantity]():        Quantity.AsAny(),
			reflect.TypeFor[metav1.Duration]():           Duration.AsAny(),
			reflect.TypeFor[metav1.Time]():               Time.AsAny(),
			reflect.TypeFor[any]():                       rapid.Just[any](nil), // Return nil for all untyped (any, interface{}) fields.
			reflect.TypeFor[*metav1.FieldsV1]():          rapid.Just[any](nil), // Return nil for K8s accounting fields.
			reflect.TypeFor[corev1.Probe]():              Probe.AsAny(),        // We use the Probe type to simplify typing but it's serialization isn't fully "partial" which is acceptable.
			reflect.TypeFor[*redpanda.PartialSidecars](): rapid.Just[any](nil), // Intentionally not included in the operator as the operator handles this itself.
		},
		Fields: map[reflect.Type]map[string]*rapid.Generator[any]{
			reflect.TypeFor[redpanda.PartialValues](): {
				"Console":           rapid.Just[any](nil), // Asserted in their own test.
				"Connectors":        rapid.Just[any](nil), // Asserted in their own test.
				"CommonAnnotations": rapid.Just[any](nil), // This was accidentally added and shouldn't exist.
			},
			reflect.TypeFor[redpanda.PartialStorage](): {
				"TieredStorageHostPath":         rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"TieredStoragePersistentVolume": rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeFor[redpanda.PartialStatefulset](): {
				"SecurityContext":    rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"PodSecurityContext": rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeFor[redpanda.PartialTieredStorageCredentials](): {
				"ConfigurationKey": rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"Key":              rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"Name":             rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeFor[redpanda.PartialTLSCert](): {
				// Duration is incorrectly typed as a *string. Ensure it's a valid [metav1.]
				"Duration": rapid.Custom(func(t *rapid.T) *string {
					dur := rapid.Ptr(rapid.Int64(), true).Draw(t, "Duration")
					if dur == nil {
						return nil
					}
					return ptr.To(time.Duration(*dur).String())
				}).AsAny(),
			},
			reflect.TypeFor[redpanda.PartialBootstrapUser](): {
				"Password": rapid.Just[any](nil), // This field is intentionally not documented or added to the CRD
			},
			reflect.TypeFor[redpanda.PartialServiceAccountCfg](): {
				"AutomountServiceAccountToken": rapid.Just[any](nil),
			},
		},
	}

	t.Run("clusterSpec", rapid.MakeCheck(func(t *rapid.T) {
		AssertJSONCompat[redpanda.PartialValues, redpandav1alpha2.RedpandaClusterSpec](t, cfg, func(from *redpanda.PartialValues) {
			if from.Storage != nil && from.Storage.Tiered != nil && from.Storage.Tiered.PersistentVolume != nil {
				// Incorrect type (should be a *resource.Quantity) on an anonymous struct in Partial Values.
				from.Storage.Tiered.PersistentVolume.Size = nil
			}
		})
	}))

	t.Run("connectors", rapid.MakeCheck(func(t *rapid.T) {
		AssertJSONCompat[connectors.PartialValues, redpandav1alpha2.RedpandaConnectors](t, cfg, nil)
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
	cr.SetGroupVersionKind(redpandav1alpha2.GroupVersion.WithKind("Redpanda"))

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
