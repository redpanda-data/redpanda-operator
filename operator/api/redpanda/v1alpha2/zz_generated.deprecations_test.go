// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

// TODO: The following fields are documented as deprecated in the API comments
// but their Go field names do not use the `Deprecated` prefix. Consider
// renaming them (or adding wrapper fields with the Deprecated prefix) so the
// reflective deprecation detector can find them uniformly. Examples found in
// this package:
//
// - Redpanda:
//   - ClusterSpec.Console
//   - ClusterSpec.Connectors
//   - ClusterSpec.Connectors.Test.Enabled
//   - ClusterSpec.Logging.UsageStats.Organization
//   - ClusterSpec.Storage.Tiered.Config.CloudStorageReconciliationIntervalMs
//   - ClusterSpec.PostInstallJob.SecurityContext
//   - ClusterSpec.PostUpgradeJob.SecurityContext
//   - ClusterSpec.Statefulset.SideCars.ConfigWatcher.ExtraVolumeMounts
//   - ClusterSpec.Statefulset.SideCars.ConfigWatcher.Resources
//   - ClusterSpec.Statefulset.SideCars.ConfigWatcher.SecurityContext
//   - ClusterSpec.Statefulset.SideCars.RpkStatus.Resources
//   - ClusterSpec.Statefulset.SideCars.RpkStatus.SecurityContext
//   - ClusterSpec.Statefulset.SideCars.Controllers.Resources
//   - ClusterSpec.Statefulset.SideCars.Controllers.SecurityContext
//   - ClusterSpec.Listeners.HTTP.KafkaEndpoint
//   - ClusterSpec.Listeners.RPC.TLS.SecretRef
//   - ClusterSpec.Listeners.SchemaRegistry.KafkaEndpoint
// - Topic:
//   - KafkaAPISpec

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/deprecations"
)

func TestDeprecatedFieldWarnings(t *testing.T) {
	tests := []struct {
		name         string
		obj          client.Object
		wantWarnings []string
	}{
		{
			name: "Console",
			obj: &Console{
				Spec: ConsoleSpec{
					ClusterSource: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.cluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
			},
		},
		{
			name: "Redpanda",
			obj: &Redpanda{
				Spec: RedpandaSpec{
					ClusterSpec: ptr.To(RedpandaClusterSpec{
						Console: ptr.To(RedpandaConsole{
							DeprecatedConfigMap:  ptr.To(ConsoleCreateObj{}),
							DeprecatedConsole:    ptr.To(runtime.RawExtension{}),
							DeprecatedEnterprise: ptr.To(runtime.RawExtension{}),
							DeprecatedTests:      ptr.To(DeprecatedEnablable{}),
						}),
						DeprecatedFullNameOverride: "deprecated",
						DeprecatedLicenseKey:       ptr.To("deprecated"),
						DeprecatedLicenseSecretRef: ptr.To(LicenseSecretRef{}),
						DeprecatedTests:            ptr.To(DeprecatedEnablable{}),
					}),
					DeprecatedMigration: ptr.To(DeprecatedMigration{}),
				},
			},
			wantWarnings: []string{
				"field 'spec.clusterSpec.fullNameOverride' is deprecated and set",
				"field 'spec.clusterSpec.console.configmap' is deprecated and set",
				"field 'spec.clusterSpec.console.console' is deprecated and set",
				"field 'spec.clusterSpec.console.enterprise' is deprecated and set",
				"field 'spec.clusterSpec.console.tests' is deprecated and set",
				"field 'spec.clusterSpec.license_key' is deprecated and set",
				"field 'spec.clusterSpec.license_secret_ref' is deprecated and set",
				"field 'spec.clusterSpec.tests' is deprecated and set",
				"field 'spec.migration' is deprecated and set",
			},
		},
		{
			name: "RedpandaRole",
			obj: &RedpandaRole{
				Spec: RoleSpec{
					ClusterSource: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.cluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
			},
		},
		{
			name: "Schema",
			obj: &Schema{
				Spec: SchemaSpec{
					ClusterSource: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.cluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
			},
		},
		{
			name: "ShadowLink",
			obj: &ShadowLink{
				Spec: ShadowLinkSpec{
					ShadowCluster: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
					SourceCluster: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.shadowCluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.shadowCluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.sourceCluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
			},
		},
		{
			name: "Topic",
			obj: &Topic{
				Spec: TopicSpec{
					ClusterSource: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
					KafkaAPISpec: ptr.To(KafkaAPISpec{
						SASL: ptr.To(KafkaSASL{
							AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
								DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
								DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
							}),
							DeprecatedPassword: ptr.To(SecretKeyRef{}),
							GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
								DeprecatedPassword: ptr.To(SecretKeyRef{}),
							}),
							OAUth: ptr.To(KafkaSASLOAuthBearer{
								DeprecatedToken: ptr.To(SecretKeyRef{}),
							}),
						}),
						TLS: ptr.To(CommonTLS{
							DeprecatedCaCert: ptr.To(SecretKeyRef{}),
							DeprecatedCert:   ptr.To(SecretKeyRef{}),
							DeprecatedKey:    ptr.To(SecretKeyRef{}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.cluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
				"field 'spec.kafkaApiSpec.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.tls.certSecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.tls.keySecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.kafkaApiSpec.sasl.passwordSecretRef' is deprecated and set",
			},
		},
		{
			name: "User",
			obj: &User{
				Spec: UserSpec{
					ClusterSource: ptr.To(ClusterSource{
						StaticConfiguration: ptr.To(StaticConfigurationSource{
							Admin: ptr.To(AdminAPISpec{
								SASL: ptr.To(AdminSASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							Kafka: ptr.To(KafkaAPISpec{
								SASL: ptr.To(KafkaSASL{
									AWSMskIam: ptr.To(KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    ptr.To(SecretKeyRef{}),
										DeprecatedSessionToken: ptr.To(SecretKeyRef{}),
									}),
									DeprecatedPassword: ptr.To(SecretKeyRef{}),
									GSSAPIConfig: ptr.To(KafkaSASLGSSAPI{
										DeprecatedPassword: ptr.To(SecretKeyRef{}),
									}),
									OAUth: ptr.To(KafkaSASLOAuthBearer{
										DeprecatedToken: ptr.To(SecretKeyRef{}),
									}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
							SchemaRegistry: ptr.To(SchemaRegistrySpec{
								SASL: ptr.To(SchemaRegistrySASL{
									DeprecatedAuthToken: ptr.To(SecretKeyRef{}),
									DeprecatedPassword:  ptr.To(SecretKeyRef{}),
								}),
								TLS: ptr.To(CommonTLS{
									DeprecatedCaCert: ptr.To(SecretKeyRef{}),
									DeprecatedCert:   ptr.To(SecretKeyRef{}),
									DeprecatedKey:    ptr.To(SecretKeyRef{}),
								}),
							}),
						}),
					}),
				},
			},
			wantWarnings: []string{
				"field 'spec.cluster.staticConfiguration.kafka.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.oauth.tokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.gssapi.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.secretKeySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.awsMskIam.sessionTokenSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.kafka.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.admin.sasl.token' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.caCertSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.certSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.tls.keySecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.passwordSecretRef' is deprecated and set",
				"field 'spec.cluster.staticConfiguration.schemaRegistry.sasl.token' is deprecated and set",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			warnings, err := deprecations.FindDeprecatedFieldWarnings(tc.obj)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.wantWarnings, warnings)
		})
	}
}
