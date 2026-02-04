// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package fuzzing

import (
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
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

func ClusterSpecConfig() rapid.MakeConfig {
	return rapid.MakeConfig{
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
				// WARNING: we have essentially broken the byte-to-byte compatibility guarantees between v1alpha2 and the v25 helm chart
				// due to how we mapped fields we want to eventually deprecate in the chart to a big pod template field. Because
				// this test goes from chart --> CRD we're going to just nil out the entirety of the pod template since we don't ever
				// do any back conversion in the operator, and only convert CRD --> chart manually.
				"PodTemplate": rapid.Just[any](nil),
			},
			reflect.TypeFor[redpanda.PartialStorage](): {
				"TieredStorageHostPath":         rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"TieredStoragePersistentVolume": rapid.Just[any](nil), // Deprecated field, not worth fixing.
			},
			reflect.TypeFor[redpanda.PartialStatefulset](): {
				"SecurityContext":    rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"PodSecurityContext": rapid.Just[any](nil), // Deprecated field, not worth fixing.
				"UpdateStrategy":     rapid.Just[any](nil),
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
			reflect.TypeFor[corev1.SecretKeySelector](): {
				"Optional": rapid.Just[any](nil),
			},
			reflect.TypeFor[redpanda.PartialClusterConfigValue](): {
				// Work around for divergences in the omitempty behavior of bool vs *bool.
				"UseRawValue": rapid.Just(ptr.To(true)).AsAny(),
				// Work around for a type that we do not control.
				"ExternalSecretRefSelector": rapid.Just[any](nil),
			},
		},
	}
}
