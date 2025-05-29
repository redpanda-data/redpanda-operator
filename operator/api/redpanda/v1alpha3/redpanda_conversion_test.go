package v1alpha3_test

import (
	"maps"
	"reflect"
	"slices"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestConversion(t *testing.T) {
	asciiString := rapid.StringOf(rapid.RuneFrom(nil, unicode.ASCII_Hex_Digit))
	yamlRepr := rapid.Custom(func(t *rapid.T) redpandav1alpha3.YAMLRepresentation {
		value := rapid.OneOf(
			rapid.Just[any](nil),
			rapid.Float32().AsAny(),
			rapid.Int32().AsAny(),
			asciiString.AsAny(),
			// TODO(chrisseto) some fun issues with YAML escaping pop up when
			// allowing maps or rapid.Int here.
			// rapid.MapOf(asciiString, asciiString).AsAny(),
		).Draw(t, "Value")

		out, err := yaml.Marshal(value)
		if err != nil {
			panic(err)
		}

		return redpandav1alpha3.YAMLRepresentation(out)
	})

	nonDynamicConfig := rapid.MapOf(rapid.StringMatching(`[a-z0-9_-]{1,15}`), rapid.Custom(func(t *rapid.T) redpandav1alpha3.ValueSource {
		value := yamlRepr.Draw(t, "Value")
		return redpandav1alpha3.ValueSource{Value: &value}
	}))

	cfg := rapid.MakeConfig{
		Kinds: extend(rapidutil.KubernetesTypes.Kinds, map[reflect.Kind]*rapid.Generator[any]{
			// Many of our Int32 types should technically be Uint32s but we use
			// Int32 to match Kubernetes' APIs. The redpanda helm chart uses <
			// 0 checks as an additional "is enabled" check which makes this
			// conversion quite difficult to test. For simplicity, we exclude
			// such cases as they're exceptionally unlikely to be utilized in
			// practice.
			reflect.Int32: rapid.Int32Min(0).AsAny(),
		}),
		Types: extend(rapidutil.KubernetesTypes.Types, map[reflect.Type]*rapid.Generator[any]{
			reflect.TypeFor[redpandav1alpha3.YAMLRepresentation](): yamlRepr.AsAny(),
			reflect.TypeFor[redpandav1alpha3.CertificateSource](): rapid.MakeCustom[redpandav1alpha3.CertificateSource](rapidutil.KubernetesTypes).Filter(func(src redpandav1alpha3.CertificateSource) bool {
				// Require exactly one of CertificateSource's fields to be specified.
				switch {
				case src.CertManager != nil && src.IssuerRef == nil && src.Secrets == nil:
					return true
				case src.CertManager == nil && src.IssuerRef != nil && src.Secrets == nil:
					return true
				case src.CertManager == nil && src.IssuerRef == nil && src.Secrets != nil:
					return true
				default:
					return false
				}
			}).AsAny(),
		}),
		Fields: extend(rapidutil.KubernetesTypes.Fields, map[reflect.Type]map[string]*rapid.Generator[any]{
			reflect.TypeFor[corev1.ResourceRequirements](): {
				"Claims": rapid.Just[any](nil), // Claims may be specified via podTemplate.
			},
			reflect.TypeFor[redpandav1alpha3.Listener](): {
				"Port": rapid.Int32Min(1).AsAny(),
				"AdvertisedPorts": rapid.Custom(func(t *rapid.T) any {
					ports := rapid.SliceOf(rapid.Int32Min(0)).Draw(t, "AdvertisedPorts")
					// Returning an empty slice will result in test failures due to "omitempty" turning []T{} into []T(nil).
					// To work around this, we turn any empty slices into nil slices.
					if len(ports) == 0 {
						return nil
					}
					return ports
				}),
			},
			reflect.TypeFor[redpandav1alpha3.BrokerTemplate](): {
				"NodeConfig": nonDynamicConfig.AsAny(),
				"RPKConfig":  nonDynamicConfig.AsAny(),
			},
			reflect.TypeFor[redpandav1alpha3.LicenseValueFrom](): {
				"SecretKeyRef": rapid.Custom(func(t *rapid.T) any {
					return corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: rapid.StringMatching("^[a-z]{1,10}$").Draw(t, "Value"),
						},
						Key: rapid.StringMatching("^[a-z]{1,10}$").Draw(t, "Value"),
					}
				}),
			},
		}),
	}

	t.Run("Through v1alpha2", rapid.MakeCheck(func(t *rapid.T) {
		original := rapid.MakeCustom[redpandav1alpha3.Redpanda](cfg).Draw(t, "original")

		if original.Spec.NodePoolSpec.BrokerTemplate.NodeConfig == nil {
			original.Spec.NodePoolSpec.BrokerTemplate.NodeConfig = map[string]redpandav1alpha3.ValueSource{}
		}

		original.Spec.NodePoolSpec.BrokerTemplate.NodeConfig["crash_loop_limit"] = redpandav1alpha3.ValueSource{
			Value: (*redpandav1alpha3.YAMLRepresentation)(ptr.To("5\n")),
		}

		preprocessListeners(&original.Spec.Listeners.Admin, redpandav1alpha3.Listener{
			Name: "internal",
			Port: 9644,
			TLS: &redpandav1alpha3.ListenerTLS{
				CertificateSource: redpandav1alpha3.CertificateSource{
					CertManager: &redpandav1alpha3.CertManagerCertificateSource{},
				},
			},
		})

		preprocessListeners(&original.Spec.Listeners.Kafka, redpandav1alpha3.Listener{
			Name: "internal",
			Port: 9093,
			TLS: &redpandav1alpha3.ListenerTLS{
				CertificateSource: redpandav1alpha3.CertificateSource{
					CertManager: &redpandav1alpha3.CertManagerCertificateSource{},
				},
			},
		})

		preprocessListeners(&original.Spec.Listeners.SchemaRegistry, redpandav1alpha3.Listener{
			Name: "internal",
			Port: 8081,
			TLS: &redpandav1alpha3.ListenerTLS{
				CertificateSource: redpandav1alpha3.CertificateSource{
					CertManager: &redpandav1alpha3.CertManagerCertificateSource{},
				},
			},
		})

		preprocessListeners(&original.Spec.Listeners.HTTP, redpandav1alpha3.Listener{
			Name: "internal",
			Port: 8082,
			TLS: &redpandav1alpha3.ListenerTLS{
				CertificateSource: redpandav1alpha3.CertificateSource{
					CertManager: &redpandav1alpha3.CertManagerCertificateSource{},
				},
			},
		})

		var old redpandav1alpha2.Redpanda
		require.NoError(t, original.ConvertTo(&old))

		var roundtripped redpandav1alpha3.Redpanda
		require.NoError(t, roundtripped.ConvertFrom(&old))

		testutil.AssertEqual(t, original, roundtripped)
	}))

	// v1alpha2 -> v1alpha3 -> v1alpha2 is another beast entirely and will be
	// implemented in a follow up PR.
	// t.Run("Through v1alpha3", rapid.MakeCheck(func(t *rapid.T) {
	// 	original := rapid.MakeCustom[v1alpha2.Redpanda](rapidutil.KubernetesTypes).Draw(t, "original")
	//
	// 	var intermediate v1alpha3.Redpanda
	// 	require.NoError(t, intermediate.ConvertFrom(&original))
	//
	// 	var roundtripped v1alpha2.Redpanda
	// 	require.NoError(t, intermediate.ConvertTo(&roundtripped))
	//
	// 	if !assert.Equal(t, original, roundtripped) {
	// 		printDiff(t, original, intermediate, roundtripped)
	// 	}
	// }))
}

func preprocessListeners(listeners *[]redpandav1alpha3.Listener, internalDefault redpandav1alpha3.Listener) {
	// rapid doesn't have the context to know that listener names should be
	// unique. So we enforce it here.
	byName := make(map[string]redpandav1alpha3.Listener, len(*listeners))
	for _, l := range *listeners {
		byName[l.Name] = l
	}

	// Sort alphabetically. The ensures stability for comparisons as
	// we're going between a map and slice.
	unique := make([]redpandav1alpha3.Listener, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		unique = append(unique, byName[name])
	}

	*listeners = unique

	// Finally, if an internal listener isn't present, inject one to match the
	// structure of v1alpha2.
	// This is intentionally excluded from the sorting as we want the internal
	// listener to be the first entry.
	// TODO(chrisseto): This should be moved into a defaulting webhook.
	if !slices.ContainsFunc(*listeners, func(l redpandav1alpha3.Listener) bool {
		return l.Name == "internal"
	}) {
		*listeners = slices.Insert(*listeners, 0, internalDefault)
	}
}

func extend[K comparable, V any](a, b map[K]V) map[K]V {
	clone := make(map[K]V, len(a)+len(b))
	maps.Copy(clone, a)
	maps.Copy(clone, b)
	return clone
}
