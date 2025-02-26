// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package certmanager_test

import (
	"context"
	"fmt"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

//nolint:funlen // the subtests might causes linter to complain
func TestClusterCertificates(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-tls-secret-node-certificate",
			Namespace: "cert-manager",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("XXX"),
			"tls.key": []byte("XXX"),
			"ca.crt":  []byte("XXX"),
		},
	}
	issuer := certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "issuer",
			Namespace: "test",
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: nil,
			},
		},
	}
	validateVolumesFn := func(volName string, expectedSecretItems []string) func([]corev1.Volume) bool {
		return func(vols []corev1.Volume) bool {
			for i := range vols {
				v := vols[i]
				if v.Name == volName {
					if len(v.VolumeSource.Secret.Items) != len(expectedSecretItems) {
						return false
					}
					/* lazy way validation as we don't have many items */
					for _, i := range expectedSecretItems {
						found := false
						for _, j := range v.VolumeSource.Secret.Items {
							if i == j.Key {
								found = true
							}
						}
						if !found {
							return false
						}
					}
					return true
				}
			}
			return false
		}
	}

	tests := []struct {
		name              string
		pandaCluster      *vectorizedv1alpha1.Cluster
		expectedNames     []string
		volumesCount      int
		verifyVolumes     func(vols []corev1.Volume) bool
		expectedBrokerTLS *config.ServerTLS
	}{
		{"kafka tls disabled", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: false,
							},
						},
					},
				},
			},
		}, []string{}, 0, nil, nil},
		{"kafka tls", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda"}, 1, func(vols []corev1.Volume) bool {
			// verify the volume also contains CA in the node tls folder
			for _, i := range vols[0].Secret.Items {
				if i.Key == cmmetav1.TLSCAKey {
					return true
				}
			}
			return false
		}, &config.ServerTLS{
			Enabled:        true,
			TruststoreFile: "/etc/tls/certs/ca.crt",
		}},
		{"kafka tls on external only", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							External: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: true,
							},
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda"}, 1, func(vols []corev1.Volume) bool {
			return true
		}, nil},
		{"kafka tls with two tls listeners", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
							},
						},
						{
							External: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: true,
							},
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda"}, 1, nil, &config.ServerTLS{
			Enabled:        true,
			TruststoreFile: "/etc/tls/certs/ca.crt",
		}},
		{"kafka tls with external node issuer", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
								IssuerRef: &cmmetav1.ObjectReference{
									Name: "issuer",
									Kind: "Issuer",
								},
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda"}, 1, func(vols []corev1.Volume) bool {
			// verify the volume does not contain CA in the node tls folder when node cert is injected
			for i, v := range vols {
				if v.Name == "tlscert" {
					for _, i := range vols[i].Secret.Items {
						if i.Key == cmmetav1.TLSCAKey {
							return false
						}
					}
				}
			}
			return true
		}, &config.ServerTLS{
			Enabled: true,
		}},
		{"kafka mutual tls", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
							},
						},
						{
							External: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: true,
							},
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda", "test-operator-client", "test-user-client", "test-admin-client"}, 2, func(vols []corev1.Volume) bool {
			// verify the ca volume contains also client cert
			foundKey := false
			foundCrt := false
			for i, v := range vols {
				if v.Name == "tlsca" {
					for _, i := range vols[i].Secret.Items {
						if i.Key == corev1.TLSCertKey {
							foundCrt = true
						}
						if i.Key == corev1.TLSPrivateKeyKey {
							foundKey = true
						}
						if foundKey && foundCrt {
							break
						}
					}
					break
				}
			}
			return foundCrt && foundKey
		}, &config.ServerTLS{
			Enabled:        true,
			KeyFile:        "/etc/tls/certs/ca/tls.key",
			CertFile:       "/etc/tls/certs/ca/tls.crt",
			TruststoreFile: "/etc/tls/certs/ca.crt",
		}},
		{"kafka mutual tls with two tls listeners", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
						{
							TLS: vectorizedv1alpha1.KafkaAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
							},
						},
					},
				},
			},
		}, []string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-redpanda", "test-operator-client", "test-user-client", "test-admin-client"}, 2, nil, &config.ServerTLS{
			Enabled:        true,
			KeyFile:        "/etc/tls/certs/ca/tls.key",
			CertFile:       "/etc/tls/certs/ca/tls.crt",
			TruststoreFile: "/etc/tls/certs/ca.crt",
		}},
		{"admin api tls disabled", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					AdminAPI: []vectorizedv1alpha1.AdminAPI{
						{
							TLS: vectorizedv1alpha1.AdminAPITLS{
								Enabled: false,
							},
						},
					},
				},
			},
		}, []string{}, 0, nil, nil},
		{"admin api tls", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					AdminAPI: []vectorizedv1alpha1.AdminAPI{
						{
							TLS: vectorizedv1alpha1.AdminAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
		}, []string{"test-admin-selfsigned-issuer", "test-admin-root-certificate", "test-admin-root-issuer", "test-admin-api-node"}, 1, nil, nil},
		{"admin api mutual tls", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					AdminAPI: []vectorizedv1alpha1.AdminAPI{
						{
							TLS: vectorizedv1alpha1.AdminAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
							},
						},
					},
				},
			},
		}, []string{"test-admin-selfsigned-issuer", "test-admin-root-certificate", "test-admin-root-issuer", "test-admin-api-node", "test-admin-api-client"}, 2, nil, nil},
		{"pandaproxy api tls disabled", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
						{
							TLS: vectorizedv1alpha1.PandaproxyAPITLS{
								Enabled: false,
							},
						},
					},
				},
			},
		}, []string{}, 0, nil, nil},
		{
			"pandaproxy api tls", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled: true,
								},
							},
						},
					},
				},
			},
			[]string{"test-proxy-selfsigned-issuer", "test-proxy-root-certificate", "test-proxy-root-issuer", "test-proxy-api-node"},
			1, validateVolumesFn("tlspandaproxycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"pandaproxy api mutual tls", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled:           true,
									RequireClientAuth: true,
								},
							},
						},
					},
				},
			},
			[]string{"test-proxy-selfsigned-issuer", "test-proxy-root-certificate", "test-proxy-root-issuer", "test-proxy-api-node", "test-proxy-api-client"},
			2, validateVolumesFn("tlspandaproxycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"pandaproxy api mutual tls with external ca provided by customer",
			&vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled:           true,
									RequireClientAuth: true,
									ClientCACertRef: &corev1.TypedLocalObjectReference{
										Name: "client-ca-secret",
									},
								},
							},
						},
					},
				},
			},
			[]string{"test-proxy-api-trusted-client-ca", "test-proxy-selfsigned-issuer", "test-proxy-root-certificate", "test-proxy-root-issuer", "test-proxy-api-node", "test-proxy-api-client"},
			2, validateVolumesFn("tlspandaproxycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"pandaproxy api mutual tls with external ca provided by customer and external node issuer",
			&vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled:           true,
									RequireClientAuth: true,
									ClientCACertRef: &corev1.TypedLocalObjectReference{
										Name: "client-ca-secret",
									},
									IssuerRef: &cmmetav1.ObjectReference{
										Name: "issuer",
										Kind: "Issuer",
									},
								},
							},
						},
					},
				},
			},
			[]string{"test-proxy-api-trusted-client-ca", "test-proxy-selfsigned-issuer", "test-proxy-root-certificate", "test-proxy-root-issuer", "test-proxy-api-node", "test-proxy-api-client"},
			2, validateVolumesFn("tlspandaproxycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{"schematregistry api tls disabled", &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
						TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
							Enabled: false,
						},
					},
				},
			},
		}, []string{}, 0, nil, nil},
		{
			"schematregistry api tls", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled: true,
							},
						},
					},
				},
			},
			[]string{"test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer", "test-schema-registry-node"},
			1, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"schematregistry api mutual tls", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
							},
						},
					},
				},
			},
			[]string{"test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer", "test-schema-registry-node", "test-schema-registry-client"},
			2, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"schematregistry with tls, nodesecretref and without requireClientAuth", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled:           true,
								RequireClientAuth: false,
								NodeSecretRef: &corev1.ObjectReference{
									Name:      secret.Name,
									Namespace: secret.Namespace,
								},
							},
						},
					},
				},
			},
			[]string{"test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer"},
			1, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"kafka and schematregistry with nodesecretref", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
								NodeSecretRef: &corev1.ObjectReference{
									Name:      secret.Name,
									Namespace: secret.Namespace,
								},
							},
						},
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								TLS: vectorizedv1alpha1.KafkaAPITLS{
									Enabled:           true,
									RequireClientAuth: true,
									NodeSecretRef: &corev1.ObjectReference{
										Name:      secret.Name,
										Namespace: secret.Namespace,
									},
								},
							},
						},
					},
				},
			},
			[]string{"test-kafka-selfsigned-issuer", "test-kafka-root-certificate", "test-kafka-root-issuer", "test-operator-client", "test-user-client", "test-admin-client", "test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer", "test-schema-registry-client"},
			4, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), &config.ServerTLS{
				Enabled:        true,
				KeyFile:        "/etc/tls/certs/ca/tls.key",
				CertFile:       "/etc/tls/certs/ca/tls.crt",
				TruststoreFile: "/etc/tls/certs/ca.crt",
			},
		},
		{
			"schematregistry api mutual tls with external ca provided by customer", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
								ClientCACertRef: &corev1.TypedLocalObjectReference{
									Name: "client-ca-secret",
								},
							},
						},
					},
				},
			},
			[]string{"test-schema-registry-trusted-client-ca", "test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer", "test-schema-registry-node", "test-schema-registry-client"},
			2, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), nil,
		},
		{
			"schematregistry api mutual tls with external ca provided by customer and external node issuer", &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
								Enabled:           true,
								RequireClientAuth: true,
								ClientCACertRef: &corev1.TypedLocalObjectReference{
									Name: "client-ca-secret",
								},
								IssuerRef: &cmmetav1.ObjectReference{
									Name: "issuer",
									Kind: "Issuer",
								},
							},
						},
					},
				},
			},
			[]string{"test-schema-registry-trusted-client-ca", "test-schema-registry-selfsigned-issuer", "test-schema-registry-root-certificate", "test-schema-registry-root-issuer", "test-schema-registry-node", "test-schema-registry-client"},
			2, validateVolumesFn("tlsschemaregistrycert", []string{"tls.crt", "tls.key"}), nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc, err := certmanager.NewClusterCertificates(context.TODO(), tt.pandaCluster,
				types.NamespacedName{
					Name:      "test",
					Namespace: "test",
				}, fake.NewClientBuilder().WithRuntimeObjects(&secret, &issuer).Build(), "cluster.local", "cluster2.local", scheme.Scheme, logr.Discard())
			require.NoError(t, err)
			resources, err := cc.Resources(context.TODO())
			require.NoError(t, err)
			require.Equal(t, len(tt.expectedNames), len(resources))
			for _, n := range tt.expectedNames {
				found := false
				for _, r := range resources {
					if r.Key().Name == n {
						found = true
						break
					}
				}
				require.True(t, found, fmt.Sprintf("name %s not found in resources", n))
			}
			v, vm := cc.Volumes()
			require.Equal(t, tt.volumesCount, len(v), fmt.Sprintf("%s: volumes count don't match", tt.name))
			require.Equal(t, tt.volumesCount, len(vm), fmt.Sprintf("%s: volume mounts count don't match", tt.name))
			if tt.verifyVolumes != nil {
				require.True(t, tt.verifyVolumes(v), fmt.Sprintf("failed during volumes verification in the test: %v", tt.name))
			}
			brokerTLS := cc.KafkaClientBrokerTLS(resourcetypes.GetTLSMountPoints())
			require.Equal(t, tt.expectedBrokerTLS == nil, brokerTLS == nil)
			if tt.expectedBrokerTLS != nil && brokerTLS != nil {
				require.Equal(t, *tt.expectedBrokerTLS, *brokerTLS)
			}
		})
	}
}
