// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

var fakeK8sClient = fake.NewClientBuilder().Build()

//nolint:funlen // this is ok for a test
func TestDefault(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}
	type test struct {
		name                                string
		replicas                            int32
		additionalConfigurationSetByWebhook bool
		configAlreadyPresent                bool
	}
	tests := []test{
		{
			name:                                "do not set default topic replication when there is less than 3 replicas",
			replicas:                            2,
			additionalConfigurationSetByWebhook: false,
		},
		{
			name:                                "sets default topic replication",
			replicas:                            3,
			additionalConfigurationSetByWebhook: true,
		},
		{
			name:                                "does not set default topic replication when it already exists in CRD",
			replicas:                            3,
			additionalConfigurationSetByWebhook: false,
			configAlreadyPresent:                true,
		},
	}
	fields := []string{"redpanda.internal_topic_replication_factor"}
	for _, tt := range tests {
		for _, field := range fields {
			t.Run(tt.name, func(t *testing.T) {
				redpandaCluster := &vectorizedv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "",
					},
					Spec: vectorizedv1alpha1.ClusterSpec{
						Replicas:      ptr.To(tt.replicas),
						Configuration: vectorizedv1alpha1.RedpandaConfig{},
					},
				}

				if tt.configAlreadyPresent {
					redpandaCluster.Spec.AdditionalConfiguration = make(map[string]string)
					redpandaCluster.Spec.AdditionalConfiguration[field] = "111"
				}

				require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
				val, exist := redpandaCluster.Spec.AdditionalConfiguration[field]
				if (exist && val == "3") != tt.additionalConfigurationSetByWebhook {
					t.Fail()
				}
			})
		}
	}

	t.Run("missing schema registry does not set default port", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas:      ptr.To(int32(1)),
				Configuration: vectorizedv1alpha1.RedpandaConfig{},
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Redpanda: nil,
				},
			},
		}

		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.Nil(t, redpandaCluster.Spec.Configuration.SchemaRegistry)
	})
	t.Run("if schema registry exist, but the port is 0 the default is set", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{},
				},
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Redpanda: nil,
				},
			},
		}

		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.Equal(t, 8081, redpandaCluster.Spec.Configuration.SchemaRegistry.Port)
	})
	t.Run("if schema registry exist and port have not zero value the default will not be used", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
						Port: 999,
					},
				},
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Redpanda: nil,
				},
			},
		}

		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.Equal(t, 999, redpandaCluster.Spec.Configuration.SchemaRegistry.Port)
	})
	t.Run("if schema registry is defined as rest of external listeners the default port is used", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
						External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
							ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
						},
					},
				},
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Redpanda: nil,
				},
			},
		}

		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.Equal(t, 8081, redpandaCluster.Spec.Configuration.SchemaRegistry.Port)
	})
	t.Run("pod disruption budget", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
			},
		}
		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.True(t, redpandaCluster.Spec.PodDisruptionBudget.Enabled)
		assert.Equal(t, intstr.FromInt(1), *redpandaCluster.Spec.PodDisruptionBudget.MaxUnavailable)
	})
	t.Run("cluster license key default is set", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
				LicenseRef: &vectorizedv1alpha1.SecretKeyRef{
					Name:      "test",
					Namespace: "",
				},
			},
		}
		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.Equal(t, vectorizedv1alpha1.DefaultLicenseSecretKey, redpandaCluster.Spec.LicenseRef.Key)
	})

	t.Run("when restart config is nil, set UnderReplicatedPartitionThreshold to 0", func(t *testing.T) {
		redpandaCluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "",
			},
			Spec: vectorizedv1alpha1.ClusterSpec{},
		}
		require.NoError(t, webhook.Default(context.Background(), redpandaCluster))
		assert.NotNil(t, redpandaCluster.Spec.RestartConfig)
		assert.Equal(t, 0, redpandaCluster.Spec.RestartConfig.UnderReplicatedPartitionThreshold)
	})
}

func TestValidateUpdate(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	var replicas0 int32
	var replicas3 int32 = 3

	redpandaCluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Replicas:      ptr.To(replicas3),
			Configuration: vectorizedv1alpha1.RedpandaConfig{},
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("0.9Gi"),
					},
				},
				Redpanda: nil,
			},
		},
	}

	updatedCluster := redpandaCluster.DeepCopy()
	updatedCluster.Spec.Replicas = &replicas0
	updatedCluster.Spec.Configuration = vectorizedv1alpha1.RedpandaConfig{
		KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
			{
				Port: 123,
				TLS: vectorizedv1alpha1.KafkaAPITLS{
					RequireClientAuth: true,
					IssuerRef: &cmmetav1.ObjectReference{
						Name: "test",
					},
					NodeSecretRef: &corev1.ObjectReference{
						Name:      "name",
						Namespace: "default",
					},
					Enabled: false,
				},
			},
		},
	}

	_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatedCluster)
	if err == nil {
		t.Fatalf("expecting validation error but got none")
	}

	// verify the error causes contain all expected fields
	statusError := err.(*apierrors.StatusError)
	expectedFields := []string{
		field.NewPath("spec").Child("replicas").String(),
		field.NewPath("spec").Child("resources").Child("requests").Child("memory").String(),
		field.NewPath("spec").Child("configuration").Child("kafkaApi").Index(0).Child("tls").Child("requireClientAuth").String(),
		field.NewPath("spec").Child("configuration").Child("kafkaApi").Index(0).Child("tls").Child("nodeSecretRef").String(),
	}

	for _, ef := range expectedFields {
		found := false
		for _, c := range statusError.Status().Details.Causes {
			if ef == c.Field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expecting failure on field %s but have %v", ef, statusError.Status().Details.Causes)
		}
	}
}

//nolint:funlen // this is ok for a test
func TestValidateUpdate_NoError(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	var replicas2 int32 = 2

	redpandaCluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI:       []vectorizedv1alpha1.KafkaAPI{{Port: 124, AuthenticationMethod: "none"}},
				AdminAPI:       []vectorizedv1alpha1.AdminAPI{{Port: 125}},
				RPCServer:      vectorizedv1alpha1.SocketAddress{Port: 126},
				SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{Port: 127},
				PandaproxyAPI:  []vectorizedv1alpha1.PandaproxyAPI{{Port: 128}},
			},
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
						corev1.ResourceCPU:    resource.MustParse("1"),
					},
				},
				Redpanda: nil,
			},
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{
					Name:     "test",
					Replicas: ptr.To(replicas2),
				},
			},
		},
	}

	t.Run("same object updated", func(t *testing.T) {
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, redpandaCluster)
		assert.NoError(t, err)
	})

	t.Run("scale up", func(t *testing.T) {
		scaleUp := *redpandaCluster.Spec.NodePools[0].Replicas + 1
		updatedScaleUp := redpandaCluster.DeepCopy()
		updatedScaleUp.Spec.Replicas = &scaleUp
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatedScaleUp)
		assert.NoError(t, err)
	})

	t.Run("change image and tag", func(t *testing.T) {
		updatedImage := redpandaCluster.DeepCopy()
		updatedImage.Spec.Image = "differentimage"
		updatedImage.Spec.Version = "111"
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatedImage)
		assert.NoError(t, err)
	})

	t.Run("collision in the port", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.KafkaAPI[0].Port = 200
		updatePort.Spec.Configuration.AdminAPI[0].Port = 200
		updatePort.Spec.Configuration.RPCServer.Port = 200
		updatePort.Spec.Configuration.SchemaRegistry.Port = 200

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("collision in the port when external connectivity is enabled", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.KafkaAPI = append(updatePort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.AdminAPI = append(updatePort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.PandaproxyAPI = append(updatePort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}})

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("no collision when schema registry has the next port to panda proxy", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.KafkaAPI[0].Port = 200
		updatePort.Spec.Configuration.KafkaAPI = append(updatePort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "none", External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.PandaproxyAPI = append(updatePort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}})
		updatePort.Spec.Configuration.SchemaRegistry.External = &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
		}

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.NoError(t, err)
	})

	t.Run("collision in the port when external connectivity is enabled", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.KafkaAPI = append(updatePort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.KafkaAPI[0].Port = 200
		updatePort.Spec.Configuration.AdminAPI[0].Port = 300
		updatePort.Spec.Configuration.RPCServer.Port = 201

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("collision in the port when external connectivity is enabled", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.KafkaAPI = append(updatePort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.KafkaAPI[0].Port = 200
		updatePort.Spec.Configuration.AdminAPI[0].Port = 300
		updatePort.Spec.Configuration.AdminAPI[0].External.Enabled = true
		updatePort.Spec.Configuration.RPCServer.Port = 301

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("port collision with proxy and schema registry", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.SchemaRegistry.Port = updatePort.Spec.Configuration.PandaproxyAPI[0].Port

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("collision in admin port when external connectivity is enabled", func(t *testing.T) {
		updatePort := redpandaCluster.DeepCopy()
		updatePort.Spec.Configuration.AdminAPI[0].External.Enabled = true
		updatePort.Spec.Configuration.KafkaAPI = append(updatePort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		updatePort.Spec.Configuration.KafkaAPI[0].Port = 201
		updatePort.Spec.Configuration.AdminAPI[0].Port = 200
		updatePort.Spec.Configuration.RPCServer.Port = 300

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, updatePort)
		assert.Error(t, err)
	})

	t.Run("requireclientauth true and tls enabled", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI[0].TLS.RequireClientAuth = true
		tls.Spec.Configuration.KafkaAPI[0].TLS.Enabled = true

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, tls)
		assert.NoError(t, err)
	})

	t.Run("multiple external listeners", func(t *testing.T) {
		exPort := redpandaCluster.DeepCopy()
		exPort.Spec.Configuration.KafkaAPI[0].External.Enabled = true
		exPort.Spec.Configuration.KafkaAPI = append(exPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 123, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, exPort)

		assert.Error(t, err)
	})

	t.Run("multiple internal listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.KafkaAPI = append(multiPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 123})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, multiPort)

		assert.Error(t, err)
	})

	t.Run("external listener cannot have port specified", func(t *testing.T) {
		exPort := redpandaCluster.DeepCopy()
		exPort.Spec.Configuration.KafkaAPI[0].External.Enabled = true
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, exPort)

		assert.Error(t, err)
	})

	t.Run("no admin port", func(t *testing.T) {
		noPort := redpandaCluster.DeepCopy()
		noPort.Spec.Configuration.AdminAPI = []vectorizedv1alpha1.AdminAPI{}

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, noPort)
		assert.Error(t, err)
	})

	t.Run("multiple internal admin listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI = append(multiPort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{Port: 123})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, multiPort)

		assert.Error(t, err)
	})

	t.Run("multiple admin listeners with tls", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI[0].TLS.Enabled = true
		multiPort.Spec.Configuration.AdminAPI = append(multiPort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{
				Port:     123,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
				TLS:      vectorizedv1alpha1.AdminAPITLS{Enabled: true},
			})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, multiPort)

		assert.Error(t, err)
	})

	t.Run("tls admin listener without enabled true", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI[0].TLS.RequireClientAuth = true
		multiPort.Spec.Configuration.AdminAPI[0].TLS.Enabled = false
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, multiPort)

		assert.Error(t, err)
	})

	t.Run("proxy subdomain must be the same as kafka subdomain", func(t *testing.T) {
		withSub := redpandaCluster.DeepCopy()
		withSub.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{
			{
				Port:     145,
				External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "subdomain"}},
			},
		}
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, withSub)

		assert.Error(t, err)
	})

	t.Run("cannot have multiple internal proxy listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.PandaproxyAPI = append(multiPort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{Port: 123}, vectorizedv1alpha1.PandaproxyAPI{Port: 321})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, multiPort)

		assert.Error(t, err)
	})

	t.Run("cannot have external proxy listener without an internal one", func(t *testing.T) {
		noInternal := redpandaCluster.DeepCopy()
		noInternal.Spec.Configuration.PandaproxyAPI = append(noInternal.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}, Port: 123})
		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, noInternal)

		assert.Error(t, err)
	})

	t.Run("pandaproxy tls disabled with client auth enabled", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI = append(tls.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
			Port:     30092,
		})
		tls.Spec.Configuration.PandaproxyAPI = append(tls.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{
			External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
			},
			TLS: vectorizedv1alpha1.PandaproxyAPITLS{Enabled: false, RequireClientAuth: true},
		})

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, tls)
		assert.Error(t, err)
	})

	t.Run("pandaproxy tls issuerref with secretref is not allowed", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI = append(tls.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "cluster.com"},
			Port:     30092,
		})
		tls.Spec.Configuration.PandaproxyAPI = append(tls.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{
			External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "cluster.com"},
			},
			TLS: vectorizedv1alpha1.PandaproxyAPITLS{Enabled: true, IssuerRef: &cmmetav1.ObjectReference{}, NodeSecretRef: &corev1.ObjectReference{}},
		})

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, tls)
		assert.Error(t, err)
	})

	t.Run("pandaproxy tls can specify issuerref", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI = append(tls.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			AuthenticationMethod: "none",
			External:             vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "cluster.com"},
			Port:                 30092,
		})
		tls.Spec.Configuration.PandaproxyAPI = append(tls.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{
			External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "cluster.com"},
			},
			TLS: vectorizedv1alpha1.PandaproxyAPITLS{Enabled: true, IssuerRef: &cmmetav1.ObjectReference{}},
		})

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, tls)
		assert.NoError(t, err)
	})

	t.Run("update priority class name", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
		pc := &schedulingv1.PriorityClass{}
		pcName := "my-priority-class-2"
		pc.SetName(pcName)
		err := fakeK8sClient.Create(context.TODO(), pc)
		assert.NoError(t, err)
		defer func() { _ = fakeK8sClient.Delete(context.TODO(), pc) }()

		cluster := redpandaCluster.DeepCopy()
		cluster.Spec.PriorityClassName = pcName

		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, cluster)
		assert.NoError(t, err)
	})

	t.Run("priority class does not exist in update", func(t *testing.T) {
		pcName := "priority-class-not-exist"

		cluster := redpandaCluster.DeepCopy()
		cluster.Spec.PriorityClassName = pcName

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, cluster)
		assert.Error(t, err)
	})

	t.Run("resource limits/requests on redpanda resources", func(t *testing.T) {
		c := redpandaCluster.DeepCopy()
		c.Spec.Resources.Limits = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("1Gi"),
			corev1.ResourceCPU:    resource.MustParse("1"),
		}
		c.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("2Gi"),
			corev1.ResourceCPU:    resource.MustParse("1"),
		}
		one := int32(1)
		c.Spec.Replicas = &one

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, c)
		assert.Error(t, err)
	})

	t.Run("resource limits/requests on rpk status resources", func(t *testing.T) {
		c := redpandaCluster.DeepCopy()
		c.Spec.Sidecars = vectorizedv1alpha1.Sidecars{
			RpkStatus: &vectorizedv1alpha1.Sidecar{
				Enabled: true,
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"),
						corev1.ResourceCPU:    resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
						corev1.ResourceCPU:    resource.MustParse("1"),
					},
				},
			},
		}

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, c)
		assert.Error(t, err)
	})

	t.Run("cluster can be deleted even if licenseRef not found", func(t *testing.T) {
		license := redpandaCluster.DeepCopy()
		license.Spec.LicenseRef = &vectorizedv1alpha1.SecretKeyRef{Name: "notfound", Namespace: "notfound"}

		// Set cluster to deleting state
		now := metav1.Now()
		license.SetDeletionTimestamp(&now)

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, license)
		assert.NoError(t, err)
	})

	decreaseCases := []struct {
		initial    string
		target     string
		error      bool
		lowerBound string
	}{
		{
			initial: "2",
			target:  "2500m",
			error:   false,
		},
		{
			initial: "2",
			target:  "1001m",
			error:   false,
		},
		{
			initial:    "2000m",
			target:     "999m",
			error:      true,
			lowerBound: "1001m",
		},
		{
			initial:    "1.1",
			target:     "1",
			error:      true,
			lowerBound: "1001m",
		},
	}
	for _, tc := range decreaseCases {
		t.Run(fmt.Sprintf("CPU request change from %s to %s", tc.initial, tc.target), func(t *testing.T) {
			oldCluster := redpandaCluster.DeepCopy()
			oldCluster.Spec.NodePools[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("20Gi"),
				corev1.ResourceCPU:    resource.MustParse(tc.initial),
			}
			oldCluster.Spec.NodePools[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("20Gi"),
				corev1.ResourceCPU:    resource.MustParse(tc.initial),
			}

			newCluster := redpandaCluster.DeepCopy()
			newCluster.Spec.NodePools[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("20Gi"),
				corev1.ResourceCPU:    resource.MustParse(tc.target),
			}

			_, err := webhook.ValidateUpdate(context.Background(), oldCluster, newCluster)
			if tc.error {
				assert.Error(t, err)
				if err != nil && tc.lowerBound != "" {
					parts := strings.Split(err.Error(), " ")
					computedBound := parts[len(parts)-1]
					assert.Equal(t, tc.lowerBound, computedBound)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // this is ok for a test
func TestCreation(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	redpandaCluster := validRedpandaCluster()

	t.Run("no collision in the port", func(t *testing.T) {
		newPort := redpandaCluster.DeepCopy()
		newPort.Spec.Configuration.KafkaAPI[0].Port = 200

		_, err := webhook.ValidateCreate(context.Background(), newPort)
		assert.NoError(t, err)
	})

	t.Run("collision in the port", func(t *testing.T) {
		newPort := redpandaCluster.DeepCopy()
		newPort.Spec.Configuration.KafkaAPI[0].Port = 200
		newPort.Spec.Configuration.AdminAPI[0].Port = 200
		newPort.Spec.Configuration.RPCServer.Port = 200
		newPort.Spec.Configuration.SchemaRegistry.Port = 200

		_, err := webhook.ValidateCreate(context.Background(), newPort)
		assert.Error(t, err)
	})

	t.Run("collision in the port when external connectivity is enabled", func(t *testing.T) {
		newPort := redpandaCluster.DeepCopy()
		newPort.Spec.Configuration.AdminAPI[0].Port = newPort.Spec.Configuration.KafkaAPI[0].Port + 1
		newPort.Spec.Configuration.KafkaAPI = append(newPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		newPort.Spec.Configuration.AdminAPI = append(newPort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		newPort.Spec.Configuration.PandaproxyAPI = append(newPort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}})

		_, err := webhook.ValidateCreate(context.Background(), newPort)
		assert.Error(t, err)
	})

	t.Run("no collision when schema registry has the next port to panda proxy", func(t *testing.T) {
		newPort := redpandaCluster.DeepCopy()
		newPort.Spec.Configuration.KafkaAPI[0].Port = 200
		newPort.Spec.Configuration.KafkaAPI = append(newPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "none", External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		newPort.Spec.Configuration.PandaproxyAPI = append(newPort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}})
		newPort.Spec.Configuration.SchemaRegistry.External = &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
		}

		_, err := webhook.ValidateCreate(context.Background(), newPort)
		assert.NoError(t, err)
	})

	t.Run("port collision with proxy and schema registry", func(t *testing.T) {
		newPort := redpandaCluster.DeepCopy()
		newPort.Spec.Configuration.SchemaRegistry.Port = newPort.Spec.Configuration.PandaproxyAPI[0].Port

		_, err := webhook.ValidateCreate(context.Background(), newPort)
		assert.Error(t, err)
	})

	t.Run("no kafka port", func(t *testing.T) {
		noPort := redpandaCluster.DeepCopy()
		noPort.Spec.Configuration.KafkaAPI = []vectorizedv1alpha1.KafkaAPI{}

		_, err := webhook.ValidateCreate(context.Background(), noPort)
		assert.Error(t, err)
	})

	t.Run("no admin port", func(t *testing.T) {
		noPort := redpandaCluster.DeepCopy()
		noPort.Spec.Configuration.AdminAPI = []vectorizedv1alpha1.AdminAPI{}

		_, err := webhook.ValidateCreate(context.Background(), noPort)
		assert.Error(t, err)
	})

	t.Run("multiple internal admin listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI = append(multiPort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{Port: 123})
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("incorrect memory (need 2GB per core)", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
					corev1.ResourceCPU:    resource.MustParse("2"),
				},
			},
			Redpanda: nil,
		}

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.Error(t, err)
	})

	t.Run("no 2GB per core required when in developer mode", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
					corev1.ResourceCPU:    resource.MustParse("2"),
				},
			},
			Redpanda: nil,
		}
		memory.Spec.Configuration.DeveloperMode = true

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.NoError(t, err)
	})

	t.Run("incorrect redpanda memory (need <= request)", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			Redpanda: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.Error(t, err)
	})

	//nolint:dupl // the values are different
	t.Run("incorrect redpanda memory (need <= limit)", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("3Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			Redpanda: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.Error(t, err)
	})

	//nolint:dupl // the values are different
	t.Run("correct redpanda memory", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2.223Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2.223Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			Redpanda: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.NoError(t, err)
	})

	//nolint:dupl // the values are different
	t.Run("correct redpanda memory (boundary check)", func(t *testing.T) {
		memory := redpandaCluster.DeepCopy()
		memory.Spec.Resources = vectorizedv1alpha1.RedpandaResourceRequirements{
			ResourceRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			Redpanda: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), memory)
		assert.NoError(t, err)
	})

	t.Run("tls properly configured", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI[0].TLS.Enabled = true
		tls.Spec.Configuration.KafkaAPI[0].TLS.RequireClientAuth = true

		_, err := webhook.ValidateCreate(context.Background(), tls)
		assert.NoError(t, err)
	})

	t.Run("require client auth without tls enabled", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.KafkaAPI[0].TLS.Enabled = false
		tls.Spec.Configuration.KafkaAPI[0].TLS.RequireClientAuth = true

		_, err := webhook.ValidateCreate(context.Background(), tls)
		assert.Error(t, err)
	})

	t.Run("multiple external listeners", func(t *testing.T) {
		exPort := redpandaCluster.DeepCopy()
		exPort.Spec.Configuration.KafkaAPI[0].External.Enabled = true
		exPort.Spec.Configuration.KafkaAPI = append(exPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 123, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
		_, err := webhook.ValidateCreate(context.Background(), exPort)

		assert.Error(t, err)
	})

	t.Run("multiple internal listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.KafkaAPI = append(multiPort.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 123})
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("external listener with port", func(t *testing.T) {
		exPort := redpandaCluster.DeepCopy()
		exPort.Spec.Configuration.KafkaAPI[0].External.Enabled = true
		_, err := webhook.ValidateCreate(context.Background(), exPort)

		assert.Error(t, err)
	})

	t.Run("multiple admin listeners with tls", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI[0].TLS.Enabled = true
		multiPort.Spec.Configuration.AdminAPI = append(multiPort.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{
				Port:     123,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
				TLS:      vectorizedv1alpha1.AdminAPITLS{Enabled: true},
			})
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("tls admin listener without enabled true", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.AdminAPI[0].TLS.RequireClientAuth = true
		multiPort.Spec.Configuration.AdminAPI[0].TLS.Enabled = false
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("proxy subdomain must be the same as kafka subdomain", func(t *testing.T) {
		withSub := redpandaCluster.DeepCopy()
		withSub.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{
			{
				Port:     145,
				External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "subdomain"}},
			},
		}
		_, err := webhook.ValidateCreate(context.Background(), withSub)

		assert.Error(t, err)
	})

	t.Run("cannot have multiple internal proxy listeners", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.PandaproxyAPI = append(multiPort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{Port: 123}, vectorizedv1alpha1.PandaproxyAPI{Port: 321})
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("cannot have external proxy listener without an internal one", func(t *testing.T) {
		noInternal := redpandaCluster.DeepCopy()
		noInternal.Spec.Configuration.PandaproxyAPI = append(noInternal.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}, Port: 123})
		_, err := webhook.ValidateCreate(context.Background(), noInternal)

		assert.Error(t, err)
	})

	t.Run("external proxy listener cannot have port specified", func(t *testing.T) {
		multiPort := redpandaCluster.DeepCopy()
		multiPort.Spec.Configuration.PandaproxyAPI = append(multiPort.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}}, Port: 123},
			vectorizedv1alpha1.PandaproxyAPI{Port: 321})
		_, err := webhook.ValidateCreate(context.Background(), multiPort)

		assert.Error(t, err)
	})

	t.Run("pandaproxy tls disabled but client auth enabled", func(t *testing.T) {
		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.PandaproxyAPI = append(tls.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{TLS: vectorizedv1alpha1.PandaproxyAPITLS{Enabled: false, RequireClientAuth: true}})

		_, err := webhook.ValidateCreate(context.Background(), tls)
		assert.Error(t, err)
	})

	t.Run("priority class name is set: success", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)

		pc := &schedulingv1.PriorityClass{}
		pcName := "my-priority-class-1"
		pc.SetName(pcName)
		err := fakeK8sClient.Create(context.TODO(), pc)
		assert.NoError(t, err)
		defer func() { _ = fakeK8sClient.Delete(context.TODO(), pc) }()

		cluster := redpandaCluster.DeepCopy()
		cluster.Spec.PriorityClassName = pcName
		_, err = webhook.ValidateCreate(context.Background(), cluster)
		assert.NoError(t, err)
	})

	t.Run("specified priority class name does not exist: failure", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)

		cluster := redpandaCluster.DeepCopy()
		cluster.Spec.PriorityClassName = "my-priority-class-not-exist"
		_, err := webhook.ValidateCreate(context.Background(), cluster)
		assert.Error(t, err)
	})

	t.Run("pandaproxy mtls with external ca set by clientCACertRef: success", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)

		caCertSecret := &corev1.Secret{}
		caCertName := "pandaproxy-client-valid-ca-cert"
		caCertSecret.SetName(caCertName)
		caCertSecret.SetNamespace(redpandaCluster.Namespace)
		certData, err := os.ReadFile("./testdata/ca.crt.pem")
		assert.NoError(t, err)

		caCertSecret.Data = map[string][]byte{"ca.crt": certData}
		err = fakeK8sClient.Create(context.TODO(), caCertSecret)
		assert.NoError(t, err)

		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{
			{
				Port: 1234,
				TLS: vectorizedv1alpha1.PandaproxyAPITLS{
					Enabled:           true,
					RequireClientAuth: true,
					ClientCACertRef: &corev1.TypedLocalObjectReference{
						Name: caCertName,
						Kind: "secret",
					},
				},
			},
		}
		_, err = webhook.ValidateCreate(context.Background(), tls)
		assert.NoError(t, err)
	})

	// More failure test cases are covered by those for Schema Registry since the common validation function is called.
	t.Run("pandaproxy mtls with external ca set by clientCACertRef: invalid certificate", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)

		caCertSecret := &corev1.Secret{}
		caCertName := "pandaproxy-client-not-ca-cert"
		caCertSecret.SetName(caCertName)
		caCertSecret.SetNamespace(redpandaCluster.Namespace)
		caCertSecret.Data = map[string][]byte{"ca.crt": []byte("invalid cert")}
		err := fakeK8sClient.Create(context.TODO(), caCertSecret)
		assert.NoError(t, err)

		tls := redpandaCluster.DeepCopy()
		tls.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{
			{
				Port: 1234,
				TLS: vectorizedv1alpha1.PandaproxyAPITLS{
					Enabled:           true,
					RequireClientAuth: true,
					ClientCACertRef: &corev1.TypedLocalObjectReference{
						Name: caCertName,
						Kind: "secret",
					},
				},
			},
		}

		_, err = webhook.ValidateCreate(context.Background(), tls)
		assert.Error(t, err)
	})

	t.Run("kafka external subdomain is provided along with preferred address type", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 123, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, PreferredAddressType: "preferred", Subdomain: "subdomain"}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	// No support for IP-based TLS certs (#2256)
	t.Run("kafka TLS for external listener without a subdomain", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				TLS: vectorizedv1alpha1.KafkaAPITLS{
					Enabled: true,
				},
				Port: 123, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, PreferredAddressType: "InternalIP"},
			})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("bootstrap loadbalancer for kafka api needs a port", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("bootstrap loadbalancer not allowed for admin", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.AdminAPI = append(rp.Spec.Configuration.AdminAPI,
			vectorizedv1alpha1.AdminAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
				Port: 123,
			}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("bootstrap loadbalancer not allowed for pandaproxy", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.PandaproxyAPI = append(rp.Spec.Configuration.PandaproxyAPI,
			vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
				Port: 123,
			}}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("bootstrap loadbalancer not allowed for schemaregistry", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		rp.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled: true, Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
					Port: 123,
				},
			},
		}}
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	//nolint:dupl // not really a duplicate
	t.Run("endpoint template not allowed for schemaregistry", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		const commonDomain = "company.org"

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:   true,
			Subdomain: commonDomain,
		}})
		rp.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:          true,
				Subdomain:        commonDomain,
				EndpointTemplate: "xxx",
			},
		}}
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("endpoint allowed for schemaregistry", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		const commonDomain = "company.org"

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			AuthenticationMethod: "none",
			External: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
		})
		rp.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
			Endpoint: "xxx",
		}}
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.NoError(t, err)
	})
	//nolint:dupl // not really a duplicate
	t.Run("invalid endpoint not allowed for schemaregistry", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		const commonDomain = "company.org"

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:   true,
			Subdomain: commonDomain,
		}})
		rp.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
			Endpoint: "xx.xx",
		}}
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("endpoint template not allowed for adminapi", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()
		const commonDomain = "company.org"

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:   true,
			Subdomain: commonDomain,
		}})
		rp.Spec.Configuration.AdminAPI = append(rp.Spec.Configuration.AdminAPI, vectorizedv1alpha1.AdminAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			Subdomain:        commonDomain,
			EndpointTemplate: "xxx",
		}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("endpoint template without subdomain is not allowed in kafka API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			EndpointTemplate: "xxx",
		}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("endpoint template without subdomain is not allowed in pandaproxy API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled: true,
		}})
		rp.Spec.Configuration.PandaproxyAPI = append(rp.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			EndpointTemplate: "xxx",
		}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("invalid endpoint template in kafka API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			Subdomain:        "example.com",
			EndpointTemplate: "{{.Inexistent}}",
		}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("valid endpoint template in kafka API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			AuthenticationMethod: "none",
			External: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:          true,
				Subdomain:        "example.com",
				EndpointTemplate: "{{.Index}}-broker",
			},
		})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.NoError(t, err)
	})
	t.Run("invalid endpoint template in pandaproxy API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		const commonDomain = "mydomain"
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:   true,
			Subdomain: commonDomain,
		}})
		rp.Spec.Configuration.PandaproxyAPI = append(rp.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			Subdomain:        commonDomain,
			EndpointTemplate: "{{.Index | nonexistent }}",
		}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.Error(t, err)
	})
	t.Run("valid endpoint template in pandaproxy API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		const commonDomain = "mydomain"
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			AuthenticationMethod: "none",
			External: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
		})
		rp.Spec.Configuration.PandaproxyAPI = append(rp.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
			Enabled:          true,
			Subdomain:        commonDomain,
			EndpointTemplate: "{{.Index}}-pp",
		}}})
		_, err := webhook.ValidateCreate(context.Background(), rp)
		assert.NoError(t, err)
	})
	t.Run("valid ingress configuration in pandaproxy API", func(t *testing.T) {
		rp := redpandaCluster.DeepCopy()

		const commonDomain = "mydomain"
		rp.Spec.Configuration.KafkaAPI = append(rp.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{
			AuthenticationMethod: "none",
			External: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
		})
		rp.Spec.Configuration.PandaproxyAPI = append(rp.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
			ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
				Enabled:   true,
				Subdomain: commonDomain,
			},
			Ingress: &vectorizedv1alpha1.IngressConfig{},
		}})

		cases := []struct {
			endpoint string
			error    bool
		}{
			{
				endpoint: "pproxy",
			},
			{
				endpoint: "",
			},
			{
				endpoint: " 1",
				error:    true,
			},
			{
				endpoint: "pproxy.subdomain",
				error:    true,
			},
		}
		for _, c := range cases {
			rp.Spec.Configuration.PandaproxyAPI[len(rp.Spec.Configuration.PandaproxyAPI)-1].External.Ingress.Endpoint = c.endpoint
			_, err := webhook.ValidateCreate(context.Background(), rp)
			if c.error {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		}
	})
}

func TestSchemaRegistryValidations(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	redpandaCluster := validRedpandaCluster()

	t.Run("if schema registry externally available, kafka external listener is required", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
			},
		}
		schemaReg.Spec.Configuration.KafkaAPI[0].External.Enabled = false

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
	})
	t.Run("schema registry externally available is valid when it has the same subdomain as kafka external listener", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "test.com"},
			},
		}
		schemaReg.Spec.Configuration.KafkaAPI = append(schemaReg.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "none", External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "test.com"}})

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.NoError(t, err)
	})
	//nolint:dupl // the tests are not duplicates
	t.Run("if schema registry externally available, it should have same subdomain as kafka external listener", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "test.com"},
			},
		}
		schemaReg.Spec.Configuration.KafkaAPI = append(schemaReg.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "other.com"}})

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
	})
	//nolint:dupl // the tests are not duplicates
	t.Run("if schema registry externally available, kafka external listener should not be empty", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
				ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "test.com"},
			},
		}
		schemaReg.Spec.Configuration.KafkaAPI = append(schemaReg.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: ""}})

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
	})

	t.Run("schema registry mTLS enabled and TLS enabled is valid", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.NoError(t, err)
	})

	t.Run("if schema registry mTLS enabled, TLS should also be enabled", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           false,
				RequireClientAuth: true,
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
	})
}

func TestSchemaRegistryTLSExternalCAConfigValidations(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	redpandaCluster := validRedpandaCluster()

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, name must be provided in clientCACertRef", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef:   &corev1.TypedLocalObjectReference{},
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, kind must be set to secret in clientCACertRef", func(t *testing.T) {
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: "test",
					Kind: "configmap",
				},
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, the CA certificate secret must exist", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: "does-not-exist",
					Kind: "secret",
				},
			},
		}

		_, err := webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})

	t.Run("if schema registry mTLS enabled, clientCACertRef is set and cluster is being deleted, skip update validation", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
		schemaReg := redpandaCluster.DeepCopy()
		deleteTime := metav1.Now()
		schemaReg.DeletionTimestamp = &deleteTime
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: "does-not-exist",
					Kind: "secret",
				},
			},
		}

		_, err := webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.NoError(t, err)
	})
}

func TestSchemaRegistryTLSExternalCACertValidations(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	redpandaCluster := validRedpandaCluster()

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, the CA certificate secret must provide ca.crt", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
		caCertSecret := &corev1.Secret{}
		caCertName := "schema-registry-client-ca-cert-no-ca-crt"
		caCertSecret.SetName(caCertName)
		caCertSecret.SetNamespace(redpandaCluster.Namespace)
		caCertSecret.Data = map[string][]byte{"no.ca.crt": []byte("no.ca.crt")}
		err := fakeK8sClient.Create(context.TODO(), caCertSecret)
		assert.NoError(t, err)

		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: caCertName,
				},
			},
		}

		_, err = webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, the CA certificate secret must be in PEM", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
		caCertSecret := &corev1.Secret{}
		caCertName := "schema-registry-client-ca-cert-invalid-pem"
		caCertSecret.SetName(caCertName)
		caCertSecret.SetNamespace(redpandaCluster.Namespace)
		caCertSecret.Data = map[string][]byte{"ca.crt": []byte("not-in-pem")}
		err := fakeK8sClient.Create(context.TODO(), caCertSecret)
		assert.NoError(t, err)

		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: caCertName,
					Kind: "secret",
				},
			},
		}

		_, err = webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})

	t.Run("if schema registry mTLS enabled and clientCACertRef is set, the certificate secret must be valid x509 certificate", func(t *testing.T) {
		vectorizedv1alpha1.SetK8sClient(fakeK8sClient)

		caCertSecret := &corev1.Secret{}
		caCertName := "schema-registry-client-ca-cert-not-ca-cert"
		caCertSecret.SetName(caCertName)
		caCertSecret.SetNamespace(redpandaCluster.Namespace)
		certData, err := os.ReadFile("./testdata/not.ca.crt.pem")
		assert.NoError(t, err)

		caCertSecret.Data = map[string][]byte{"ca.crt": certData}
		err = fakeK8sClient.Create(context.TODO(), caCertSecret)
		assert.NoError(t, err)

		schemaReg := redpandaCluster.DeepCopy()
		schemaReg.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
			TLS: &vectorizedv1alpha1.SchemaRegistryAPITLS{
				Enabled:           true,
				RequireClientAuth: true,
				ClientCACertRef: &corev1.TypedLocalObjectReference{
					Name: caCertName,
					Kind: "secret",
				},
			},
		}

		_, err = webhook.ValidateCreate(context.Background(), schemaReg)
		assert.Error(t, err)
		_, err = webhook.ValidateUpdate(context.Background(), redpandaCluster, schemaReg)
		assert.Error(t, err)
	})
}

func TestAdminAPITLSExternalCA(t *testing.T) { //nolint:funlen // better packing related tests together
	webhook := &vectorizedv1alpha1.Cluster{}

	cases := []struct {
		name        string
		adminAPI    []vectorizedv1alpha1.AdminAPI
		clusterFn   func(cluster *vectorizedv1alpha1.Cluster)
		secret      *corev1.Secret
		skipCreate  bool
		createError bool
		updateError bool
	}{
		{
			name: "if admin API mTLS enabled and clientCACertRef is set, name must be provided in clientCACertRef",
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef:   &corev1.TypedLocalObjectReference{},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "if admin API mTLS enabled and clientCACertRef is set, the CA certificate secret must exist",
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "does-not-exist",
							Kind: "Secret",
						},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "if admin API mTLS enabled, clientCACertRef is set and cluster is being deleted, skip update validation",
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "does-not-exist",
							Kind: "Secret",
						},
					},
				},
			},
			clusterFn: func(c *vectorizedv1alpha1.Cluster) {
				deleteTime := metav1.Now()
				c.DeletionTimestamp = &deleteTime
			},
			skipCreate:  true,
			updateError: false,
		},
		{
			name: "if admin API mTLS enabled and clientCACertRef is set, the CA certificate secret must provide ca.crt",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert-no-ca-crt",
				},
				Data: map[string][]byte{"no.ca.crt": []byte("no.ca.crt")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert-no-ca-crt",
							Kind: "Secret",
						},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "if admin API mTLS enabled and clientCACertRef is set, the CA certificate secret must be in PEM",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert-invalid-pem",
				},
				Data: map[string][]byte{"ca.crt": []byte("not-in-pem")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert-invalid-pem",
							Kind: "Secret",
						},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "if admin API mTLS enabled and clientCACertRef is set, the certificate secret must be valid x509 certificate",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert-not-ca-cert",
				},
				Data: map[string][]byte{"ca.crt": readFile(t, "./testdata/not.ca.crt.pem")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert-not-ca-cert",
							Kind: "Secret",
						},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "admin API mTLS with clientCACertRef can have one of issuer ref or node secret ref",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert-in",
				},
				Data: map[string][]byte{"ca.crt": readFile(t, "./testdata/ca.crt.pem")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
				},
				{
					Port: 30644,
					External: vectorizedv1alpha1.ExternalConnectivityConfig{
						Enabled:   true,
						Subdomain: "panda.dev",
					},
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert-in",
							Kind: "Secret",
						},
						RequireClientAuth: true,
						IssuerRef: &cmmetav1.ObjectReference{
							Kind: "ClusterIssuer",
							Name: "letsencrypt",
						},
						NodeSecretRef: &corev1.ObjectReference{
							Kind:       "Secret",
							Name:       "mysecret",
							APIVersion: "v1",
						},
					},
				},
			},
			createError: true,
			updateError: true,
		},
		{
			name: "if admin API mTLS enabled and clientCACertRef is set to a valid x509 certificate, cluster is accepted",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert",
				},
				Data: map[string][]byte{"ca.crt": readFile(t, "./testdata/ca.crt.pem")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert",
							Kind: "Secret",
						},
					},
				},
			},
			createError: false,
			updateError: false,
		},
		{
			name: "admin API mTLS with clientCACertRef can be enabled also on the external listener only",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "admin-api-client-ca-cert-ext",
				},
				Data: map[string][]byte{"ca.crt": readFile(t, "./testdata/ca.crt.pem")},
			},
			adminAPI: []vectorizedv1alpha1.AdminAPI{
				{
					Port: 9644,
				},
				{
					Port: 30644,
					External: vectorizedv1alpha1.ExternalConnectivityConfig{
						Enabled:   true,
						Subdomain: "panda.dev",
					},
					TLS: vectorizedv1alpha1.AdminAPITLS{
						Enabled:           true,
						RequireClientAuth: true,
						ClientCACertRef: &corev1.TypedLocalObjectReference{
							Name: "admin-api-client-ca-cert-ext",
							Kind: "Secret",
						},
					},
				},
			},
			createError: false,
			updateError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vectorizedv1alpha1.SetK8sClient(fakeK8sClient)
			adminCluster := validRedpandaCluster()
			if tc.secret != nil {
				if tc.secret.Namespace == "" {
					tc.secret.Namespace = adminCluster.Namespace
				}
				err := fakeK8sClient.Create(context.Background(), tc.secret)
				assert.NoError(t, err)
			}
			adminCluster.Spec.Configuration.AdminAPI = tc.adminAPI
			if tc.clusterFn != nil {
				tc.clusterFn(adminCluster)
			}
			if !tc.skipCreate {
				_, err := webhook.ValidateCreate(context.Background(), adminCluster)
				if tc.createError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
			_, err := webhook.ValidateUpdate(context.Background(), validRedpandaCluster(), adminCluster)
			if tc.updateError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func validRedpandaCluster() *vectorizedv1alpha1.Cluster {
	return &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{
					Name:     "test",
					Replicas: ptr.To(int32(3)),
				},
			},
			Replicas: ptr.To(int32(1)),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI:       []vectorizedv1alpha1.KafkaAPI{{Port: 124, AuthenticationMethod: "none"}},
				AdminAPI:       []vectorizedv1alpha1.AdminAPI{{Port: 126}},
				RPCServer:      vectorizedv1alpha1.SocketAddress{Port: 128},
				SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{Port: 130},
				PandaproxyAPI:  []vectorizedv1alpha1.PandaproxyAPI{{Port: 132}},
			},
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
						corev1.ResourceCPU:    resource.MustParse("1"),
					},
				},
				Redpanda: nil,
			},
		},
	}
}

func TestPodDisruptionBudget(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	rpCluster := validRedpandaCluster()
	value := intstr.FromInt(1)

	t.Run("pdb not enabled is valid", func(t *testing.T) {
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = &vectorizedv1alpha1.PDBConfig{
			Enabled: false,
		}

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.NoError(t, err)
	})

	t.Run("pdb with only maxunavailable is valid", func(t *testing.T) {
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = &vectorizedv1alpha1.PDBConfig{
			Enabled:        true,
			MaxUnavailable: &value,
		}

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.NoError(t, err)
	})

	t.Run("pdb with only minavailable is valid", func(t *testing.T) {
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = &vectorizedv1alpha1.PDBConfig{
			Enabled:      true,
			MinAvailable: &value,
		}

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.NoError(t, err)
	})

	t.Run("pdb with both minavailable and maxunavailable is invalid", func(t *testing.T) {
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = &vectorizedv1alpha1.PDBConfig{
			Enabled:        true,
			MinAvailable:   &value,
			MaxUnavailable: &value,
		}

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.Error(t, err)
	})

	t.Run("pdb with minavailable but enabled=false is invalid", func(t *testing.T) {
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = &vectorizedv1alpha1.PDBConfig{
			Enabled:      false,
			MinAvailable: &value,
		}

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.Error(t, err)
	})

	t.Run("pdb is nil", func(t *testing.T) {
		// this can happen only if webhook is disabled
		rpc := rpCluster.DeepCopy()
		rpc.Spec.PodDisruptionBudget = nil

		_, err := webhook.ValidateCreate(context.Background(), rpc)
		assert.NoError(t, err)
	})
}

//nolint:funlen // matrix test has many cases
func TestRangesAndCollisions(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	cases := []struct {
		name                   string
		kafkaInternal          int
		kafkaExternal          int
		adminAPIInternal       int
		adminAPIExternal       int
		pandaproxyAPIInternal  int
		pandaproxyAPIExternal  int
		schemaRegistryPort     int
		schemaRegistryExternal bool
		schemaRegistryStatic   bool
		error                  bool
	}{
		{
			name:                  "working",
			kafkaInternal:         9092,
			kafkaExternal:         30092,
			adminAPIInternal:      9644,
			adminAPIExternal:      30644,
			pandaproxyAPIInternal: 8081,
			pandaproxyAPIExternal: 30081,
			schemaRegistryPort:    8082,
		},
		{
			name:             "collision kafka and admin",
			kafkaInternal:    9092,
			kafkaExternal:    30092,
			adminAPIInternal: 9644,
			adminAPIExternal: 30092,
			error:            true,
		},
		{
			name:                  "collision admin and panda",
			kafkaInternal:         9092,
			kafkaExternal:         30092,
			adminAPIInternal:      9644,
			adminAPIExternal:      30644,
			pandaproxyAPIInternal: 8081,
			pandaproxyAPIExternal: 30644,
			error:                 true,
		},
		{
			name:                   "collision panda and schema",
			kafkaInternal:          9092,
			kafkaExternal:          30092,
			adminAPIInternal:       9644,
			adminAPIExternal:       30644,
			pandaproxyAPIInternal:  8081,
			pandaproxyAPIExternal:  30644,
			schemaRegistryPort:     30644,
			schemaRegistryExternal: true,
			schemaRegistryStatic:   true,
			error:                  true,
		},
		{
			name:             "kafka outside range",
			kafkaInternal:    9092,
			kafkaExternal:    29999,
			adminAPIInternal: 9644,
			error:            true,
		},
		{
			name:             "admin outside range",
			kafkaInternal:    9092,
			kafkaExternal:    30092,
			adminAPIInternal: 9644,
			adminAPIExternal: 29999,
			error:            true,
		},
		{
			name:                  "pandaproxy outside range",
			kafkaInternal:         9092,
			kafkaExternal:         30092,
			adminAPIInternal:      9644,
			pandaproxyAPIInternal: 8081,
			pandaproxyAPIExternal: 29999,
			error:                 true,
		},
		{
			name:                   "schema registry outside range",
			kafkaInternal:          9092,
			kafkaExternal:          30092,
			adminAPIInternal:       9644,
			schemaRegistryPort:     29999,
			schemaRegistryExternal: true,
			schemaRegistryStatic:   true,
			error:                  true,
		},
		{
			name:                   "schema registry allowed when auto generated",
			kafkaInternal:          9092,
			kafkaExternal:          30092,
			adminAPIInternal:       9644,
			schemaRegistryPort:     29999,
			schemaRegistryExternal: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpCluster := validRedpandaCluster()
			c := rpCluster.DeepCopy()

			c.Spec.Configuration.KafkaAPI = []vectorizedv1alpha1.KafkaAPI{}
			c.Spec.Configuration.AdminAPI = []vectorizedv1alpha1.AdminAPI{}
			c.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{}
			c.Spec.Configuration.SchemaRegistry = nil

			if tc.kafkaInternal != 0 {
				c.Spec.Configuration.KafkaAPI = append(c.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "none", Port: tc.kafkaInternal})
			}
			if tc.kafkaExternal != 0 {
				c.Spec.Configuration.KafkaAPI = append(c.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "none", Port: tc.kafkaExternal, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
			}
			if tc.adminAPIInternal != 0 {
				c.Spec.Configuration.AdminAPI = append(c.Spec.Configuration.AdminAPI, vectorizedv1alpha1.AdminAPI{Port: tc.adminAPIInternal})
			}
			if tc.adminAPIExternal != 0 {
				c.Spec.Configuration.AdminAPI = append(c.Spec.Configuration.AdminAPI, vectorizedv1alpha1.AdminAPI{Port: tc.adminAPIExternal, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
			}
			if tc.pandaproxyAPIInternal != 0 {
				c.Spec.Configuration.PandaproxyAPI = append(c.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{Port: tc.pandaproxyAPIInternal})
			}
			if tc.pandaproxyAPIExternal != 0 {
				c.Spec.Configuration.PandaproxyAPI = append(c.Spec.Configuration.PandaproxyAPI, vectorizedv1alpha1.PandaproxyAPI{Port: tc.pandaproxyAPIExternal, External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
					ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
				}})
			}
			if tc.schemaRegistryPort != 0 && !tc.schemaRegistryExternal {
				c.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{Port: tc.schemaRegistryPort}
			} else if tc.schemaRegistryPort != 0 && tc.schemaRegistryExternal {
				c.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{Port: tc.schemaRegistryPort, External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
					ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
					StaticNodePort:             tc.schemaRegistryStatic,
				}}
			}

			_, err := webhook.ValidateUpdate(context.Background(), rpCluster, c)
			if tc.error {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // this is ok for a test
func TestKafkaTLSRules(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	rpCluster := validRedpandaCluster()

	//nolint:dupl // the tests are not duplicates
	t.Run("different issuer for two tls listeners", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
			Enabled: true,
			IssuerRef: &cmmetav1.ObjectReference{
				Name: "issuer",
				Kind: "ClusterIssuer",
			},
		}
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 30001, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "redpanda.com"}, TLS: vectorizedv1alpha1.KafkaAPITLS{
				Enabled: true,
				IssuerRef: &cmmetav1.ObjectReference{
					Name: "other",
					Kind: "ClusterIssuer",
				},
			}})

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.Error(t, err)
	})

	t.Run("same issuer for two tls listeners is allowed", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
			Enabled: true,
			IssuerRef: &cmmetav1.ObjectReference{
				Name: "issuer",
				Kind: "ClusterIssuer",
			},
		}
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				AuthenticationMethod: "none",
				Port:                 30001,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{
					Enabled: true, Subdomain: "redpanda.com",
				}, TLS: vectorizedv1alpha1.KafkaAPITLS{
					Enabled: true,
					IssuerRef: &cmmetav1.ObjectReference{
						Name: "issuer",
						Kind: "ClusterIssuer",
					},
				},
			})

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})

	//nolint:dupl // the tests are not duplicates
	t.Run("different nodeSecretRef for two tls listeners", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
			Enabled: true,
			NodeSecretRef: &corev1.ObjectReference{
				Name:      "node",
				Namespace: "default",
			},
		}
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{Port: 30001, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "redpanda.com"}, TLS: vectorizedv1alpha1.KafkaAPITLS{
				Enabled: true,
				NodeSecretRef: &corev1.ObjectReference{
					Name:      "other-node",
					Namespace: "default",
				},
			}})

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.Error(t, err)
	})

	t.Run("same nodesecretref for two tls listeners is allowed", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
			Enabled: true,
			NodeSecretRef: &corev1.ObjectReference{
				Name:      "node",
				Namespace: "default",
			},
		}
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				AuthenticationMethod: "none",
				Port:                 30001,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{
					Enabled:   true,
					Subdomain: "redpanda.com",
				},
				TLS: vectorizedv1alpha1.KafkaAPITLS{
					Enabled: true,
					NodeSecretRef: &corev1.ObjectReference{
						Name:      "node",
						Namespace: "default",
					},
				},
			})

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})
}

func TestKafkaAuthenticationMethod(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	rpCluster := validRedpandaCluster()

	t.Run("no authentication method provided", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				AuthenticationMethod: "",
				Port:                 30001,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{
					Enabled:   true,
					Subdomain: "redpanda.com",
				},
			})

		_, err := webhook.ValidateCreate(context.Background(), newRp)
		assert.Error(t, err)

		_, err = webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.Error(t, err)
	})

	t.Run("sasl authentication method provided", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				AuthenticationMethod: "sasl",
				Port:                 30001,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{
					Enabled:   true,
					Subdomain: "redpanda.com",
				},
			})

		_, err := webhook.ValidateCreate(context.Background(), newRp)
		assert.NoError(t, err)

		_, err = webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})

	t.Run("mtls_identity authentication method provided", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.Configuration.KafkaAPI = append(newRp.Spec.Configuration.KafkaAPI,
			vectorizedv1alpha1.KafkaAPI{
				AuthenticationMethod: "mtls_identity",
				Port:                 30001,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{
					Enabled:   true,
					Subdomain: "redpanda.com",
				},
			})

		_, err := webhook.ValidateCreate(context.Background(), newRp)
		assert.NoError(t, err)

		_, err = webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})
}

func TestCloudStorage(t *testing.T) {
	webhook := &vectorizedv1alpha1.Cluster{}

	rpCluster := validRedpandaCluster()

	const (
		bucket    = "bucket"
		region    = "us-west-1"
		accessKey = "key"
		secretKey = "secret"
		namespace = "ns"
	)

	t.Run("valid cloud storage with config file", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.CloudStorage.Enabled = true
		newRp.Spec.CloudStorage.Bucket = bucket
		newRp.Spec.CloudStorage.Region = region
		newRp.Spec.CloudStorage.AccessKey = accessKey
		newRp.Spec.CloudStorage.SecretKeyRef.Name = secretKey
		newRp.Spec.CloudStorage.SecretKeyRef.Namespace = namespace

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})

	t.Run("invalid cloud storage with config file (no access key)", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.CloudStorage.Enabled = true
		newRp.Spec.CloudStorage.Bucket = bucket
		newRp.Spec.CloudStorage.Region = region
		newRp.Spec.CloudStorage.SecretKeyRef.Name = secretKey
		newRp.Spec.CloudStorage.SecretKeyRef.Namespace = namespace

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.Error(t, err)
	})

	t.Run("invalid cloud storage with config file (no secret)", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.CloudStorage.Enabled = true
		newRp.Spec.CloudStorage.CredentialsSource = vectorizedv1alpha1.CredentialsSourceConfigFile
		newRp.Spec.CloudStorage.Bucket = bucket
		newRp.Spec.CloudStorage.Region = region
		newRp.Spec.CloudStorage.AccessKey = accessKey

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.Error(t, err)
	})

	t.Run("valid cloud storage with sts", func(t *testing.T) {
		newRp := rpCluster.DeepCopy()
		newRp.Spec.CloudStorage.Enabled = true
		newRp.Spec.CloudStorage.CredentialsSource = vectorizedv1alpha1.CredentialsSource("sts")
		newRp.Spec.CloudStorage.Bucket = bucket
		newRp.Spec.CloudStorage.Region = region

		_, err := webhook.ValidateUpdate(context.Background(), rpCluster, newRp)
		assert.NoError(t, err)
	})
}

func readFile(t *testing.T, name string) []byte {
	f, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", name, err)
	}
	return f
}
