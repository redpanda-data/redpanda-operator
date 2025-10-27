// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

func TestStaticConfig(t *testing.T) {
	cases := []struct {
		Name string
		In   ir.StaticConfigurationSource
		Out  PartialRenderValues
	}{
		{
			Name: "empty",
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Redpanda: &PartialRedpanda{},
				}),
			},
		},
		{
			Name: "kafka with TLS",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker-0.svc.cluster.local", "broker-1.svc.cluster.local"},
					TLS: &ir.CommonTLS{
						Key: &ir.SecretKeyRef{Name: "kafka-cert", Key: "tls.key"},
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-cert"},
								Key:                  "ca.crt",
							},
						},
						InsecureSkipTLSVerify: true,
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{
							"broker-0.svc.cluster.local",
							"broker-1.svc.cluster.local",
						},
						TLS: &PartialTLS{
							Enabled:               ptr.To(true),
							InsecureSkipTLSVerify: ptr.To(true),
							CaFilepath:            ptr.To("/etc/tls/certs/secrets/kafka-cert/ca.crt"),
							KeyFilepath:           ptr.To("/etc/tls/certs/secrets/kafka-cert/tls.key"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "kafka-cert",
									},
									Items: []corev1.KeyToPath{
										{Key: "ca.crt", Path: "secrets/kafka-cert/ca.crt"},
										{Key: "tls.key", Path: "secrets/kafka-cert/tls.key"},
									},
								},
							}},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "kafka with SASL",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Username:  "test-user",
						Password:  &ir.SecretKeyRef{Name: "kafka-sasl", Key: "password"},
						Mechanism: "PLAIN",
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"broker:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To("test-user"),
							Mechanism: ptr.To("PLAIN"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{
					Name: "KAFKA_SASL_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kafka-sasl",
							},
							Key: "password",
						},
					},
				}},
			},
		},
		{
			Name: "admin API with TLS and SASL",
			In: ir.StaticConfigurationSource{
				Admin: &ir.AdminAPISpec{
					URLs: []string{"https://admin:9644"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-ca"},
								Key:                  "ca.crt",
							},
						},
						Cert:                  &ir.SecretKeyRef{Name: "admin-cert", Key: "tls.crt"},
						Key:                   &ir.SecretKeyRef{Name: "admin-cert", Key: "tls.key"},
						InsecureSkipTLSVerify: true,
					},
					Auth: &ir.AdminAuth{
						Username: "admin-user",
						Password: ir.SecretKeyRef{Name: "admin-creds", Key: "password"},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled: ptr.To(true),
							URLs:    []string{"https://admin:9644"},
							Authentication: &PartialHTTPAuthentication{
								BasicAuth: &PartialHTTPBasicAuth{
									Username: ptr.To("admin-user"),
								},
							},
							TLS: &PartialTLS{
								Enabled:               ptr.To(true),
								InsecureSkipTLSVerify: ptr.To(true),
								CaFilepath:            ptr.To("/etc/tls/certs/secrets/admin-ca/ca.crt"),
								CertFilepath:          ptr.To("/etc/tls/certs/secrets/admin-cert/tls.crt"),
								KeyFilepath:           ptr.To("/etc/tls/certs/secrets/admin-cert/tls.key"),
							},
						},
					},
				}),
				ExtraEnv: []corev1.EnvVar{{
					Name: "REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "admin-creds",
							},
							Key: "password",
						},
					},
				}},
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "admin-ca",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "secrets/admin-ca/ca.crt"},
										},
									},
								},
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "admin-cert",
										},
										Items: []corev1.KeyToPath{
											{Key: "tls.crt", Path: "secrets/admin-cert/tls.crt"},
											{Key: "tls.key", Path: "secrets/admin-cert/tls.key"},
										},
									},
								},
							},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "schema registry with TLS and SASL",
			In: ir.StaticConfigurationSource{
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"https://schema:8081"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "schema-ca"},
								Key:                  "ca.crt",
							},
						},
					},
					SASL: &ir.SchemaRegistrySASL{
						Username:  "schema-user",
						Password:  ir.SecretKeyRef{Name: "schema-creds", Key: "password"},
						AuthToken: ir.SecretKeyRef{Name: "schema-creds", Key: "token"},
					},
				},
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"kafka:9092"},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"kafka:9092"},
					},
					SchemaRegistry: &PartialSchema{
						Enabled: ptr.To(true),
						URLs:    []string{"https://schema:8081"},
						Authentication: &PartialHTTPAuthentication{
							BasicAuth: &PartialHTTPBasicAuth{
								Username: ptr.To("schema-user"),
							},
						},
						TLS: &PartialTLS{
							Enabled:    ptr.To(true),
							CaFilepath: ptr.To("/etc/tls/certs/secrets/schema-ca/ca.crt"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{
					{
						Name: "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "schema-creds",
								},
								Key: "password",
							},
						},
					},
					{
						Name: "SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "schema-creds",
								},
								Key: "token",
							},
						},
					},
				},
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "schema-ca",
									},
									Items: []corev1.KeyToPath{
										{Key: "ca.crt", Path: "secrets/schema-ca/ca.crt"},
									},
								},
							}},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "schema registry with insecure TLS",
			In: ir.StaticConfigurationSource{
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"https://schema:8081"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "schema-ca"},
								Key:                  "ca.crt",
							},
						},
						InsecureSkipTLSVerify: true,
					},
				},
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"kafka:9092"},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"kafka:9092"},
					},
					SchemaRegistry: &PartialSchema{
						Enabled: ptr.To(true),
						URLs:    []string{"https://schema:8081"},
						TLS: &PartialTLS{
							Enabled:               ptr.To(true),
							InsecureSkipTLSVerify: ptr.To(true),
							CaFilepath:            ptr.To("/etc/tls/certs/secrets/schema-ca/ca.crt"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "schema-ca",
									},
									Items: []corev1.KeyToPath{
										{Key: "ca.crt", Path: "secrets/schema-ca/ca.crt"},
									},
								},
							}},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "complete config",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"kafka-0:9092", "kafka-1:9092"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-tls"},
								Key:                  "ca.crt",
							},
						},
						Cert: &ir.SecretKeyRef{Name: "kafka-tls", Key: "tls.crt"},
						Key:  &ir.SecretKeyRef{Name: "kafka-tls", Key: "tls.key"},
					},
					SASL: &ir.KafkaSASL{
						Username:  "kafka-user",
						Password:  &ir.SecretKeyRef{Name: "kafka-auth", Key: "password"},
						Mechanism: "SCRAM-SHA-256",
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"https://admin:9644"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-tls"},
								Key:                  "ca.crt",
							},
						},
					},
					Auth: &ir.AdminAuth{
						Username: "admin",
						Password: ir.SecretKeyRef{Name: "admin-auth", Key: "password"},
					},
				},
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"https://schema:8081"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "schema-tls"},
								Key:                  "ca.crt",
							},
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"kafka-0:9092", "kafka-1:9092"},
						TLS: &PartialTLS{
							Enabled:      ptr.To(true),
							CaFilepath:   ptr.To("/etc/tls/certs/secrets/kafka-tls/ca.crt"),
							CertFilepath: ptr.To("/etc/tls/certs/secrets/kafka-tls/tls.crt"),
							KeyFilepath:  ptr.To("/etc/tls/certs/secrets/kafka-tls/tls.key"),
						},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To("kafka-user"),
							Mechanism: ptr.To("SCRAM-SHA-256"),
						},
					},
					SchemaRegistry: &PartialSchema{
						Enabled: ptr.To(true),
						URLs:    []string{"https://schema:8081"},
						TLS: &PartialTLS{
							Enabled:    ptr.To(true),
							CaFilepath: ptr.To("/etc/tls/certs/secrets/schema-tls/ca.crt"),
						},
					},
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled: ptr.To(true),
							URLs:    []string{"https://admin:9644"},
							Authentication: &PartialHTTPAuthentication{
								BasicAuth: &PartialHTTPBasicAuth{
									Username: ptr.To("admin"),
								},
							},
							TLS: &PartialTLS{
								Enabled:    ptr.To(true),
								CaFilepath: ptr.To("/etc/tls/certs/secrets/admin-tls/ca.crt"),
							},
						},
					},
				}),
				ExtraEnv: []corev1.EnvVar{
					{
						Name: "KAFKA_SASL_PASSWORD",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-auth"},
								Key:                  "password",
							},
						},
					},
					{
						Name: "REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-auth"},
								Key:                  "password",
							},
						},
					},
				},
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "admin-tls",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "secrets/admin-tls/ca.crt"},
										},
									},
								},
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "kafka-tls",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "secrets/kafka-tls/ca.crt"},
											{Key: "tls.crt", Path: "secrets/kafka-tls/tls.crt"},
											{Key: "tls.key", Path: "secrets/kafka-tls/tls.key"},
										},
									},
								},
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "schema-tls",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "secrets/schema-tls/ca.crt"},
										},
									},
								},
							},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "CA certificates from ConfigMap",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"kafka:9092"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-ca-config"},
								Key:                  "ca.crt",
							},
						},
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"https://admin:9644"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ObjectKeyRef{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-ca-config"},
								Key:                  "ca.crt",
							},
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"kafka:9092"},
						TLS: &PartialTLS{
							Enabled:    ptr.To(true),
							CaFilepath: ptr.To("/etc/tls/certs/configmaps/kafka-ca-config/ca.crt"),
						},
					},
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled: ptr.To(true),
							URLs:    []string{"https://admin:9644"},
							TLS: &PartialTLS{
								Enabled:    ptr.To(true),
								CaFilepath: ptr.To("/etc/tls/certs/configmaps/admin-ca-config/ca.crt"),
							},
						},
					},
				}),
				ExtraVolumes: []corev1.Volume{{
					Name: "redpanda-certificates",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{
								{
									ConfigMap: &corev1.ConfigMapProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "admin-ca-config",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "configmaps/admin-ca-config/ca.crt"},
										},
									},
								},
								{
									ConfigMap: &corev1.ConfigMapProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "kafka-ca-config",
										},
										Items: []corev1.KeyToPath{
											{Key: "ca.crt", Path: "configmaps/kafka-ca-config/ca.crt"},
										},
									},
								},
							},
						},
					},
				}},
				ExtraVolumeMounts: []corev1.VolumeMount{{
					Name:      "redpanda-certificates",
					MountPath: "/etc/tls/certs",
				}},
			},
		},
		{
			Name: "nil values handling",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"kafka:9092"},
					SASL: &ir.KafkaSASL{
						Username: "user",
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"admin:9644"},
				},
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"schema:8081"},
					SASL: &ir.SchemaRegistrySASL{
						Username:  "schema-user",
						Password:  ir.SecretKeyRef{},
						AuthToken: ir.SecretKeyRef{},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"kafka:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To("user"),
							Mechanism: ptr.To(""),
						},
					},
					SchemaRegistry: &PartialSchema{
						Enabled: ptr.To(true),
						URLs:    []string{"schema:8081"},
						Authentication: &PartialHTTPAuthentication{
							BasicAuth: &PartialHTTPBasicAuth{
								Username: ptr.To("schema-user"),
							},
						},
					},
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled: ptr.To(true),
							URLs:    []string{"admin:9644"},
						},
					},
				}),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			values := StaticConfigurationSourceToPartialRenderValues(&tc.In)

			require.Equal(t, tc.Out, values)
		})
	}
}

func TestConfigMapper_addEnv(t *testing.T) {
	mapper := &configMapper{}

	// Test with valid secret ref
	mapper.addEnv("TEST_VAR", ir.SecretKeyRef{
		Name: "test-secret",
		Key:  "test-key",
	})

	require.Len(t, mapper.Env, 1)
	require.Equal(t, "TEST_VAR", mapper.Env[0].Name)
	require.Equal(t, "test-secret", mapper.Env[0].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, "test-key", mapper.Env[0].ValueFrom.SecretKeyRef.Key)

	// Test with empty secret ref (should not add)
	mapper.addEnv("EMPTY_VAR", ir.SecretKeyRef{})
	require.Len(t, mapper.Env, 1)

	// Test with empty name
	mapper.addEnv("EMPTY_NAME", ir.SecretKeyRef{Key: "key"})
	require.Len(t, mapper.Env, 1)

	// Test with empty key
	mapper.addEnv("EMPTY_KEY", ir.SecretKeyRef{Name: "name"})
	require.Len(t, mapper.Env, 1)
}

func TestVolumes_MaybeAdd(t *testing.T) {
	v := &volumes{
		Name:       "test-vol",
		Dir:        "/test/dir",
		Secrets:    make(map[string]map[string]bool),
		ConfigMaps: make(map[string]map[string]bool),
	}

	// Test with nil ref
	result := v.MaybeAdd(nil)
	require.Nil(t, result)
	require.Empty(t, v.Secrets)

	// Test with valid ref
	result = v.MaybeAdd(&ir.ObjectKeyRef{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "secret-name"},
			Key:                  "secret-key",
		},
	})
	require.NotNil(t, result)
	require.Equal(t, "/test/dir/secrets/secret-name/secret-key", *result)
	require.Contains(t, v.Secrets, "secret-name")
	require.Equal(t, map[string]bool{"secret-key": true}, v.Secrets["secret-name"])

	// Test adding another key to same secret
	result2 := v.MaybeAdd(&ir.ObjectKeyRef{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "secret-name"},
			Key:                  "another-key",
		},
	})
	require.NotNil(t, result2)
	require.Equal(t, "/test/dir/secrets/secret-name/another-key", *result2)
	require.Equal(t, map[string]bool{"secret-key": true, "another-key": true}, v.Secrets["secret-name"])

	// Test with ConfigMap reference
	result3 := v.MaybeAdd(&ir.ObjectKeyRef{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "config-name"},
			Key:                  "config-key",
		},
	})
	require.NotNil(t, result3)
	require.Equal(t, "/test/dir/configmaps/config-name/config-key", *result3)
	require.Contains(t, v.ConfigMaps, "config-name")
	require.Equal(t, map[string]bool{"config-key": true}, v.ConfigMaps["config-name"])
}

func TestVolumes_VolumeMounts(t *testing.T) {
	v := &volumes{
		Name:       "test-vol",
		Dir:        "/test/dir",
		Secrets:    make(map[string]map[string]bool),
		ConfigMaps: make(map[string]map[string]bool),
	}

	// Test with no secrets
	result := v.VolumeMounts()
	require.Nil(t, result)

	// Test with secrets
	v.Secrets["secret1"] = map[string]bool{"key1": true}
	result = v.VolumeMounts()
	require.Len(t, result, 1)
	require.Equal(t, "test-vol", result[0].Name)
	require.Equal(t, "/test/dir", result[0].MountPath)
}

func TestVolumes_Volumes(t *testing.T) {
	v := &volumes{
		Name:       "test-vol",
		Dir:        "/test/dir",
		Secrets:    make(map[string]map[string]bool),
		ConfigMaps: make(map[string]map[string]bool),
	}

	// Test with no secrets
	result := v.Volumes()
	require.Nil(t, result)

	// Test with secrets
	v.Secrets["secret1"] = map[string]bool{"key1": true, "key2": true}
	v.Secrets["secret2"] = map[string]bool{"key3": true}
	result = v.Volumes()

	require.Len(t, result, 1)
	require.Equal(t, "test-vol", result[0].Name)
	require.NotNil(t, result[0].VolumeSource.Projected)
}
