// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	vectorizedcontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	consolepkg "github.com/redpanda-data/redpanda-operator/operator/pkg/console"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	webhooks "github.com/redpanda-data/redpanda-operator/operator/webhooks/redpanda"
)

type mockKafkaAdmin struct{}

func (m *mockKafkaAdmin) CreateACLs(
	context.Context, *kadm.ACLBuilder,
) (kadm.CreateACLsResults, error) {
	return nil, nil
}

func (m *mockKafkaAdmin) DeleteACLs(
	context.Context, *kadm.ACLBuilder,
) (kadm.DeleteACLsResults, error) {
	return nil, nil
}

//nolint:funlen // Test using testEnv needs to be long
func TestDoNotValidateWhenDeleted(t *testing.T) {
	scheme := controller.UnifiedScheme

	testEnv := &testutils.RedpandaTestEnv{}

	cfg, err := testEnv.StartRedpandaTestEnv(true)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	defer testEnv.Stop() //nolint:errcheck // in test test env error is not relevant

	testAdminAPI := &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("testAdminAPI").WithName("mockAdminAPI")}
	testAdminAPIFactory := func(
		_ context.Context,
		_ client.Reader,
		_ *vectorizedv1alpha1.Cluster,
		_ string,
		_ types.AdminTLSConfigProvider,
		_ redpanda.DialContextFunc,
		pods ...string,
	) (adminutils.AdminAPIClient, error) {
		if len(pods) == 1 {
			return &adminutils.NodePoolScopedMockAdminAPI{
				MockAdminAPI: testAdminAPI,
				Pod:          pods[0],
			}, nil
		}
		return testAdminAPI, nil
	}

	webhookInstallOptions := &testEnv.WebhookInstallOptions
	webhookServer := webhook.NewServer(webhook.Options{
		Host:    webhookInstallOptions.LocalServingHost,
		Port:    webhookInstallOptions.LocalServingPort,
		CertDir: webhookInstallOptions.LocalServingCertDir,
	})
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme,
		WebhookServer:  webhookServer,
		LeaderElection: false,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	require.NoError(t, err)

	testStore := consolepkg.NewStore(mgr.GetClient(), mgr.GetScheme())
	testKafkaAdmin := &mockKafkaAdmin{}
	testKafkaAdminFactory := func(context.Context, client.Client, *vectorizedv1alpha1.Cluster, *consolepkg.Store) (consolepkg.KafkaAdminClient, error) {
		return testKafkaAdmin, nil
	}

	err = (&vectorizedv1alpha1.Cluster{}).SetupWebhookWithManager(mgr)
	require.NoError(t, err)
	hookServer := mgr.GetWebhookServer()

	err = (&vectorizedcontrollers.ConsoleReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		Log:                     ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Console"),
		AdminAPIClientFactory:   testAdminAPIFactory,
		Store:                   testStore,
		EventRecorder:           mgr.GetEventRecorderFor("Console"),
		KafkaAdminClientFactory: testKafkaAdminFactory,
	}).WithClusterDomain("").SetupWithManager(mgr)
	require.NoError(t, err)

	hookServer.Register("/mutate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
		Handler: &webhooks.ConsoleDefaulter{
			Client:  mgr.GetClient(),
			Decoder: admission.NewDecoder(mgr.GetScheme()),
		},
	})
	hookServer.Register("/validate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
		Handler: &webhooks.ConsoleValidator{
			Client:  mgr.GetClient(),
			Decoder: admission.NewDecoder(mgr.GetScheme()),
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err = mgr.Start(ctx)
		if err != nil {
			require.NoError(t, err)
		}
	}()

	for !mgr.GetCache().WaitForCacheSync(ctx) {
	}

	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // in test TLS verification is not necessary

	for err != nil {
		time.Sleep(1 * time.Second)
		conn, err = tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // in test TLS verification is not necessary
	}

	conn.Close()

	c, err := client.New(testEnv.Config, client.Options{Scheme: mgr.GetScheme()})
	require.NoError(t, err)

	one := int32(1)
	cluster := vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Port: 9091,
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{
					{
						Port: 8080,
					},
				},
			},
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{
					Name:     "test",
					Replicas: &one,
				},
			},
		},
	}

	console := vectorizedv1alpha1.Console{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "console",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ConsoleSpec{
			Server: vectorizedv1alpha1.Server{
				HTTPListenPort: 8080,
			},
			ClusterRef: vectorizedv1alpha1.NamespaceNameRef{
				Name:      "cluster",
				Namespace: "default",
			},
			Deployment: vectorizedv1alpha1.Deployment{
				Image: "vectorized/console:master-173596f",
			},
			Connect: vectorizedv1alpha1.Connect{Enabled: true},
			Cloud: &vectorizedv1alpha1.CloudConfig{
				PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
					Enabled: true,
					BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
						Username: "test",
						PasswordRef: vectorizedv1alpha1.SecretKeyRef{
							Name:      "prom-pass",
							Namespace: "default",
							Key:       "pass",
						},
					},
					Prometheus: &vectorizedv1alpha1.PrometheusConfig{
						Address: "test",
						Jobs: []vectorizedv1alpha1.PrometheusScraperJobConfig{
							{JobName: "test", KeepLabels: []string{"test"}},
						},
					},
				},
			},
		},
	}

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prom-pass",
			Namespace: "default",
		},
		StringData: map[string]string{
			"pass": "pass",
		},
	}

	err = c.Create(ctx, &secret)
	require.NoError(t, err)

	err = c.Create(ctx, &cluster)
	require.NoError(t, err)

	cluster.Status = vectorizedv1alpha1.ClusterStatus{
		Conditions: []vectorizedv1alpha1.ClusterCondition{
			{
				Type:               vectorizedv1alpha1.ClusterConfiguredConditionType,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
			},
		},
	}
	err = c.Status().Update(ctx, &cluster)
	require.NoError(t, err)

	err = c.Create(ctx, &console)
	require.NoError(t, err)

	err = c.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      "cluster",
	}, &cluster)
	require.NoError(t, err)

	err = c.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      "console",
	}, &console)
	require.NoError(t, err)

	for len(console.Finalizers) == 0 {
		time.Sleep(1 * time.Second)
		err = c.Get(ctx, client.ObjectKey{
			Namespace: "default",
			Name:      "console",
		}, &console)
		require.NoError(t, err)
	}

	err = c.Delete(ctx, &secret)
	require.NoError(t, err)

	err = c.Delete(ctx, &console)
	require.NoError(t, err)

	err = c.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      "console",
	}, &console)

	for !apierrors.IsNotFound(err) {
		time.Sleep(1 * time.Second)
		err = c.Get(ctx, client.ObjectKey{
			Namespace: "default",
			Name:      "console",
		}, &console)
	}
}

//nolint:funlen // this is table driven test
func TestValidatePrometheus(t *testing.T) {
	ctl := fake.NewClientBuilder().Build()
	consoleNamespace := "console"
	secretName := "secret"
	passwordKey := "password"
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: consoleNamespace,
		},
		Data: map[string][]byte{
			passwordKey: []byte("password"),
		},
	}
	require.NoError(t, ctl.Create(context.TODO(), &secret))

	tests := []struct {
		name                    string
		cloudConfig             vectorizedv1alpha1.CloudConfig
		expectedValidationError bool
	}{
		{"valid", vectorizedv1alpha1.CloudConfig{
			PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
				Enabled: true,
				BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
					Username: "username",
					PasswordRef: vectorizedv1alpha1.SecretKeyRef{
						Name:      secretName,
						Namespace: consoleNamespace,
						Key:       passwordKey,
					},
				},
				Prometheus: &vectorizedv1alpha1.PrometheusConfig{
					Address: "address",
					Jobs: []vectorizedv1alpha1.PrometheusScraperJobConfig{
						{
							JobName:    "job",
							KeepLabels: []string{"label"},
						},
					},
				},
			},
		}, false},
		{"missing job config", vectorizedv1alpha1.CloudConfig{
			PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
				Enabled: true,
				BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
					Username: "username",
					PasswordRef: vectorizedv1alpha1.SecretKeyRef{
						Name:      secretName,
						Namespace: consoleNamespace,
						Key:       passwordKey,
					},
				},
				Prometheus: &vectorizedv1alpha1.PrometheusConfig{
					Address: "address",
				},
			},
		}, true},
		{"missing basic auth password", vectorizedv1alpha1.CloudConfig{
			PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
				Enabled: true,
				BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
					Username: "username",
				},
				Prometheus: &vectorizedv1alpha1.PrometheusConfig{
					Address: "address",
					Jobs: []vectorizedv1alpha1.PrometheusScraperJobConfig{
						{
							JobName:    "job",
							KeepLabels: []string{"label"},
						},
					},
				},
			},
		}, true},
		{"nonexistent secret", vectorizedv1alpha1.CloudConfig{
			PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
				Enabled: true,
				BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
					Username: "username",
					PasswordRef: vectorizedv1alpha1.SecretKeyRef{
						Name:      "nonexisting",
						Namespace: consoleNamespace,
						Key:       passwordKey,
					},
				},
				Prometheus: &vectorizedv1alpha1.PrometheusConfig{
					Address: "address",
					Jobs: []vectorizedv1alpha1.PrometheusScraperJobConfig{
						{
							JobName:    "job",
							KeepLabels: []string{"label"},
						},
					},
				},
			},
		}, true},
		{"wrong basic auth secret key", vectorizedv1alpha1.CloudConfig{
			PrometheusEndpoint: &vectorizedv1alpha1.PrometheusEndpointConfig{
				Enabled: true,
				BasicAuth: vectorizedv1alpha1.BasicAuthConfig{
					Username: "username",
					PasswordRef: vectorizedv1alpha1.SecretKeyRef{
						Name:      secretName,
						Namespace: consoleNamespace,
						Key:       "nonexistingkey",
					},
				},
				Prometheus: &vectorizedv1alpha1.PrometheusConfig{
					Address: "address",
					Jobs: []vectorizedv1alpha1.PrometheusScraperJobConfig{
						{
							JobName:    "job",
							KeepLabels: []string{"label"},
						},
					},
				},
			},
		}, true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, err := webhooks.ValidatePrometheus(context.TODO(), ctl, &vectorizedv1alpha1.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "console",
					Namespace: consoleNamespace,
				},
				Spec: vectorizedv1alpha1.ConsoleSpec{
					Cloud: &tests[i].cloudConfig,
				},
			})
			require.NoError(t, err)
			if tt.expectedValidationError {
				require.NotEmpty(t, errs)
			} else {
				require.Empty(t, errs)
			}
		})
	}
}
