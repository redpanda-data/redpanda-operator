// Copyright 2026 Redpanda Data, Inc.
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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

func TestShadowLinkClusterSettings_BootstrapRegression(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	server := &envtest.APIServer{}
	etcd := &envtest.Etcd{}

	testEnv := testutils.RedpandaTestEnv{
		Environment: envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: server,
				Etcd:      etcd,
			},
		},
	}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Cleanup(func() {
		_ = testEnv.Stop()
	})

	container, err := redpanda.Run(ctx, os.Getenv("TEST_REDPANDA_REPO")+":"+os.Getenv("TEST_REDPANDA_VERSION"),
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("superuser"),
		redpanda.WithNewServiceAccount("superuser", "password"),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	kafkaAddress, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)

	mgr := setupTestManager(t, ctx, cfg, c)

	factory := NewFactory(mgr, nil)

	settings, err := factory.RemoteClusterSettings(ctx, &redpandav1alpha2.ShadowLink{
		Spec: redpandav1alpha2.ShadowLinkSpec{
			SourceCluster: &redpandav1alpha2.ClusterSource{
				StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
					Kafka: &redpandav1alpha2.KafkaAPISpec{
						Brokers: []string{kafkaAddress},
					},
				},
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, settings.BootstrapServers, 1)
	require.Equal(t, kafkaAddress, settings.BootstrapServers[0])
}

func TestShadowLinkClusterSettings_SchemaRegistryCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Cleanup(func() {
		_ = testEnv.Stop()
	})

	c, err := client.New(cfg, client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)

	mgr := setupTestManager(t, ctx, cfg, c)
	factory := NewFactory(mgr, nil)

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sr-credentials",
			Namespace: metav1.NamespaceDefault,
		},
		StringData: map[string]string{
			"api-key":    "the-key",
			"api-secret": "the-secret",
		},
	}))

	mode := func(api *redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions) redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsMode {
		if api != nil {
			return redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsModeAPI
		}
		return redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsModeTopic
	}

	link := func(api *redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions) *redpandav1alpha2.ShadowLink {
		return &redpandav1alpha2.ShadowLink{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "link",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.ShadowLinkSpec{
				SourceCluster: &redpandav1alpha2.ClusterSource{
					StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
						Kafka: &redpandav1alpha2.KafkaAPISpec{
							Brokers: []string{"broker:9092"},
						},
					},
				},
				SchemaRegistrySyncOptions: &redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions{
					Mode:                    mode(api),
					ShadowSchemaRegistryAPI: api,
				},
			},
		}
	}

	secretRef := func(key string) redpandav1alpha2.ValueSource {
		return redpandav1alpha2.ValueSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "sr-credentials"},
				Key:                  key,
			},
		}
	}

	t.Run("resolves basic auth from a secret in the link's namespace", func(t *testing.T) {
		settings, err := factory.RemoteClusterSettings(ctx, link(&redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
			SourceURL: "https://registry.example.com",
			Authentication: &redpandav1alpha2.ShadowLinkSchemaRegistryAuthentication{
				Basic: &redpandav1alpha2.ShadowLinkSchemaRegistryBasicAuthentication{
					Username: secretRef("api-key"),
					Password: secretRef("api-secret"),
				},
			},
		}))
		require.NoError(t, err)
		require.NotNil(t, settings.SchemaRegistry)
		require.NotNil(t, settings.SchemaRegistry.BasicAuthentication)
		require.Equal(t, "the-key", settings.SchemaRegistry.BasicAuthentication.Username)
		require.Equal(t, "the-secret", settings.SchemaRegistry.BasicAuthentication.Password)
	})

	t.Run("no schema registry settings without api options", func(t *testing.T) {
		settings, err := factory.RemoteClusterSettings(ctx, link(nil))
		require.NoError(t, err)
		require.Nil(t, settings.SchemaRegistry)
	})

	t.Run("resolves against a V1 (vectorized) source cluster with the same logic", func(t *testing.T) {
		// Operator V1 mode (cloud/BYOC): the ShadowLink's source is a
		// vectorized Cluster. Kafka settings resolve from the V1 CR while
		// the schema registry API credentials resolve through the exact
		// same secret loading used for the static/V2 paths above.
		cluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "v1-source",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Image:    "docker.io/redpandadata/redpanda",
				Version:  "v24.3.5",
				Replicas: ptr.To(int32(1)),
				Configuration: vectorizedv1alpha1.RedpandaConfig{
					RPCServer:         vectorizedv1alpha1.SocketAddress{Port: 33145},
					KafkaAPI:          []vectorizedv1alpha1.KafkaAPI{{Port: 9092}},
					AdminAPI:          []vectorizedv1alpha1.AdminAPI{{Port: 9644}},
					SchemaRegistryAPI: []vectorizedv1alpha1.SchemaRegistryAPI{{Port: 8081}},
					DeveloperMode:     true,
				},
			},
		}
		require.NoError(t, c.Create(ctx, cluster))
		require.NoError(t, c.Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "v1-source-0",
				Namespace: metav1.NamespaceDefault,
				Labels:    labels.ForCluster(cluster),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}},
			},
		}))

		l := link(&redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
			SourceURL: "https://registry.example.com",
			Authentication: &redpandav1alpha2.ShadowLinkSchemaRegistryAuthentication{
				Basic: &redpandav1alpha2.ShadowLinkSchemaRegistryBasicAuthentication{
					Username: secretRef("api-key"),
					Password: secretRef("api-secret"),
				},
			},
		})
		l.Spec.SourceCluster = &redpandav1alpha2.ClusterSource{
			ClusterRef: &redpandav1alpha2.ClusterRef{
				Name:  "v1-source",
				Group: ptr.To("redpanda.vectorized.io"),
				Kind:  ptr.To("Cluster"),
			},
		}

		settings, err := factory.RemoteClusterSettings(ctx, l)
		require.NoError(t, err)

		// Kafka side came from the V1 cluster: one bootstrap URL per pod,
		// addressed through the headless service on the internal port.
		require.Len(t, settings.BootstrapServers, 1)
		require.True(t, strings.HasPrefix(settings.BootstrapServers[0], "v1-source-0.v1-source."), "unexpected bootstrap %q", settings.BootstrapServers[0])
		require.True(t, strings.HasSuffix(settings.BootstrapServers[0], ":9092"), "unexpected bootstrap %q", settings.BootstrapServers[0])
		require.Nil(t, settings.Authentication)
		require.Nil(t, settings.TLSSettings)

		// Schema registry side is the shared secret-resolution path.
		require.NotNil(t, settings.SchemaRegistry)
		require.NotNil(t, settings.SchemaRegistry.BasicAuthentication)
		require.Equal(t, "the-key", settings.SchemaRegistry.BasicAuthentication.Username)
		require.Equal(t, "the-secret", settings.SchemaRegistry.BasicAuthentication.Password)
	})

	t.Run("errors when the referenced secret does not exist", func(t *testing.T) {
		_, err := factory.RemoteClusterSettings(ctx, link(&redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
			SourceURL: "https://registry.example.com",
			Authentication: &redpandav1alpha2.ShadowLinkSchemaRegistryAuthentication{
				Basic: &redpandav1alpha2.ShadowLinkSchemaRegistryBasicAuthentication{
					Username: redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
							Key:                  "api-key",
						},
					},
					Password: secretRef("api-secret"),
				},
			},
		}))
		require.Error(t, err)
	})

	t.Run("skips resolution when schema replication is disabled", func(t *testing.T) {
		// A disabled schemaRegistrySyncOptions must not resolve (and must not
		// fail on) a stale/missing Secret reference on the API block —
		// otherwise a feature the user turned off would fail the whole link.
		disabledLink := link(&redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
			SourceURL: "https://registry.example.com",
			Authentication: &redpandav1alpha2.ShadowLinkSchemaRegistryAuthentication{
				Basic: &redpandav1alpha2.ShadowLinkSchemaRegistryBasicAuthentication{
					Username: redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
							Key:                  "api-key",
						},
					},
					Password: secretRef("api-secret"),
				},
			},
		})
		disabledLink.Spec.SchemaRegistrySyncOptions.Mode = redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsModeDisabled

		settings, err := factory.RemoteClusterSettings(ctx, disabledLink)
		require.NoError(t, err)
		require.Nil(t, settings.SchemaRegistry)
	})
}
