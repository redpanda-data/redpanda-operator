package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redpanda-data/helm-charts/pkg/helm"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/cluster.redpanda.com/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	k3sVersion   = "rancher/k3s:v1.27.1-k3s1"
	chartVersion = ""
)

func ensureMapAndSetValue(values map[string]any, name, key string, value any) {
	if v, ok := values[name]; ok {
		m := v.(map[string]any)
		m[key] = value
		values[name] = m

		return
	}

	values[name] = map[string]any{
		key: value,
	}
}

func TestClientFactory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping factory tests in short mode")
	}

	var suffix atomic.Int32

	ctx := context.Background()

	container, err := k3s.Run(ctx, k3sVersion)
	require.NoError(t, err)
	t.Cleanup(func() {
		container.Terminate(context.Background())
	})

	config, err := container.GetKubeConfig(ctx)
	require.NoError(t, err)
	restcfg, err := clientcmd.RESTConfigFromKubeConfig(config)
	require.NoError(t, err)

	helmClient, err := helm.New(helm.Options{
		KubeConfig: restcfg,
	})
	require.NoError(t, err)
	require.NoError(t, helmClient.RepoAdd(ctx, "jetstack", "https://charts.jetstack.io"))
	require.NoError(t, helmClient.RepoAdd(ctx, "redpandadata", "https://charts.redpanda.com"))

	_, err = helmClient.Install(ctx, "jetstack/cert-manager", helm.InstallOptions{
		CreateNamespace: true,
		Name:            "cert-manager",
		Namespace:       "cert-manager",
		Values: map[string]any{
			"crds": map[string]any{
				"enabled": true,
			},
		},
	})
	require.NoError(t, err)

	factory, err := NewClientFactory(restcfg)
	require.NoError(t, err)

	factory = factory.WithDialer(kube.NewPodDialer(restcfg).DialContext)

	type credentials struct {
		Name      string
		Password  string
		Mechanism string
	}

	for name, tt := range map[string]struct {
		TLS  bool
		Auth *credentials
	}{
		"TLS": {
			TLS: true,
		},
		"no TLS": {
			TLS: false,
		},
		"TLS+SCRAM-512": {
			TLS: true,
			Auth: &credentials{
				Name:      "admin",
				Password:  "change-me",
				Mechanism: "SCRAM-SHA-512",
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			values := map[string]any{}
			ensureMapAndSetValue(values, "tls", "enabled", tt.TLS)
			if tt.Auth != nil {
				ensureMapAndSetValue(values, "auth", "sasl", map[string]any{
					"enabled":   true,
					"secretRef": "users",
					"users": []map[string]any{{
						"name":      tt.Auth.Name,
						"password":  tt.Auth.Password,
						"mechanism": tt.Auth.Mechanism,
					}},
				})
			}

			// to reduce the bootup time of the cluster
			ensureMapAndSetValue(values, "statefulset", "replicas", 1)
			ensureMapAndSetValue(values, "console", "enabled", false)
			// to keep nodeport services from conflicting
			ensureMapAndSetValue(values, "external", "enabled", false)
			ensureMapAndSetValue(values, "image", "tag", "v24.2.2")

			name := fmt.Sprintf("k3s-%d-%d", time.Now().Unix(), suffix.Add(1))

			_, err := helmClient.Install(ctx, "redpandadata/redpanda", helm.InstallOptions{
				Version:         chartVersion,
				CreateNamespace: true,
				Name:            name,
				Namespace:       name,
				Values:          values,
			})
			require.NoError(t, err)

			t.Run("Cluster", func(t *testing.T) {
				var cluster v1alpha2.Redpanda
				cluster.Name = name
				cluster.Namespace = name
				cluster.Spec.ClusterSpec = &v1alpha2.RedpandaClusterSpec{}

				data, err := json.Marshal(values)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, cluster.Spec.ClusterSpec))

				adminClient, err := factory.ClusterAdmin(ctx, &cluster)
				require.NoError(t, err)
				metadata, err := adminClient.BrokerMetadata(ctx)
				require.NoError(t, err)
				require.Len(t, metadata.Brokers.NodeIDs(), 1)
			})

			t.Run("KafkaAPISpec", func(t *testing.T) {
				var spec redpandav1alpha1.KafkaAPISpec
				spec.Brokers = []string{fmt.Sprintf("%s-0.%s.%s.svc:9093", name, name, name)}
				if tt.Auth != nil {
					require.NoError(t, factory.Create(ctx, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret",
							Namespace: name,
						},
						StringData: map[string]string{
							"password": tt.Auth.Password,
						},
					}))

					spec.SASL = &redpandav1alpha1.KafkaSASL{
						Username: tt.Auth.Name,
						Password: redpandav1alpha1.SecretKeyRef{
							Name: "secret",
							Key:  "password",
						},
						Mechanism: redpandav1alpha1.SASLMechanism(tt.Auth.Mechanism),
					}
				}
				if tt.TLS {
					spec.TLS = &redpandav1alpha1.KafkaTLS{
						CaCert: &redpandav1alpha1.SecretKeyRef{
							Name: fmt.Sprintf("%s-default-root-certificate", name),
							Key:  corev1.TLSCertKey,
						},
						// this is necessary since we don't have a way of setting a ServerName
						// and one or the other must be set according to franz-go
						InsecureSkipTLSVerify: true,
					}
				}
				adminClient, err := factory.Admin(ctx, name, nil, &spec)
				require.NoError(t, err)
				metadata, err := adminClient.BrokerMetadata(ctx)
				require.NoError(t, err)
				require.Len(t, metadata.Brokers.NodeIDs(), 1)
			})
		})
	}
}
