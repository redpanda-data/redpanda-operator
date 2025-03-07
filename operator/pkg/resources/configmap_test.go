// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

func TestEnsureConfigMap(t *testing.T) {
	require.NoError(t, vectorizedv1alpha1.AddToScheme(scheme.Scheme))
	clusterWithExternal := pandaCluster().DeepCopy()
	clusterWithExternal.Spec.Configuration.KafkaAPI = append(clusterWithExternal.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{AuthenticationMethod: "sasl", Port: 30001, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
	clusterWithMultipleKafkaTLS := pandaCluster().DeepCopy()
	clusterWithMultipleKafkaTLS.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{Enabled: true}
	clusterWithMultipleKafkaTLS.Spec.Configuration.KafkaAPI = append(clusterWithMultipleKafkaTLS.Spec.Configuration.KafkaAPI, vectorizedv1alpha1.KafkaAPI{Port: 30001, TLS: vectorizedv1alpha1.KafkaAPITLS{Enabled: true}, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
	clusterWithVersion22_2 := pandaCluster().DeepCopy()
	clusterWithVersion22_2.Spec.Version = "v22.2.0"
	clusterWithVersion22_3 := pandaCluster().DeepCopy()
	clusterWithVersion22_3.Spec.Version = "v22.3.0"

	testcases := []struct {
		name             string
		cluster          vectorizedv1alpha1.Cluster
		expectedString   string
		unExpectedString string
	}{
		{
			name:    "External port specified",
			cluster: *clusterWithExternal,
			expectedString: `- address: 0.0.0.0
          port: 30001
          name: kafka-external`,
		},
		{
			name:    "Multiple Kafka TLS",
			cluster: *clusterWithMultipleKafkaTLS,
			expectedString: `- name: kafka
          key_file: /etc/tls/certs/tls.key
          cert_file: /etc/tls/certs/tls.crt
          enabled: true
        - name: kafka-external
          key_file: /etc/tls/certs/tls.key
          cert_file: /etc/tls/certs/tls.crt
          enabled: true`,
		},
		{
			name:           "Disable empty_seed_starts_cluster",
			cluster:        *clusterWithVersion22_3,
			expectedString: "empty_seed_starts_cluster: false",
		},
	}
	for i, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().Build()
			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "archival",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"archival": []byte("XXX"),
				},
			}
			require.NoError(t, c.Create(context.TODO(), &secret))
			cfgRes := resources.NewConfigMap(
				c,
				&testcases[i].cluster,
				scheme.Scheme,
				"cluster.local",
				types.NamespacedName{Name: "test", Namespace: "test"},
				types.NamespacedName{Name: "test", Namespace: "test"},
				TestBrokerTLSConfigProvider{},
				ctrl.Log.WithName("test"))
			require.NoError(t, cfgRes.Ensure(context.TODO()))

			actual := &corev1.ConfigMap{}
			err := c.Get(context.Background(), cfgRes.Key(), actual)
			require.NoError(t, err)
			data := actual.Data["redpanda.yaml"]
			if tc.expectedString != "" {
				require.True(t, strings.Contains(data, tc.expectedString), fmt.Sprintf("expecting %s but got %v", tc.expectedString, data))
			}
			if tc.unExpectedString != "" {
				require.False(t, strings.Contains(data, tc.unExpectedString), fmt.Sprintf("expecting %s to be absent but got %v", tc.unExpectedString, data))
			}
		})
	}
}

func TestEnsureConfigMap_AdditionalConfig(t *testing.T) {
	require.NoError(t, vectorizedv1alpha1.AddToScheme(scheme.Scheme))

	testcases := []struct {
		name                    string
		additionalConfiguration map[string]string
		expectedStrings         []string
		expectedHash            string
	}{
		{
			name:                    "Primitive object in additional configuration",
			additionalConfiguration: map[string]string{"redpanda.transactional_id_expiration_ms": "25920000000", "rpk.overprovisioned": "true"},
			expectedStrings:         []string{"transactional_id_expiration_ms: 25920000000"},
			expectedHash:            "78b397cdd7924ba28cea07b55e0c8444",
		},
		{
			name:                    "Complex struct in additional configuration",
			additionalConfiguration: map[string]string{"schema_registry.schema_registry_api": "[{'name':'external','address':'0.0.0.0','port':8081}]}"},
			expectedStrings: []string{`schema_registry:
    schema_registry_api:
        - address: 0.0.0.0
          port: 8081
          name: external`},
			expectedHash: "a5d7af0c3bafb1488e1d147da992cf11",
		},
		{
			name: "shadow index cache directory",
			expectedStrings: []string{
				`cloud_storage_cache_directory: /var/lib/shadow-index-cache`,
				`cloud_storage_cache_size: "10737418240"`,
			},
			expectedHash: "3b8a2186bb99ebb9b3db10452cdfd45a",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			panda := pandaCluster().DeepCopy()
			panda.Spec.AdditionalConfiguration = tc.additionalConfiguration
			c := fake.NewClientBuilder().Build()
			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "archival",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"archival": []byte("XXX"),
				},
			}
			require.NoError(t, c.Create(context.TODO(), &secret))
			cfgRes := resources.NewConfigMap(
				c,
				panda,
				scheme.Scheme,
				"cluster.local",
				types.NamespacedName{Name: "test", Namespace: "test"},
				types.NamespacedName{Name: "test", Namespace: "test"},
				TestBrokerTLSConfigProvider{},
				ctrl.Log.WithName("test"))
			require.NoError(t, cfgRes.Ensure(context.TODO()))

			actual := &corev1.ConfigMap{}
			err := c.Get(context.Background(), cfgRes.Key(), actual)
			require.NoError(t, err)
			data := actual.Data["redpanda.yaml"]
			for _, es := range tc.expectedStrings {
				require.True(t, strings.Contains(data, es), fmt.Sprintf("expecting %s but got %v", es, data))
			}

			fileName := strings.ReplaceAll("./testdata/"+tc.name+".golden", " ", "_")
			if os.Getenv("OVERWRITE_GOLDEN_FILES") != "" {
				err = os.WriteFile(fileName, []byte(data), 0o600)
				require.NoError(t, err)
			}
			golden, err := os.ReadFile(fileName)
			require.NoError(t, err)
			require.Equal(t, string(golden), data)
			hash, err := cfgRes.GetNodeConfigHash(context.TODO())
			require.NoError(t, err)
			require.Equal(t, tc.expectedHash, hash)
		})
	}
}

func TestConfigMapResource_replicas(t *testing.T) { //nolint:funlen // test table is long
	logger := logr.Discard()
	tests := []struct {
		name        string
		clusterName string
		clusterFQDN string
		replicas    int32
		wantArgs    *config.RedpandaNodeConfig
		wantErr     bool
	}{
		{
			name:        "create seed server list with 1 node",
			clusterName: "onenode",
			clusterFQDN: "domain.dom",
			replicas:    1,
			wantArgs: &config.RedpandaNodeConfig{
				EmptySeedStartsCluster: ptr.To(false),
				SeedServers: []config.SeedServer{
					{
						Host: config.SocketAddress{
							Address: "onenode-first-0.domain.dom",
							Port:    33145,
						},
					},
				},
			},
		},
		{
			name:        "create seed server list with 3 nodes",
			replicas:    3,
			clusterName: "threenode",
			clusterFQDN: "domain.dom",
			wantArgs: &config.RedpandaNodeConfig{
				EmptySeedStartsCluster: ptr.To(false),
				SeedServers: []config.SeedServer{
					{
						Host: config.SocketAddress{
							Address: "threenode-first-0.domain.dom",
							Port:    33145,
						},
					},
					{
						Host: config.SocketAddress{
							Address: "threenode-first-1.domain.dom",
							Port:    33145,
						},
					},
					{
						Host: config.SocketAddress{
							Address: "threenode-first-2.domain.dom",
							Port:    33145,
						},
					},
				},
			},
		},
		{
			name:     "fail to create seed server list with 0 nodes",
			replicas: 0,
			wantErr:  true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().Build()

			p := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.clusterName,
					Namespace: "namespace",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					NodePools: []vectorizedv1alpha1.NodePoolSpec{
						{
							Name:     "first",
							Replicas: &tt.replicas,
						},
					},
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: 123,
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled: false,
								},
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{
							{
								Port: 5678,
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled: false,
								},
							},
						},
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								Port: 91011,
							},
						},
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							Port: 121314,
						},
					},
				},
			}

			r := resources.NewConfigMap(c, p, nil, tt.clusterFQDN, types.NamespacedName{Namespace: "namespace", Name: "internal"}, types.NamespacedName{Namespace: "namespace", Name: "external"}, TestBrokerTLSConfigProvider{}, logger)

			cfg, err := r.CreateConfiguration(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			} else if !tt.wantErr {
				require.NoError(t, err)
			}

			require.Equal(t, tt.wantArgs.SeedServers, cfg.NodeConfiguration.Redpanda.SeedServers)
			// Assert that the PandaproxyClient and SchemaRegistryClient configurations remain stable regardless of changes to replicas.
			require.Equal(t, []config.SocketAddress{{Address: tt.clusterFQDN, Port: 123}}, cfg.NodeConfiguration.PandaproxyClient.Brokers)
			require.Equal(t, []config.SocketAddress{{Address: tt.clusterFQDN, Port: 123}}, cfg.NodeConfiguration.SchemaRegistryClient.Brokers)
		})
	}
}

func TestConfigmap_BrokerTLSClients(t *testing.T) {
	panda := pandaCluster().DeepCopy()
	panda.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
		Enabled:           true,
		RequireClientAuth: true,
	}
	panda.Spec.Configuration.SchemaRegistry = &vectorizedv1alpha1.SchemaRegistryAPI{
		Port: 8081,
	}
	panda.Spec.Configuration.PandaproxyAPI = []vectorizedv1alpha1.PandaproxyAPI{
		{Port: 8082},
	}
	c := fake.NewClientBuilder().Build()
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "archival",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"archival": []byte("XXX"),
		},
	}
	require.NoError(t, c.Create(context.TODO(), &secret))
	cfgRes := resources.NewConfigMap(
		c,
		panda,
		scheme.Scheme,
		"cluster.local",
		types.NamespacedName{Name: "test", Namespace: "test"},
		types.NamespacedName{Name: "test", Namespace: "test"},
		TestBrokerTLSConfigProvider{},
		ctrl.Log.WithName("test"))
	require.NoError(t, cfgRes.Ensure(context.TODO()))

	actual := &corev1.ConfigMap{}
	err := c.Get(context.Background(), cfgRes.Key(), actual)
	require.NoError(t, err)
	data := actual.Data["redpanda.yaml"]
	cfg := config.ProdDefault()
	require.NoError(t, yaml.Unmarshal([]byte(data), cfg))
	require.Equal(t, "/etc/tls/certs/ca/tls.key", cfg.PandaproxyClient.BrokerTLS.KeyFile)
	require.Equal(t, "/etc/tls/certs/ca/tls.crt", cfg.PandaproxyClient.BrokerTLS.CertFile)
	require.Equal(t, "/etc/tls/certs/ca.crt", cfg.PandaproxyClient.BrokerTLS.TruststoreFile)
	require.Equal(t, "/etc/tls/certs/ca/tls.key", cfg.SchemaRegistryClient.BrokerTLS.KeyFile)
	require.Equal(t, "/etc/tls/certs/ca/tls.crt", cfg.SchemaRegistryClient.BrokerTLS.CertFile)
	require.Equal(t, "/etc/tls/certs/ca.crt", cfg.SchemaRegistryClient.BrokerTLS.TruststoreFile)
}

type TestBrokerTLSConfigProvider struct{}

func (TestBrokerTLSConfigProvider) KafkaClientBrokerTLS(mountPoints *resourcetypes.TLSMountPoints) *config.ServerTLS {
	return &config.ServerTLS{
		KeyFile:        "/etc/tls/certs/ca/tls.key",
		CertFile:       "/etc/tls/certs/ca/tls.crt",
		TruststoreFile: "/etc/tls/certs/ca.crt",
		Enabled:        true,
	}
}
