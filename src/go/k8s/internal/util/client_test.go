package util

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
)

func ensureMapAndSetValue(values map[string]interface{}, name, key string, value interface{}) {
	if v, ok := values[name]; ok {
		m := v.(map[string]interface{})
		m[key] = value
		values[name] = m

		return
	}

	values[name] = map[string]interface{}{
		key: value,
	}
}

func TestClientFactory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping factory tests in short mode")
	}

	stack := testutils.RunK3sStack(t, testutils.K3sStackOptions{
		InstallationTimeout: 2 * time.Minute,
	})
	client := stack.Client()
	factory := NewClientFactory(client)
	factory.remapper = stack

	clusterClient := func(ctx context.Context, cluster *v1alpha2.Redpanda) (*kadm.Client, error) {
		return factory.ClusterAdmin(ctx, cluster)
	}
	kafkaAPISpecClient := func(ctx context.Context, cluster *v1alpha2.Redpanda) (*kadm.Client, error) {
		return factory.Admin(ctx, cluster.Namespace, testutils.KafkaAPISpecFromCluster(cluster))
	}

	for name, tt := range map[string]struct {
		Version        string
		Values         map[string]interface{}
		GetAdminClient func(context.Context, *v1alpha2.Redpanda) (*kadm.Client, error)
	}{
		"Cluster: basic test with TLS": {
			Values: map[string]interface{}{
				"tls": map[string]interface{}{
					"enabled": true,
				},
			},
			GetAdminClient: clusterClient,
		},
		"KafkaAPISpec: basic test with TLS": {
			Values: map[string]interface{}{
				"tls": map[string]interface{}{
					"enabled": true,
				},
			},
			GetAdminClient: kafkaAPISpecClient,
		},
		"Cluster: basic test no TLS": {
			Values: map[string]interface{}{
				"tls": map[string]interface{}{
					"enabled": false,
				},
			},
			GetAdminClient: clusterClient,
		},
		"KafkaAPISpec: basic test no TLS": {
			Values: map[string]interface{}{
				"tls": map[string]interface{}{
					"enabled": false,
				},
			},
			GetAdminClient: kafkaAPISpecClient,
		},
		"Cluster: SASL SCRAM": {
			Values: map[string]interface{}{
				"auth": map[string]interface{}{
					"sasl": map[string]interface{}{
						"enabled":   true,
						"secretRef": "users",
						"users": []map[string]interface{}{{
							"name":      "admin",
							"password":  "change-me",
							"mechanism": "SCRAM-SHA-512",
						}},
					},
				},
			},
			GetAdminClient: clusterClient,
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// to reduce the bootup time of the cluster
			ensureMapAndSetValue(tt.Values, "statefulset", "replicas", 1)
			ensureMapAndSetValue(tt.Values, "console", "enabled", false)
			// to keep nodeport services from conflicting
			ensureMapAndSetValue(tt.Values, "external", "enabled", false)
			ensureMapAndSetValue(tt.Values, "image", "tag", "v24.2.2")

			cluster := stack.InstallCluster(t, testutils.RedpandaOptions{
				HelmVersion: tt.Version,
				Values:      tt.Values,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()

			stack.Forward(t, cluster, adminPort)

			adminClient, err := tt.GetAdminClient(ctx, cluster)
			require.NoError(t, err)

			metadata, err := adminClient.BrokerMetadata(ctx)
			require.NoError(t, err)
			require.Len(t, metadata.Brokers.NodeIDs(), 1)
		})
	}
}
