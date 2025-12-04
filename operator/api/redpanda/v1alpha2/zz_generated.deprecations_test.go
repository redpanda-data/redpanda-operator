package v1alpha2

// TODO: The following fields are documented as deprecated in the API comments
// but their Go field names do not use the `Deprecated` prefix. Consider
// renaming them (or adding wrapper fields with the Deprecated prefix) so the
// reflective deprecation detector can find them uniformly. Examples found in
// this package:
//
// - Redpanda:
//   - Migration
//   - ClusterSpec.LicenseKey
//   - ClusterSpec.LicenseSecretRef
//   - ClusterSpec.Console
//   - ClusterSpec.Connectors
//   - ClusterSpec.Console.Console
//   - ClusterSpec.Console.Enterprise
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
					ClusterSource: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
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
					ClusterSpec: &RedpandaClusterSpec{
						Console: &RedpandaConsole{
							DeprecatedConfigMap: &ConsoleCreateObj{},
						},
						DeprecatedFullNameOverride: "deprecated",
					},
				},
			},
			wantWarnings: []string{
				"field 'spec.clusterSpec.fullNameOverride' is deprecated and set",
				"field 'spec.clusterSpec.console.configmap' is deprecated and set",
			},
		},
		{
			name: "RedpandaRole",
			obj: &RedpandaRole{
				Spec: RoleSpec{
					ClusterSource: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
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
					ClusterSource: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
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
					ShadowCluster: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
					SourceCluster: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
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
					ClusterSource: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
					KafkaAPISpec: &KafkaAPISpec{
						SASL: &KafkaSASL{
							AWSMskIam: &KafkaSASLAWSMskIam{
								DeprecatedSecretKey:    &SecretKeyRef{},
								DeprecatedSessionToken: &SecretKeyRef{},
							},
							DeprecatedPassword: &SecretKeyRef{},
							GSSAPIConfig: &KafkaSASLGSSAPI{
								DeprecatedPassword: &SecretKeyRef{},
							},
							OAUth: &KafkaSASLOAuthBearer{
								DeprecatedToken: &SecretKeyRef{},
							},
						},
						TLS: &CommonTLS{
							DeprecatedCaCert: &SecretKeyRef{},
							DeprecatedCert:   &SecretKeyRef{},
							DeprecatedKey:    &SecretKeyRef{},
						},
					},
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
					ClusterSource: &ClusterSource{
						StaticConfiguration: &StaticConfigurationSource{
							Admin: &AdminAPISpec{
								SASL: &AdminSASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							Kafka: &KafkaAPISpec{
								SASL: &KafkaSASL{
									AWSMskIam: &KafkaSASLAWSMskIam{
										DeprecatedSecretKey:    &SecretKeyRef{},
										DeprecatedSessionToken: &SecretKeyRef{},
									},
									DeprecatedPassword: &SecretKeyRef{},
									GSSAPIConfig: &KafkaSASLGSSAPI{
										DeprecatedPassword: &SecretKeyRef{},
									},
									OAUth: &KafkaSASLOAuthBearer{
										DeprecatedToken: &SecretKeyRef{},
									},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
							SchemaRegistry: &SchemaRegistrySpec{
								SASL: &SchemaRegistrySASL{
									DeprecatedAuthToken: &SecretKeyRef{},
									DeprecatedPassword:  &SecretKeyRef{},
								},
								TLS: &CommonTLS{
									DeprecatedCaCert: &SecretKeyRef{},
									DeprecatedCert:   &SecretKeyRef{},
									DeprecatedKey:    &SecretKeyRef{},
								},
							},
						},
					},
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
