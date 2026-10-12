// Copyright 2026 Redpanda Data, Inc.
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
						Key: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-cert"}, Key: "tls.key"},
						},
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-cert"}, Key: "ca.crt"},
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
						Username: "test-user",
						Password: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-sasl"},
								Key:                  "password",
							},
						},
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
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-ca"}, Key: "ca.crt"},
						},
						Cert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-cert"}, Key: "tls.crt"},
						},
						Key: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-cert"}, Key: "tls.key"},
						},
						InsecureSkipTLSVerify: true,
					},
					Auth: &ir.AdminAuth{
						Username: "admin-user",
						Password: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-creds"},
								Key:                  "password",
							},
						},
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
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "schema-ca"}, Key: "ca.crt"},
						},
					},
					SASL: &ir.SchemaRegistrySASL{
						Username: "schema-user",
						Password: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "schema-creds"},
								Key:                  "password",
							},
						},
						AuthToken: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "schema-creds"},
								Key:                  "token",
							},
						},
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
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "schema-ca"}, Key: "ca.crt"},
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
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-tls"}, Key: "ca.crt"},
						},
						Cert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-tls"}, Key: "tls.crt"},
						},
						Key: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-tls"}, Key: "tls.key"},
						},
					},
					SASL: &ir.KafkaSASL{
						Username: "kafka-user",
						Password: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-auth"},
								Key:                  "password",
							},
						},
						Mechanism: "SCRAM-SHA-256",
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"https://admin:9644"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-tls"}, Key: "ca.crt"},
						},
					},
					Auth: &ir.AdminAuth{
						Username: "admin",
						Password: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "admin-auth"},
								Key:                  "password",
							},
						},
					},
				},
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"https://schema:8081"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ValueSource{
							SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "schema-tls"}, Key: "ca.crt"},
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
						CaCert: &ir.ValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-ca-config"}, Key: "ca.crt"},
						},
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"https://admin:9644"},
					TLS: &ir.CommonTLS{
						CaCert: &ir.ValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-ca-config"}, Key: "ca.crt"},
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
						Password: &ir.ValueSource{},
					},
				},
				Admin: &ir.AdminAPISpec{
					URLs: []string{"admin:9644"},
				},
				SchemaRegistry: &ir.SchemaRegistrySpec{
					URLs: []string{"schema:8081"},
					SASL: &ir.SchemaRegistrySASL{
						Username:  "schema-user",
						Password:  &ir.ValueSource{},
						AuthToken: &ir.ValueSource{},
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
		{
			Name: "kafka with SASL password inline",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Username:  "test-user",
						Password:  &ir.ValueSource{Inline: ptr.To("hunter2")},
						Mechanism: "SCRAM-SHA-256",
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
							Mechanism: ptr.To("SCRAM-SHA-256"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "KAFKA_SASL_PASSWORD", Value: "hunter2"}},
			},
		},
		{
			Name: "kafka with SASL password from a ConfigMap",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Username: "test-user",
						Password: &ir.ValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-sasl"},
								Key:                  "password",
							},
						},
						Mechanism: "SCRAM-SHA-512",
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
							Mechanism: ptr.To("SCRAM-SHA-512"),
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{
					Name: "KAFKA_SASL_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "kafka-sasl"},
							Key:                  "password",
						},
					},
				}},
			},
		},
		{
			Name: "kafka with SASL OAUTHBEARER",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Mechanism: "OAUTHBEARER",
						OAUth: &ir.KafkaSASLOAuthBearer{
							Token: &ir.ValueSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "oauth"},
									Key:                  "token",
								},
							},
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"broker:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To(""),
							Mechanism: ptr.To("OAUTHBEARER"),
							OAUth:     &PartialKafkaSASLOAuthBearer{},
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{
					Name: "KAFKA_SASL_OAUTH_TOKEN",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "oauth"},
							Key:                  "token",
						},
					},
				}},
			},
		},
		{
			Name: "kafka with SASL GSSAPI",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Mechanism: "GSSAPI",
						GSSAPIConfig: &ir.KafkaSASLGSSAPI{
							AuthType:           "USER_AUTH",
							KerberosConfigPath: "/etc/krb5.conf",
							ServiceName:        "redpanda",
							Username:           "console",
							Realm:              "EXAMPLE.COM",
							Password:           &ir.ValueSource{Inline: ptr.To("kerberos-password")},
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"broker:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To(""),
							Mechanism: ptr.To("GSSAPI"),
							GSSAPIConfig: &PartialKafkaSASLGSSAPI{
								AuthType:           ptr.To("USER_AUTH"),
								KeyTabPath:         ptr.To(""),
								KerberosConfigPath: ptr.To("/etc/krb5.conf"),
								ServiceName:        ptr.To("redpanda"),
								Username:           ptr.To("console"),
								Realm:              ptr.To("EXAMPLE.COM"),
								EnableFast:         ptr.To(false),
							},
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "KAFKA_SASL_GSSAPI_PASSWORD", Value: "kerberos-password"}},
			},
		},
		{
			Name: "kafka with SASL GSSAPI and FAST enabled",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Mechanism: "GSSAPI",
						GSSAPIConfig: &ir.KafkaSASLGSSAPI{
							AuthType:           "USER_AUTH",
							KerberosConfigPath: "/etc/krb5.conf",
							ServiceName:        "redpanda",
							Username:           "console",
							Realm:              "EXAMPLE.COM",
							Password:           &ir.ValueSource{Inline: ptr.To("kerberos-password")},
							EnableFast:         true,
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"broker:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To(""),
							Mechanism: ptr.To("GSSAPI"),
							GSSAPIConfig: &PartialKafkaSASLGSSAPI{
								AuthType:           ptr.To("USER_AUTH"),
								KeyTabPath:         ptr.To(""),
								KerberosConfigPath: ptr.To("/etc/krb5.conf"),
								ServiceName:        ptr.To("redpanda"),
								Username:           ptr.To("console"),
								Realm:              ptr.To("EXAMPLE.COM"),
								EnableFast:         ptr.To(true),
							},
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "KAFKA_SASL_GSSAPI_PASSWORD", Value: "kerberos-password"}},
			},
		},
		{
			Name: "kafka with SASL AWS_MSK_IAM",
			In: ir.StaticConfigurationSource{
				Kafka: &ir.KafkaAPISpec{
					Brokers: []string{"broker:9092"},
					SASL: &ir.KafkaSASL{
						Mechanism: "AWS_MSK_IAM",
						AWSMskIam: &ir.KafkaSASLAWSMskIam{
							AccessKey: "AKIAEXAMPLE",
							SecretKey: &ir.ValueSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "aws"},
									Key:                  "secret-key",
								},
							},
							SessionToken: &ir.ValueSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "aws"},
									Key:                  "session-token",
								},
							},
							UserAgent: "console",
						},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Kafka: &PartialKafka{
						Brokers: []string{"broker:9092"},
						SASL: &PartialKafkaSASL{
							Enabled:   ptr.To(true),
							Username:  ptr.To(""),
							Mechanism: ptr.To("AWS_MSK_IAM"),
							AWSMskIam: &PartialKafkaSASLAwsMskIam{
								AccessKey: ptr.To("AKIAEXAMPLE"),
								UserAgent: ptr.To("console"),
							},
						},
					},
					Redpanda: &PartialRedpanda{},
				}),
				ExtraEnv: []corev1.EnvVar{
					{
						Name: "KAFKA_SASL_AWSMSKIAM_SECRETKEY",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "aws"},
								Key:                  "secret-key",
							},
						},
					},
					{
						Name: "KAFKA_SASL_AWSMSKIAM_SESSIONTOKEN",
						ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "aws"},
								Key:                  "session-token",
							},
						},
					},
				},
			},
		},
		{
			Name: "admin API with a bearer token",
			In: ir.StaticConfigurationSource{
				Admin: &ir.AdminAPISpec{
					URLs: []string{"http://admin:9644"},
					Auth: &ir.AdminAuth{
						AuthToken: &ir.ValueSource{Inline: ptr.To("admin-token")},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled:        ptr.To(true),
							URLs:           []string{"http://admin:9644"},
							Authentication: &PartialHTTPAuthentication{},
						},
					},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "REDPANDA_ADMINAPI_AUTHENTICATION_BEARERTOKEN", Value: "admin-token"}},
			},
		},
		{
			// Console picks basic auth whenever a username is set, so a token
			// must not come with a username, and a username without a password
			// must not suppress the token.
			Name: "admin API with a bearer token and a username",
			In: ir.StaticConfigurationSource{
				Admin: &ir.AdminAPISpec{
					URLs: []string{"http://admin:9644"},
					Auth: &ir.AdminAuth{
						Username:  "ignored-user",
						AuthToken: &ir.ValueSource{Inline: ptr.To("admin-token")},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled:        ptr.To(true),
							URLs:           []string{"http://admin:9644"},
							Authentication: &PartialHTTPAuthentication{},
						},
					},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "REDPANDA_ADMINAPI_AUTHENTICATION_BEARERTOKEN", Value: "admin-token"}},
			},
		},
		{
			Name: "admin API with a password and a bearer token",
			In: ir.StaticConfigurationSource{
				Admin: &ir.AdminAPISpec{
					URLs: []string{"http://admin:9644"},
					Auth: &ir.AdminAuth{
						Username:  "admin-user",
						Password:  &ir.ValueSource{Inline: ptr.To("admin-password")},
						AuthToken: &ir.ValueSource{Inline: ptr.To("admin-token")},
					},
				},
			},
			Out: PartialRenderValues{
				Config: helmette.UnmarshalInto[map[string]any](PartialConfig{
					Redpanda: &PartialRedpanda{
						AdminAPI: &PartialRedpandaAdminAPI{
							Enabled: ptr.To(true),
							URLs:    []string{"http://admin:9644"},
							Authentication: &PartialHTTPAuthentication{
								BasicAuth: &PartialHTTPBasicAuth{Username: ptr.To("admin-user")},
							},
						},
					},
				}),
				ExtraEnv: []corev1.EnvVar{{Name: "REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD", Value: "admin-password"}},
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
	mapper.addEnv("TEST_VAR", &ir.ValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
			Key:                  "test-key",
		},
	})

	require.Len(t, mapper.Env, 1)
	require.Equal(t, "TEST_VAR", mapper.Env[0].Name)
	require.Equal(t, "test-secret", mapper.Env[0].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, "test-key", mapper.Env[0].ValueFrom.SecretKeyRef.Key)

	// Test with empty secret ref (should not add)
	mapper.addEnv("EMPTY_VAR", &ir.ValueSource{})
	require.Len(t, mapper.Env, 1)

	// Test with empty name
	mapper.addEnv("EMPTY_NAME", &ir.ValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			Key: "key",
		},
	})
	require.Len(t, mapper.Env, 1)

	// Test with empty key
	mapper.addEnv("EMPTY_KEY", &ir.ValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "name"},
		},
	})
	require.Len(t, mapper.Env, 1)

	// Test with nil source (should not add)
	mapper.addEnv("NIL_VAR", nil)
	require.Len(t, mapper.Env, 1)

	// Test with an inline value
	mapper.addEnv("INLINE_VAR", &ir.ValueSource{Inline: ptr.To("inline-value")})
	require.Len(t, mapper.Env, 2)
	require.Equal(t, corev1.EnvVar{Name: "INLINE_VAR", Value: "inline-value"}, mapper.Env[1])

	// Test with a ConfigMap ref
	mapper.addEnv("CONFIGMAP_VAR", &ir.ValueSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
			Key:                  "test-key",
		},
	})
	require.Len(t, mapper.Env, 3)
	require.Equal(t, "CONFIGMAP_VAR", mapper.Env[2].Name)
	require.Equal(t, "test-configmap", mapper.Env[2].ValueFrom.ConfigMapKeyRef.Name)
	require.Equal(t, "test-key", mapper.Env[2].ValueFrom.ConfigMapKeyRef.Key)

	// Test with an external secret, which cannot be resolved here (should
	// not add)
	mapper.addEnv("EXTERNAL_VAR", &ir.ValueSource{
		ExternalSecretRefSelector: &ir.ExternalSecretKeySelector{Name: "external"},
	})
	require.Len(t, mapper.Env, 3)
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
	result = v.MaybeAdd(&ir.ValueSource{
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
	result2 := v.MaybeAdd(&ir.ValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "secret-name"},
			Key:                  "another-key",
		},
	})
	require.NotNil(t, result2)
	require.Equal(t, "/test/dir/secrets/secret-name/another-key", *result2)
	require.Equal(t, map[string]bool{"secret-key": true, "another-key": true}, v.Secrets["secret-name"])

	// Test with ConfigMap reference
	result3 := v.MaybeAdd(&ir.ValueSource{
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
