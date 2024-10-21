// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/helm-charts/pkg/helm"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var chartVersion = ""

func init() {
	log.SetLogger(logr.Discard())
}

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

type fakeObject struct {
	metav1.ObjectMeta
	metav1.TypeMeta

	kafkaSpec *redpandav1alpha2.KafkaAPISpec
}

func wrapSpec(name string, spec *redpandav1alpha2.KafkaAPISpec) *fakeObject {
	return &fakeObject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name,
		},
		kafkaSpec: spec,
	}
}

func (f *fakeObject) GetKafkaAPISpec() *redpandav1alpha2.KafkaAPISpec {
	return f.kafkaSpec
}

func (f *fakeObject) DeepCopyObject() runtime.Object {
	return f
}

func TestClientFactory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping factory tests in short mode")
	}

	var suffix atomic.Int32

	ctx := context.Background()
	cluster, err := k3d.NewCluster(t.Name())
	require.NoError(t, err)
	t.Logf("created cluster %T %q", cluster, cluster.Name)

	t.Cleanup(func() {
		if testutil.Retain() {
			t.Logf("retain flag is set; not deleting cluster %q", cluster.Name)
			return
		}
		t.Logf("Deleting cluster %q", cluster.Name)
		require.NoError(t, cluster.Cleanup())
	})

	restcfg := cluster.RESTConfig()

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, redpandav1alpha2.AddToScheme(s))
	require.NoError(t, redpandav1alpha1.AddToScheme(s))
	kubeClient, err := client.New(restcfg, client.Options{Scheme: s, WarningHandler: client.WarningHandlerOptions{SuppressWarnings: true}})
	require.NoError(t, err)

	helmClient, err := helm.New(helm.Options{
		KubeConfig: restcfg,
	})
	require.NoError(t, err)
	require.NoError(t, helmClient.RepoAdd(ctx, "redpandadata", "https://charts.redpanda.com"))

	factory := NewFactory(restcfg, kubeClient).WithDialer(kube.NewPodDialer(restcfg).DialContext)

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
				var cluster redpandav1alpha2.Redpanda
				cluster.Name = name
				cluster.Namespace = name
				cluster.Spec.ClusterSpec = &redpandav1alpha2.RedpandaClusterSpec{}

				data, err := json.Marshal(values)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, cluster.Spec.ClusterSpec))

				kafkaClient, err := factory.KafkaClient(ctx, &cluster)
				require.NoError(t, err)
				metadata, err := kadm.NewClient(kafkaClient).BrokerMetadata(ctx)
				require.NoError(t, err)
				require.Len(t, metadata.Brokers.NodeIDs(), 1)
			})

			t.Run("KafkaAPISpec", func(t *testing.T) {
				var spec redpandav1alpha2.KafkaAPISpec
				spec.Brokers = []string{fmt.Sprintf("%s-0.%s.%s.svc.cluster.local:9093", name, name, name)}
				if tt.Auth != nil {
					require.NoError(t, kubeClient.Create(ctx, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret",
							Namespace: name,
						},
						StringData: map[string]string{
							"password": tt.Auth.Password,
						},
					}))

					spec.SASL = &redpandav1alpha2.KafkaSASL{
						Username: tt.Auth.Name,
						Password: redpandav1alpha2.SecretKeyRef{
							Name: "secret",
							Key:  "password",
						},
						Mechanism: redpandav1alpha2.SASLMechanism(tt.Auth.Mechanism),
					}
				}
				if tt.TLS {
					spec.TLS = &redpandav1alpha2.CommonTLS{
						CaCert: &redpandav1alpha2.SecretKeyRef{
							Name: fmt.Sprintf("%s-default-root-certificate", name),
							Key:  corev1.TLSCertKey,
						},
					}
				}
				kafkaClient, err := factory.KafkaClient(ctx, wrapSpec(name, &spec))
				require.NoError(t, err)
				metadata, err := kadm.NewClient(kafkaClient).BrokerMetadata(ctx)
				require.NoError(t, err)
				require.Len(t, metadata.Brokers.NodeIDs(), 1)
			})
		})
	}
}
