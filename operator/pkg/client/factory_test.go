// Copyright 2025 Redpanda Data, Inc.
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

	cmapiv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

var chartVersion = ""

func init() {
	log.SetLogger(logr.Discard())
}

func ensureMapAndSetValue(values map[string]any, key string, entries ...any) {
	if len(entries) == 1 {
		values[key] = entries[0]
		return
	}

	set := map[string]any{}
	if v, ok := values[key]; ok {
		set = v.(map[string]any)
	}

	ensureMapAndSetValue(set, entries[0].(string), entries[1:]...)

	values[key] = set
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
	cluster, err := k3d.NewCluster(t.Name(), k3d.WithAgents(1))
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

	kubeClient, err := client.New(restcfg, client.Options{Scheme: controller.UnifiedScheme, WarningHandler: client.WarningHandlerOptions{SuppressWarnings: true}})
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
				kafkaClient.Close()
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
				kafkaClient.Close()
			})
		})
	}
}

func TestClientFactoryTLSListeners(t *testing.T) {
	// Test of https://github.com/redpanda-data/helm-charts/blob/230a32adcee07184313f1c864bf9e3ab21a2e38e/charts/operator/files/three_node_redpanda.yaml

	if testing.Short() {
		t.Skip("skipping factory tests in short mode")
	}

	ctx := context.Background()
	cluster, err := k3d.NewCluster(t.Name(), k3d.WithAgents(1))
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

	kubeClient, err := client.New(restcfg, client.Options{Scheme: controller.UnifiedScheme, WarningHandler: client.WarningHandlerOptions{SuppressWarnings: true}})
	require.NoError(t, err)

	helmClient, err := helm.New(helm.Options{
		KubeConfig: restcfg,
	})
	require.NoError(t, err)
	require.NoError(t, helmClient.RepoAdd(ctx, "redpandadata", "https://charts.redpanda.com"))

	name := fmt.Sprintf("tls-test-%d", time.Now().Unix())

	err = kubeClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	require.NoError(t, err)

	err = kubeClient.Create(ctx, &cmapiv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kafka-internal-0",
			Namespace: name,
		},
		Spec: cmapiv1.CertificateSpec{
			EmailAddresses: []string{
				"test@domain.com",
			},
			Duration: ptr.To(metav1.Duration{Duration: 43800 * time.Hour}),
			IssuerRef: cmetav1.ObjectReference{
				Name:  "cluster-tls-kafka-internal-0-root-issuer",
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
			PrivateKey: &cmapiv1.CertificatePrivateKey{
				Algorithm: "ECDSA",
				Size:      256,
			},
			SecretName: "cluster-tls-user-client",
		},
	})
	require.NoError(t, err)

	factory := NewFactory(restcfg, kubeClient).WithDialer(kube.NewPodDialer(restcfg).DialContext)

	values := map[string]any{}
	ensureMapAndSetValue(values, "tls", map[string]any{
		"enabled": true,
		"certs": map[string]any{
			"kafka-internal-0": map[string]any{
				"caEnabled": true,
			},
		},
	})
	ensureMapAndSetValue(values, "listeners", "admin", map[string]any{
		"external": map[string]any{},
		"port":     9644,
		"tls": map[string]any{
			"cert":              "",
			"enabled":           false,
			"requireClientAuth": false,
		},
	})
	ensureMapAndSetValue(values, "listeners", "kafka", map[string]any{
		"authenticationMethod": "none",
		"external":             map[string]any{},
		"port":                 9092,
		"tls": map[string]any{
			"cert":              "kafka-internal-0",
			"enabled":           true,
			"requireClientAuth": false,
		},
	})

	// to reduce the bootup time of the cluster
	ensureMapAndSetValue(values, "statefulset", "replicas", 1)
	ensureMapAndSetValue(values, "console", "enabled", false)
	// to keep nodeport services from conflicting
	ensureMapAndSetValue(values, "external", "enabled", false)
	ensureMapAndSetValue(values, "image", "tag", "v24.2.2")

	var redpanda redpandav1alpha2.Redpanda
	redpanda.Name = name
	redpanda.Namespace = name
	redpanda.Spec.ClusterSpec = &redpandav1alpha2.RedpandaClusterSpec{}

	data, err := json.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, redpanda.Spec.ClusterSpec))

	_, err = helmClient.Install(ctx, "redpandadata/redpanda", helm.InstallOptions{
		Version:         chartVersion,
		CreateNamespace: true,
		Name:            name,
		Namespace:       name,
		Values:          values,
	})
	require.NoError(t, err)

	// check kafka connection
	kafkaClient, err := factory.KafkaClient(ctx, &redpanda)
	require.NoError(t, err)
	metadata, err := kadm.NewClient(kafkaClient).BrokerMetadata(ctx)
	require.NoError(t, err)
	require.Len(t, metadata.Brokers.NodeIDs(), 1)
	kafkaClient.Close()

	// check admin connection
	adminClient, err := factory.RedpandaAdminClient(ctx, &redpanda)
	require.NoError(t, err)
	defer adminClient.Close()

	brokers, err := adminClient.Brokers(ctx)
	require.NoError(t, err)
	require.Len(t, brokers, 1)
}
