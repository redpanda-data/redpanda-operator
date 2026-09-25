// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

func TestTieredStorageConfigCreds(t *testing.T) {
	cases := []struct {
		Name     string
		Config   TieredStorageConfig
		Creds    TieredStorageCredentials
		Expected []corev1.EnvVar
	}{
		{
			Name: "azure-secrets",
			Config: TieredStorageConfig{
				"cloud_storage_enabled":               true,
				"cloud_storage_azure_container":       "fake-azure-container",
				"cloud_storage_azure_storage_account": "fake-storage-account",
			},
			Creds: TieredStorageCredentials{
				AccessKey: &SecretRef{},
				SecretKey: &SecretRef{
					Key:  "some-key",
					Name: "some-secret",
				},
			},
			Expected: []corev1.EnvVar{{
				Name: "REDPANDA_CLOUD_STORAGE_AZURE_SHARED_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
						Key:                  "some-key",
					},
				},
			}},
		},
		{
			Name:   "standard-secrets",
			Config: TieredStorageConfig{},
			Creds: TieredStorageCredentials{
				AccessKey: &SecretRef{Name: "access-secret", Key: "access-key"},
				SecretKey: &SecretRef{Name: "secret-secret", Key: "secret-key"},
			},
			Expected: []corev1.EnvVar{{
				Name: "REDPANDA_CLOUD_STORAGE_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
						Key:                  "access-key",
					},
				},
			}, {
				Name: "REDPANDA_CLOUD_STORAGE_SECRET_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret-secret"},
						Key:                  "secret-key",
					},
				},
			}},
		},
		{
			Name: "explicit-precedence",
			Config: TieredStorageConfig{
				"cloud_storage_access_key":            "ACCESS_KEY",
				"cloud_storage_azure_shared_key":      "AZURE_SHARED",
				"cloud_storage_azure_container":       "fake-azure-container",
				"cloud_storage_azure_storage_account": "fake-storage-account",
			},
			Creds: TieredStorageCredentials{
				AccessKey: &SecretRef{Name: "access-secret", Key: "access-key"},
				SecretKey: &SecretRef{Name: "secret-secret", Key: "secret-key"},
			},
		},
		{
			Name: "explicit-precedence-azure",
			Config: TieredStorageConfig{
				"cloud_storage_access_key": "ACCESS_KEY",
				"cloud_storage_secret_key": "SECRET_KEY",
			},
			Creds: TieredStorageCredentials{
				AccessKey: &SecretRef{Name: "access-secret", Key: "access-key"},
				SecretKey: &SecretRef{Name: "secret-secret", Key: "secret-key"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			envvars := tc.Creds.AsEnvVars(tc.Config)
			_, fixups := tc.Config.Translate(&tc.Creds)

			require.EqualValues(t, tc.Expected, envvars)

			// Assert that any envvars have corresponding fixups at the
			// expected keys in the config. See also: [BootstrapFile].
			for _, envar := range envvars {
				key := strings.ToLower(envar.Name[len("REDPANDA_"):])
				require.Contains(t, fixups, clusterconfiguration.Fixup{
					Field: key,
					CEL:   fmt.Sprintf(`repr(envString("%s"))`, envar.Name),
				})
			}
		})
	}
}

func TestInUseServerCerts(t *testing.T) {
	cases := map[string]struct {
		TLS       TLS
		Listeners Listeners
		Expected  []string
	}{
		"internal TLS enabled collects internal cert": {
			TLS: TLS{Enabled: false},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9092,
					TLS: InternalTLS{
						Enabled: ptr.To(true),
						Cert:    "default",
					},
				},
				Admin:          ListenerConfig[NoAuth]{Port: 9644, TLS: InternalTLS{Cert: "default"}},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Cert: "default"}},
			},
			Expected: []string{"default"},
		},
		"internal TLS disabled, external TLS enabled with explicit cert": {
			TLS: TLS{
				Enabled: true,
				Certs: TLSCertMap{
					"default":  TLSCert{},
					"external": TLSCert{},
				},
			},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS: InternalTLS{
						Enabled: ptr.To(false),
						Cert:    "default",
					},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("external"),
							},
						},
					},
				},
				Admin:          ListenerConfig[NoAuth]{Port: 9644, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
			},
			Expected: []string{"external"},
		},
		"both internal and external TLS enabled with different certs": {
			TLS: TLS{
				Enabled: true,
				Certs: TLSCertMap{
					"default":  TLSCert{},
					"external": TLSCert{},
				},
			},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS: InternalTLS{
						Enabled: ptr.To(true),
						Cert:    "default",
					},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("external"),
							},
						},
					},
				},
				Admin:          ListenerConfig[NoAuth]{Port: 9644, TLS: InternalTLS{Cert: "default"}},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Cert: "default"}},
			},
			Expected: []string{"default", "external"},
		},
		"all TLS disabled returns empty": {
			TLS: TLS{Enabled: false},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS: InternalTLS{
						Enabled: ptr.To(false),
						Cert:    "default",
					},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(false),
							},
						},
					},
				},
				Admin:          ListenerConfig[NoAuth]{Port: 9644, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
			},
			Expected: []string{},
		},
		"customer scenario: global TLS on, all internal off, kafka external on with secretRef cert": {
			TLS: TLS{
				Enabled: true,
				Certs: TLSCertMap{
					"default":  TLSCert{},
					"external": TLSCert{CAEnabled: false},
				},
			},
			Listeners: Listeners{
				Admin: ListenerConfig[NoAuth]{
					Port: 9644,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
				},
				HTTP: ListenerConfig[HTTPAuthenticationMethod]{
					Port: 8082,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
				},
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS: InternalTLS{
						Enabled: ptr.To(false),
						Cert:    "default",
					},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("external"),
							},
						},
					},
				},
				SchemaRegistry: ListenerConfig[NoAuth]{
					Port: 8081,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
				},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
			},
			Expected: []string{"external"},
		},
		"multiple listeners with external-only TLS": {
			TLS: TLS{
				Enabled: true,
				Certs: TLSCertMap{
					"default":   TLSCert{},
					"kafka-ext": TLSCert{},
					"admin-ext": TLSCert{},
				},
			},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("kafka-ext"),
							},
						},
					},
				},
				Admin: ListenerConfig[NoAuth]{
					Port: 9644,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
					External: map[string]ExternalListener[NoAuth]{
						"default": {
							Port:    9645,
							Enabled: ptr.To(true),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("admin-ext"),
							},
						},
					},
				},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
			},
			Expected: []string{"admin-ext", "kafka-ext"},
		},
		"disabled external listener should not include its cert": {
			TLS: TLS{Enabled: true},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					TLS:  InternalTLS{Enabled: ptr.To(false), Cert: "default"},
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:    9095,
							Enabled: ptr.To(false),
							TLS: &ExternalTLS{
								Enabled: ptr.To(true),
								Cert:    ptr.To("external"),
							},
						},
					},
				},
				Admin:          ListenerConfig[NoAuth]{Port: 9644, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				HTTP:           ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				SchemaRegistry: ListenerConfig[NoAuth]{Port: 8081, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
				RPC: struct {
					Address *string     `json:"address,omitempty"`
					Port    int32       `json:"port" jsonschema:"required"`
					TLS     InternalTLS `json:"tls" jsonschema:"required"`
				}{Port: 33145, TLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"}},
			},
			Expected: []string{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := tc.Listeners.InUseServerCerts(&tc.TLS)
			assert.Equal(t, tc.Expected, result)
		})
	}
}

func TestExternalTLSIsEnabled(t *testing.T) {
	cases := map[string]struct {
		ExternalTLS *ExternalTLS
		InternalTLS InternalTLS
		GlobalTLS   TLS
		Expected    bool
	}{
		"nil external TLS returns false": {
			ExternalTLS: nil,
			InternalTLS: InternalTLS{Enabled: ptr.To(true), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    false,
		},
		"external explicitly enabled, internal disabled": {
			ExternalTLS: &ExternalTLS{
				Enabled: ptr.To(true),
				Cert:    ptr.To("external"),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    true,
		},
		"external explicitly disabled, internal enabled": {
			ExternalTLS: &ExternalTLS{
				Enabled: ptr.To(false),
				Cert:    ptr.To("external"),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(true), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    false,
		},
		"external not specified, falls back to internal enabled": {
			ExternalTLS: &ExternalTLS{
				Cert: ptr.To("external"),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(true), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    true,
		},
		"external not specified, falls back to internal disabled": {
			ExternalTLS: &ExternalTLS{
				Cert: ptr.To("external"),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    false,
		},
		"external enabled but no cert and internal has empty cert": {
			ExternalTLS: &ExternalTLS{
				Enabled: ptr.To(true),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(false), Cert: ""},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    false,
		},
		"external enabled, cert falls back to internal cert name": {
			ExternalTLS: &ExternalTLS{
				Enabled: ptr.To(true),
			},
			InternalTLS: InternalTLS{Enabled: ptr.To(false), Cert: "default"},
			GlobalTLS:   TLS{Enabled: true},
			Expected:    true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := tc.ExternalTLS.IsEnabled(&tc.InternalTLS, &tc.GlobalTLS)
			assert.Equal(t, tc.Expected, result)
		})
	}
}

func TestRedpandaResources_RedpandaFlags(t *testing.T) {
	cases := []struct {
		Resources RedpandaResources
		Expected  map[string]string
	}{
		{
			Resources: RedpandaResources{
				Limits:   &corev1.ResourceList{},
				Requests: &corev1.ResourceList{},
			},
			Expected: map[string]string{
				"--reserve-memory": "0M", // Always set when Limits && Requests != nil.
				// No other flags set as there's nothing to base them off of (Not recommended).
			},
		},
		{
			// overprovisioned is only set if CPU < 1000m.
			Resources: RedpandaResources{
				Limits: &corev1.ResourceList{},
				Requests: &corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				},
			},
			Expected: map[string]string{
				"--reserve-memory":  "0M", // Always set when Limits && Requests != nil.
				"--smp":             "1",
				"--overprovisioned": "",
			},
		},
		{
			Resources: RedpandaResources{
				Limits: &corev1.ResourceList{},
				Requests: &corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2500m"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
			},
			Expected: map[string]string{
				"--reserve-memory": "0M",
				"--smp":            "2",     // floor(CPU)
				"--memory":         "9216M", // memory * 90%
			},
		},
		{
			// Limits are taken if requests aren't specified.
			Resources: RedpandaResources{
				Limits: &corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("20Gi"),
				},
				Requests: &corev1.ResourceList{},
			},
			Expected: map[string]string{
				"--reserve-memory": "0M",
				"--smp":            "3",      // floor(CPU)
				"--memory":         "18432M", // memory * 90%
			},
		},
		{
			// Showcase that Requests are taken for CLI params in favor of limits.
			Resources: RedpandaResources{
				Limits: &corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("200Gi"),
				},
				Requests: &corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
				},
			},
			Expected: map[string]string{
				"--reserve-memory": "0M",
				"--smp":            "5",      // floor(CPU)
				"--memory":         "92160M", // memory * 90%
			},
		},
	}

	for _, tc := range cases {
		flags := tc.Resources.GetRedpandaFlags()
		assert.Equal(t, tc.Expected, flags)
	}
}
