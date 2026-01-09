// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/imdario/mergo"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zapio"
	"go.uber.org/zap/zaptest"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3/chart"
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/helm/helmtest"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/tlsgeneration"
	"github.com/redpanda-data/redpanda-operator/pkg/valuesutil"
)

// TestChartLock asserts that the dependencies reported in Chart.lock align
// with the dependencies and versions of the go version of the chart.
func TestChartLock(t *testing.T) {
	// TODO: Once all charts are split into individual modules, also assert
	// that the versions reported by runtime/debug.BuildInfo() align with
	// Chart.lock.
	chartLockBytes, err := fs.ReadFile(chart.ChartFiles, "Chart.lock")
	require.NoError(t, err)

	var lock helm.ChartLock
	require.NoError(t, yaml.Unmarshal(chartLockBytes, &lock))

	data, err := os.ReadFile("./go.mod")
	require.NoError(t, err)

	goMod, err := modfile.Parse("go.mod", data, func(path, version string) (string, error) {
		return version, nil
	})
	require.NoError(t, err)

	getRevision := func(req *modfile.Require) string {
		if module.IsPseudoVersion(req.Mod.Version) {
			rev, err := module.PseudoVersionRev(req.Mod.Version)
			require.NoError(t, err)

			// Go's pseudo versions are longer than those output by git
			// describe, which is used for helm versions. Truncate to git
			// describes length to make matching easier.
			return rev[:8]
		}
		// Remove the `v` from the go dependency e.g. v3.0.0 will become 3.0.0
		// That will match what the Chart.lock reports
		return req.Mod.Version[1:]
	}

	consoleModule := goMod.Require[slices.IndexFunc(goMod.Require, func(req *modfile.Require) bool {
		return req.Mod.Path == "github.com/redpanda-data/redpanda-operator/charts/console/v3"
	})]

	for _, dep := range lock.Dependencies {
		switch dep.Name {
		case "console", "console-unstable":
			require.Contains(t, dep.Version, getRevision(consoleModule), `Chart.lock and go.mod MUST specify the same version of the console chart.
If you updated go.mod, update Chart.yaml, and visa versa.
`)

		default:
			t.Errorf("unexpected dependency: %q", dep.Name)
		}
	}
}

func TestIntegrationChart(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	log := zaptest.NewLogger(t)
	w := &zapio.Writer{Log: log, Level: zapcore.InfoLevel}
	wErr := &zapio.Writer{Log: log, Level: zapcore.ErrorLevel}

	redpandaChart := "./chart"

	h := helmtest.Setup(t)

	t.Run("set-datadir-ownership", func(t *testing.T) {
		env := h.Namespaced(t)
		ctx := testutil.Context(t)

		// If the install succeeds, then the init container has worked as
		// expected.
		_ = env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values: minimalValues(&redpanda.PartialValues{
				Statefulset: &redpanda.PartialStatefulset{
					InitContainers: &struct {
						FSValidator *struct {
							Enabled    *bool   "json:\"enabled,omitempty\""
							ExpectedFS *string "json:\"expectedFS,omitempty\""
						} "json:\"fsValidator,omitempty\""
						SetDataDirOwnership *struct {
							Enabled *bool "json:\"enabled,omitempty\""
						} "json:\"setDataDirOwnership,omitempty\""
						Configurator *struct {
							AdditionalCLIArgs []string "json:\"additionalCLIArgs,omitempty\""
						} "json:\"configurator,omitempty\""
					}{
						SetDataDirOwnership: &struct {
							Enabled *bool "json:\"enabled,omitempty\""
						}{
							Enabled: ptr.To(true),
						},
					},
				},
			}),
		})
	})

	t.Run("rbac", func(t *testing.T) {
		env := h.Namespaced(t)
		ctx := testutil.Context(t)

		release := env.Install(ctx, redpandaChart, helm.InstallOptions{
			// Default + RackAwareness
			Values: minimalValues(&redpanda.PartialValues{
				RackAwareness: &redpanda.PartialRackAwareness{
					Enabled:        ptr.To(true),
					NodeAnnotation: ptr.To("k3s.io/hostname"),
				},
			}),
		})

		pods, err := kube.List[corev1.PodList](ctx, env.Ctl(), release.Namespace, client.MatchingLabels{
			"app.kubernetes.io/instance":  release.Name,
			"app.kubernetes.io/component": release.Chart + "-statefulset",
		})
		require.NoError(t, err)

		// Assert that all containers are ready, this ensures that the SideCar
		// container is working as expected.
		for _, pod := range pods.Items {
			for _, container := range pod.Status.ContainerStatuses {
				require.True(t, container.Ready, "Container %q of Pod %q should be ready", container.Name, pod.Name)
			}
		}

		// Assert that rpk debug bundle, which pings the k8s API, works with default values.
		{
			selector := fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=%s-statefulset", release.Name, release.Chart)

			var stdout bytes.Buffer
			require.NoError(t, env.Ctl().Exec(ctx, &pods.Items[0], kube.ExecOptions{
				Container: "redpanda",
				Command:   []string{"rpk", "debug", "bundle", "--namespace", release.Namespace, "--label-selector", selector},
				Stdout:    &stdout,
			}))

			t.Logf("rpk debug bundle output:\n%s\n", stdout.String())

			// Aside from asserting that RPK doesn't exit 1, we do some light
			// checks on the stdout to ensure we've not seen any issues
			// mentioning the inability to fetch Pods. We previously attempted
			// to assert that 2 expected errors were reported but different
			// errors happen on *nix vs macOS which makes such an assertion
			// unreliable.
			require.NotContains(t, stdout.String(), "no pods found in namespace")
			require.Contains(t, stdout.String(), "Debug bundle saved to")
		}

		// Assert that rack awareness, which pings the k8s API, works.
		{
			var stdout bytes.Buffer
			require.NoError(t, env.Ctl().Exec(ctx, &pods.Items[0], kube.ExecOptions{
				Container: "redpanda",
				Command:   []string{"cat", "/etc/redpanda/redpanda.yaml"},
				Stdout:    &stdout,
			}))

			// Not the best assert, we're just looking to check that the `rack` key
			// has been set to some value that starts with k3d- which indicates the
			// interaction with the k8s API worked as expected.
			require.Contains(t, stdout.String(), "rack: k3d-")
		}
	})

	t.Run("mtls-using-cert-manager", func(t *testing.T) {
		ctx := testutil.Context(t)

		env := h.Namespaced(t)

		partial := mTLSValuesUsingCertManager()

		rpRelease := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values: partial,
		})

		rpk := newClient(t, env.Ctl(), &rpRelease, partial)

		cleanup, err := rpk.ExposeRedpandaCluster(ctx, w, wErr)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		require.NoError(t, err)

		assert.NoErrorf(t, kafkaListenerTest(ctx, rpk), "Kafka listener sub test failed")
		assert.NoErrorf(t, adminListenerTest(ctx, rpk), "Admin listener sub test failed")
		schemaBytes, retrievedSchema, err := schemaRegistryListenerTest(ctx, rpk)
		if assert.NoErrorf(t, err, "Schema Registry listener sub test failed") {
			assert.JSONEq(t, string(schemaBytes), retrievedSchema)
		}
		assert.NoErrorf(t, httpProxyListenerTest(ctx, rpk), "HTTP Proxy listener sub test failed")
	})

	t.Run("mtls-using-self-created-certificates", func(t *testing.T) {
		ctx := testutil.Context(t)

		env := h.Namespaced(t)

		serverTLSSecretName := "server-tls-secret"
		clientTLSSecretName := "client-tls-secret"

		r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(1799999999))
		require.NoError(t, err)

		chartReleaseName := fmt.Sprintf("chart-%d", r.Int64())
		ca, sPublic, sPrivate, cPublic, cPrivate, err := tlsgeneration.ClientServerCertificate(chartReleaseName, env.Namespace())
		require.NoError(t, err)

		s := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverTLSSecretName,
				Namespace: env.Namespace(),
			},
			Data: map[string][]byte{
				"ca.crt":  ca,
				"tls.crt": sPublic,
				"tls.key": sPrivate,
			},
		}
		_, err = kube.Create(ctx, env.Ctl(), s)
		require.NoError(t, err)

		c := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clientTLSSecretName,
				Namespace: env.Namespace(),
			},
			Data: map[string][]byte{
				"ca.crt":  ca,
				"tls.crt": cPublic,
				"tls.key": cPrivate,
			},
		}
		_, err = kube.Create(ctx, env.Ctl(), c)
		require.NoError(t, err)

		partial := mTLSValuesWithProvidedCerts(serverTLSSecretName, clientTLSSecretName)

		rpRelease := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values:    partial,
			Name:      chartReleaseName,
			Namespace: env.Namespace(),
		})

		rpk := newClient(t, env.Ctl(), &rpRelease, partial)

		cleanup, err := rpk.ExposeRedpandaCluster(ctx, w, wErr)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		require.NoError(t, err)

		assert.NoErrorf(t, kafkaListenerTest(ctx, rpk), "Kafka listener sub test failed")
		assert.NoErrorf(t, adminListenerTest(ctx, rpk), "Admin listener sub test failed")
		schemaBytes, retrievedSchema, err := schemaRegistryListenerTest(ctx, rpk)
		assert.NoErrorf(t, err, "Schema Registry listener sub test failed")
		assert.JSONEq(t, string(schemaBytes), retrievedSchema)
		assert.NoErrorf(t, httpProxyListenerTest(ctx, rpk), "HTTP Proxy listener sub test failed")
	})

	t.Run("admin api auth required", func(t *testing.T) {
		ctx := testutil.Context(t)

		env := h.Namespaced(t)

		partial := minimalValues(&redpanda.PartialValues{
			External:      &redpanda.PartialExternalConfig{Enabled: ptr.To(false)},
			ClusterDomain: ptr.To("cluster.local"),
			Config: &redpanda.PartialConfig{
				Cluster: redpanda.PartialClusterConfig{
					"admin_api_require_auth": true,
				},
			},
			Auth: &redpanda.PartialAuth{
				SASL: &redpanda.PartialSASLAuth{
					Enabled: ptr.To(true),
					Users: []redpanda.PartialSASLUser{{
						Name:      ptr.To("superuser"),
						Password:  ptr.To("superpassword"),
						Mechanism: ptr.To[redpanda.SASLMechanism]("SCRAM-SHA-512"),
					}},
				},
			},
		})

		r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(1799999999))
		require.NoError(t, err)

		chartReleaseName := fmt.Sprintf("chart-%d", r.Int64())
		rpRelease := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values:    partial,
			Name:      chartReleaseName,
			Namespace: env.Namespace(),
		})

		rpk := newClient(t, env.Ctl(), &rpRelease, partial)

		cleanup, err := rpk.ExposeRedpandaCluster(ctx, w, wErr)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		require.NoError(t, err)

		assert.NoErrorf(t, kafkaListenerTest(ctx, rpk), "Kafka listener sub test failed")
		assert.NoErrorf(t, adminListenerTest(ctx, rpk), "Admin listener sub test failed")
		assert.NoErrorf(t, superuserTest(ctx, rpk, "superuser", "kubernetes-controller"), "Superuser sub test failed")
	})

	t.Run("admin api auth required - pre-existing secret", func(t *testing.T) {
		ctx := testutil.Context(t)

		env := h.Namespaced(t)

		err := env.Ctl().Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: env.Namespace(),
			},
			StringData: map[string]string{
				"users.txt": "superuser:superpassword:SCRAM-SHA-512",
			},
		})
		require.NoError(t, err)

		partial := minimalValues(&redpanda.PartialValues{
			External:      &redpanda.PartialExternalConfig{Enabled: ptr.To(false)},
			ClusterDomain: ptr.To("cluster.local"),
			Config: &redpanda.PartialConfig{
				Cluster: redpanda.PartialClusterConfig{
					"admin_api_require_auth": true,
				},
			},
			Auth: &redpanda.PartialAuth{
				SASL: &redpanda.PartialSASLAuth{
					Enabled:   ptr.To(true),
					SecretRef: ptr.To("my-secret"),
				},
			},
		})

		r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(1799999999))
		require.NoError(t, err)

		chartReleaseName := fmt.Sprintf("chart-%d", r.Int64())
		rpRelease := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values:    partial,
			Name:      chartReleaseName,
			Namespace: env.Namespace(),
		})

		rpk := newClient(t, env.Ctl(), &rpRelease, partial)

		cleanup, err := rpk.ExposeRedpandaCluster(ctx, w, wErr)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		require.NoError(t, err)

		assert.NoErrorf(t, kafkaListenerTest(ctx, rpk), "Kafka listener sub test failed")
		assert.NoErrorf(t, adminListenerTest(ctx, rpk), "Admin listener sub test failed")
		assert.NoErrorf(t, superuserTest(ctx, rpk, "superuser", "kubernetes-controller"), "Superuser sub test failed")
	})

	t.Run("sidecar", func(t *testing.T) {
		env := h.Namespaced(t)
		ctx := testutil.Context(t)

		release := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values: minimalValues(&redpanda.PartialValues{
				Statefulset: &redpanda.PartialStatefulset{
					SideCars: &redpanda.PartialSidecars{
						Args: []string{"--panic-after=5s"},
					},
				},
			}),
		})

		pods, err := kube.List[corev1.PodList](ctx, env.Ctl(), release.Namespace, client.MatchingLabels{
			"app.kubernetes.io/instance":  release.Name,
			"app.kubernetes.io/component": release.Chart + "-statefulset",
		})
		require.NoError(t, err)

		// Assert that all Pods have a sidecar instance that's panicking based
		// on their logs and that the panic has NOT triggered a restart of any
		// container. i.e. Sidecar crashes don't crash redpanda.
		for _, pod := range pods.Items {
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				logStream, err := env.Ctl().Logs(ctx, &pod, corev1.PodLogOptions{
					Container: "sidecar",
				})
				require.NoError(t, err)

				logs, err := io.ReadAll(logStream)
				require.NoError(t, err)

				require.Contains(t, string(logs), "unhandled panic triggered by --panic-after")
			}, 5*time.Minute, 5*time.Second)

			for _, container := range pod.Status.ContainerStatuses {
				require.Zero(t, container.RestartCount)
			}
		}
	})

	t.Run("console-integration", func(t *testing.T) {
		env := h.Namespaced(t)
		ctx := testutil.Context(t)

		release := env.Install(ctx, redpandaChart, helm.InstallOptions{
			Values: minimalValues(&redpanda.PartialValues{
				Console: &consolechart.PartialValues{
					Enabled: ptr.To(true),
				},
			}),
		})

		pods, err := kube.List[corev1.PodList](ctx, env.Ctl(), release.Namespace, client.MatchingLabels{
			"app.kubernetes.io/instance": release.Name,
			"app.kubernetes.io/name":     "console",
		})
		require.NoError(t, err)

		dialer := kube.NewPodDialer(env.Ctl().RestConfig())

		client := http.Client{
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
			},
		}

		consolePod := pods.Items[0]
		baseURL := fmt.Sprintf("http://%s.%s:8080", consolePod.Name, consolePod.Namespace)

		for _, check := range []struct {
			endpoint string
			check    func([]byte)
		}{
			{endpoint: "/api/schema-registry/mode"}, // Test that schema registry is connected.
			{endpoint: "/api/topics"},               // Test that Kafka is connected.
			{
				endpoint: "/api/console/endpoints", // Test that adminAPI is connected
				check: func(b []byte) {
					// DebugBundleService will only be supported if the admin API is configured.
					require.Contains(t, string(b), `{"endpoint":"redpanda.api.console.v1alpha1.DebugBundleService","method":"POST","isSupported":true}`)
				},
			},
		} {
			resp, err := client.Get(baseURL + check.endpoint)
			require.NoError(t, err)

			require.Equal(t, resp.StatusCode, 200)

			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			t.Logf("%s", body)

			if check.check != nil {
				check.check(body)
			}
		}
	})
}

func TieredStorageStatic(t *testing.T) redpanda.PartialValues {
	license := os.Getenv("REDPANDA_LICENSE")
	if license == "" {
		t.Skipf("$REDPANDA_LICENSE is not set")
	}

	return redpanda.PartialValues{
		Config: &redpanda.PartialConfig{
			Node: redpanda.PartialNodeConfig{
				"developer_mode": true,
			},
		},
		Enterprise: &redpanda.PartialEnterprise{
			License: &license,
		},
		Storage: &redpanda.PartialStorage{
			Tiered: &redpanda.PartialTiered{
				Config: redpanda.PartialTieredStorageConfig{
					"cloud_storage_enabled":    true,
					"cloud_storage_region":     "static-region",
					"cloud_storage_bucket":     "static-bucket",
					"cloud_storage_access_key": "static-access-key",
					"cloud_storage_secret_key": "static-secret-key",
				},
			},
		},
	}
}

func TieredStorageSecret(namespace string) corev1.Secret {
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "tiered-storage-",
			Namespace:    namespace,
		},
		Data: map[string][]byte{
			"access": []byte("from-secret-access-key"),
			"secret": []byte("from-secret-secret-key"),
		},
	}
}

func TieredStorageSecretRefs(t *testing.T, secret *corev1.Secret) redpanda.PartialValues {
	license := os.Getenv("REDPANDA_LICENSE")
	if license == "" {
		t.Skipf("$REDPANDA_LICENSE is not set")
	}

	access := "access"
	secretKey := "secret"
	return redpanda.PartialValues{
		Config: &redpanda.PartialConfig{
			Node: redpanda.PartialNodeConfig{
				"developer_mode": true,
			},
		},
		Enterprise: &redpanda.PartialEnterprise{
			License: &license,
		},
		Storage: &redpanda.PartialStorage{
			Tiered: &redpanda.PartialTiered{
				CredentialsSecretRef: &redpanda.PartialTieredStorageCredentials{
					AccessKey: &redpanda.PartialSecretRef{Name: &secret.Name, Key: &access},
					SecretKey: &redpanda.PartialSecretRef{Name: &secret.Name, Key: &secretKey},
				},
				Config: redpanda.PartialTieredStorageConfig{
					"cloud_storage_enabled": true,
					"cloud_storage_region":  "a-region",
					"cloud_storage_bucket":  "a-bucket",
				},
			},
		},
	}
}

func kafkaListenerTest(ctx context.Context, rpk *Client) error {
	input := "test-input"
	topicName := "testTopic"
	_, err := rpk.CreateTopic(ctx, topicName)
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = rpk.KafkaProduce(ctx, input, topicName)
	if err != nil {
		return errors.WithStack(err)
	}

	consumeOutput, err := rpk.KafkaConsume(ctx, topicName)
	if err != nil {
		return errors.WithStack(err)
	}

	if input != consumeOutput["value"] {
		return fmt.Errorf("expected value %s, got %s", input, consumeOutput["value"])
	}

	return nil
}

func adminListenerTest(ctx context.Context, rpk *Client) error {
	deadline := time.After(1 * time.Minute)
	for {
		select {
		case <-time.Tick(5 * time.Second):
			out, err := rpk.GetClusterHealth(ctx)
			if err != nil {
				continue
			}

			if out.IsHealthy {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("deadline exceeded")
		case <-ctx.Done():
			return fmt.Errorf("context deadline exceeded")
		}
	}
}

func superuserTest(ctx context.Context, rpk *Client, superusers ...string) error {
	deadline := time.After(1 * time.Minute)
	for {
		select {
		case <-time.Tick(5 * time.Second):
			configuredSuperusers, err := rpk.GetSuperusers(ctx)
			if err != nil {
				continue
			}

			if equalElements(configuredSuperusers, superusers) {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("deadline exceeded")
		case <-ctx.Done():
			return fmt.Errorf("context deadline exceeded")
		}
	}
}

func equalElements[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}

	counts := make(map[T]int)
	for _, val := range a {
		counts[val]++
	}

	for _, val := range b {
		if counts[val] == 0 {
			return false
		}
		counts[val]--
	}

	return true
}

func schemaRegistryListenerTest(ctx context.Context, rpk *Client) ([]byte, string, error) {
	// Test schema registry
	// Based on https://docs.redpanda.com/current/manage/schema-reg/schema-reg-api/
	formats, err := rpk.QuerySupportedFormats(ctx)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	// There is JSON, PROTOBUF and AVRO formats
	if len(formats) != 3 {
		return nil, "", fmt.Errorf("expected 2 supported formats, got %d", len(formats))
	}

	schema := map[string]any{
		"type": "record",
		"name": "sensor_sample",
		"fields": []map[string]any{
			{
				"name":        "timestamp",
				"type":        "long",
				"logicalType": "timestamp-millis",
			},
			{
				"name":        "identifier",
				"type":        "string",
				"logicalType": "uuid",
			},
			{
				"name": "value",
				"type": "long",
			},
		},
	}

	registered, err := rpk.RegisterSchema(ctx, schema)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	retrievedSchema, err := rpk.RetrieveSchema(ctx, registered.ID)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	resp, err := rpk.ListRegistrySubjects(ctx)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}
	if resp[0] != "sensor-value" {
		return nil, "", fmt.Errorf("expected sensor-value %d, got %q", registered.ID, resp[0])
	}

	if err := rpk.SoftDeleteSchema(ctx, resp[0], registered.ID); err != nil {
		return nil, "", errors.WithStack(err)
	}

	if err := rpk.HardDeleteSchema(ctx, resp[0], registered.ID); err != nil {
		return nil, "", errors.WithStack(err)
	}

	return []byte(registered.Schema.Schema), retrievedSchema.Schema, nil
}

type HTTPResponse []struct {
	Topic     string  `json:"topic"`
	Key       *string `json:"key"`
	Value     string  `json:"value"`
	Partition int     `json:"partition"`
	Offset    int     `json:"offset"`
}

func httpProxyListenerTest(ctx context.Context, rpk *Client) error {
	// Test http proxy
	// Based on https://docs.redpanda.com/current/develop/http-proxy/
	_, err := rpk.ListTopics(ctx)
	if err != nil {
		return errors.WithStack(err)
	}

	records := map[string]any{
		"records": []map[string]any{
			{
				"value":     "Redpanda",
				"partition": 0,
			},
			{
				"value":     "HTTP proxy",
				"partition": 1,
			},
			{
				"value":     "Test event",
				"partition": 2,
			},
		},
	}

	httpTestTopic := "httpTestTopic"
	_, err = rpk.CreateTopic(ctx, httpTestTopic)
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = rpk.SendEventToTopic(ctx, records, httpTestTopic)
	if err != nil {
		return errors.WithStack(err)
	}

	time.Sleep(time.Second * 5)

	record, err := rpk.RetrieveEventFromTopic(ctx, httpTestTopic, 0)
	if err != nil {
		return errors.WithStack(err)
	}

	expectedRecord := HTTPResponse{
		{
			Topic:     httpTestTopic,
			Key:       nil,
			Value:     "Redpanda",
			Partition: 0,
			Offset:    0,
		},
	}

	b, err := json.Marshal(&expectedRecord)
	if err != nil {
		return errors.WithStack(err)
	}

	if string(b) != record {
		return fmt.Errorf("expected record %s, got %s", string(b), record)
	}

	record, err = rpk.RetrieveEventFromTopic(ctx, httpTestTopic, 1)
	if err != nil {
		return errors.WithStack(err)
	}

	expectedRecord = HTTPResponse{
		{
			Topic:     httpTestTopic,
			Key:       nil,
			Value:     "HTTP proxy",
			Partition: 1,
			Offset:    0,
		},
	}

	b, err = json.Marshal(&expectedRecord)
	if err != nil {
		return errors.WithStack(err)
	}

	if string(b) != record {
		return fmt.Errorf("expected record %s, got %s", string(b), record)
	}

	record, err = rpk.RetrieveEventFromTopic(ctx, httpTestTopic, 2)
	if err != nil {
		return errors.WithStack(err)
	}

	expectedRecord = HTTPResponse{
		{
			Topic:     httpTestTopic,
			Key:       nil,
			Value:     "Test event",
			Partition: 2,
			Offset:    0,
		},
	}

	b, err = json.Marshal(&expectedRecord)
	if err != nil {
		return errors.WithStack(err)
	}

	if string(b) != record {
		return fmt.Errorf("expected record %s, got %s", string(b), record)
	}

	return nil
}

func mTLSValuesUsingCertManager() *redpanda.PartialValues {
	return minimalValues(&redpanda.PartialValues{
		TLS: &redpanda.PartialTLS{
			Certs: redpanda.PartialTLSCertMap{
				"kafka": redpanda.PartialTLSCert{
					Enabled:   ptr.To(true),
					CAEnabled: ptr.To(true),
				},
				"http": redpanda.PartialTLSCert{
					Enabled:   ptr.To(true),
					CAEnabled: ptr.To(true),
				},
				"rpc": redpanda.PartialTLSCert{
					Enabled:   ptr.To(true),
					CAEnabled: ptr.To(true),
				},
				"schema": redpanda.PartialTLSCert{
					Enabled:   ptr.To(true),
					CAEnabled: ptr.To(true),
				},
			},
		},
		External:      &redpanda.PartialExternalConfig{Enabled: ptr.To(false)},
		ClusterDomain: ptr.To("cluster.local"),
		Listeners: &redpanda.PartialListeners{
			Admin: &redpanda.PartialListenerConfig[redpanda.NoAuth]{
				TLS: &redpanda.PartialInternalTLS{
					// Uses default by default.
					RequireClientAuth: ptr.To(true),
				},
			},
			HTTP: &redpanda.PartialListenerConfig[redpanda.HTTPAuthenticationMethod]{
				TLS: &redpanda.PartialInternalTLS{
					Cert:              ptr.To("http"),
					RequireClientAuth: ptr.To(true),
				},
			},
			Kafka: &redpanda.PartialListenerConfig[redpanda.KafkaAuthenticationMethod]{
				TLS: &redpanda.PartialInternalTLS{
					Cert:              ptr.To("kafka"),
					RequireClientAuth: ptr.To(true),
				},
			},
			SchemaRegistry: &redpanda.PartialListenerConfig[redpanda.NoAuth]{
				TLS: &redpanda.PartialInternalTLS{
					Cert:              ptr.To("schema"),
					RequireClientAuth: ptr.To(true),
				},
			},
			RPC: &struct {
				Port *int32                       `json:"port,omitempty" jsonschema:"required"`
				TLS  *redpanda.PartialInternalTLS `json:"tls,omitempty" jsonschema:"required"`
			}{
				TLS: &redpanda.PartialInternalTLS{
					Cert:              ptr.To("rpc"),
					RequireClientAuth: ptr.To(true),
				},
			},
		},
	})
}

func mTLSValuesWithProvidedCerts(serverTLSSecretName, clientTLSSecretName string) *redpanda.PartialValues {
	return minimalValues(&redpanda.PartialValues{
		External:      &redpanda.PartialExternalConfig{Enabled: ptr.To(false)},
		ClusterDomain: ptr.To("cluster.local"),
		TLS: &redpanda.PartialTLS{
			Enabled: ptr.To(true),
			Certs: redpanda.PartialTLSCertMap{
				"provided": redpanda.PartialTLSCert{
					Enabled:         ptr.To(true),
					CAEnabled:       ptr.To(true),
					SecretRef:       &corev1.LocalObjectReference{Name: serverTLSSecretName},
					ClientSecretRef: &corev1.LocalObjectReference{Name: clientTLSSecretName},
				},
				"default": redpanda.PartialTLSCert{Enabled: ptr.To(false)},
			},
		},
		Listeners: &redpanda.PartialListeners{
			Admin: &redpanda.PartialListenerConfig[redpanda.NoAuth]{
				//External: redpanda.PartialExternalListeners[redpanda.PartialAdminExternal]{
				//	"default": redpanda.PartialAdminExternal{Enabled: ptr.To(false), Port: ptr.To(int32(0))},
				//},
				TLS: &redpanda.PartialInternalTLS{
					RequireClientAuth: ptr.To(true),
					Cert:              ptr.To("provided"),
				},
			},
			HTTP: &redpanda.PartialListenerConfig[redpanda.HTTPAuthenticationMethod]{
				//External: redpanda.PartialExternalListeners[redpanda.PartialHTTPExternal]{
				//	"default": redpanda.PartialHTTPExternal{Enabled: ptr.To(false), Port: ptr.To(int32(0))},
				//},
				TLS: &redpanda.PartialInternalTLS{
					RequireClientAuth: ptr.To(true),
					Cert:              ptr.To("provided"),
				},
			},
			Kafka: &redpanda.PartialListenerConfig[redpanda.KafkaAuthenticationMethod]{
				//External: redpanda.PartialExternalListeners[redpanda.PartialKafkaExternal]{
				//	"default": redpanda.PartialKafkaExternal{Enabled: ptr.To(false), Port: ptr.To(int32(0))},
				//},
				TLS: &redpanda.PartialInternalTLS{
					RequireClientAuth: ptr.To(true),
					Cert:              ptr.To("provided"),
				},
			},
			SchemaRegistry: &redpanda.PartialListenerConfig[redpanda.NoAuth]{
				//External: redpanda.PartialExternalListeners[redpanda.PartialSchemaRegistryExternal]{
				//	"default": redpanda.PartialSchemaRegistryExternal{Enabled: ptr.To(false), Port: ptr.To(int32(0))},
				//},
				TLS: &redpanda.PartialInternalTLS{
					RequireClientAuth: ptr.To(true),
					Cert:              ptr.To("provided"),
				},
			},
			RPC: &struct {
				Port *int32                       `json:"port,omitempty" jsonschema:"required"`
				TLS  *redpanda.PartialInternalTLS `json:"tls,omitempty" jsonschema:"required"`
			}{
				TLS: &redpanda.PartialInternalTLS{
					RequireClientAuth: ptr.To(true),
					Cert:              ptr.To("provided"),
				},
			},
		},
	})
}

func minimalValues(partials ...*redpanda.PartialValues) *redpanda.PartialValues {
	final := &redpanda.PartialValues{
		Console: &consolechart.PartialValues{
			Enabled: ptr.To(false),
		},
		External: &redpanda.PartialExternalConfig{
			Enabled: ptr.To(false),
		},
		Statefulset: &redpanda.PartialStatefulset{
			Replicas: ptr.To[int32](1),
			PodTemplate: &redpanda.PartialPodTemplate{
				Spec: applycorev1.PodSpec().WithTerminationGracePeriodSeconds(10),
			},
			SideCars: &redpanda.PartialSidecars{
				Image: &redpanda.PartialImage{
					Repository: ptr.To("localhost/redpanda-operator"),
					Tag:        ptr.To("dev"),
				},
			},
		},
	}

	for _, p := range partials {
		if err := mergo.Merge(final, p); err != nil {
			panic(err) // Should never happen (tm).
		}
	}

	return final
}

// getConfigMaps is parsing all manifests (resources) created by helm template
// execution. Redpanda helm chart creates 3 distinct files in ConfigMap:
// redpanda.yaml (node, tunable and cluster configuration), bootstrap.yaml
// (only cluster configuration) and profile (external connectivity rpk profile
// which is in different ConfigMap than other two).
func getConfigMaps(manifests []byte) (r *corev1.ConfigMap, rpk *corev1.ConfigMap, err error) {
	objs, err := kube.DecodeYAML(manifests, redpanda.Scheme)
	if err != nil {
		return nil, nil, err
	}

	for _, obj := range objs {
		switch obj := obj.(type) {
		case *corev1.ConfigMap:
			switch obj.Name {
			case "redpanda":
				r = obj
			case "redpanda-rpk":
				rpk = obj
			}
		}
	}

	return r, rpk, nil
}

func TestLabels(t *testing.T) {
	ctx := testutil.Context(t)
	client, err := helm.New(helm.Options{ConfigHome: testutil.TempDir(t)})
	require.NoError(t, err)

	for _, labels := range []map[string]string{
		{"foo": "bar"},
		{"baz": "1", "quux": "2"},
		// TODO: Add a test for asserting the behavior of adding a commonLabel
		// overriding a builtin value (app.kubernetes.io/name) once the
		// expected behavior is decided.
	} {
		values := &redpanda.PartialValues{
			CommonLabels: labels,
			// This guarantee does not currently extend to console.
			Console: &consolechart.PartialValues{Enabled: ptr.To(false)},
		}

		helmValues, err := redpanda.Chart.LoadValues(values)
		require.NoError(t, err)

		dot, err := redpanda.Chart.Dot(nil, helmette.Release{
			Name:      "redpanda",
			Namespace: "redpanda",
			Service:   "Helm",
		}, helmValues)
		require.NoError(t, err)

		state, err := redpanda.RenderStateFromDot(dot)
		require.NoError(t, err)

		manifests, err := client.Template(ctx, "./chart", helm.TemplateOptions{
			Name:      dot.Release.Name,
			Namespace: dot.Release.Namespace,
			// Nor does it extend to tests.
			SkipTests: true,
			Values:    values,
		})
		require.NoError(t, err)

		objs, err := kube.DecodeYAML(manifests, redpanda.Scheme)
		require.NoError(t, err)

		expectedLabels := redpanda.FullLabels(state)
		require.Subset(t, expectedLabels, values.CommonLabels, "FullLabels does not contain CommonLabels")

		for _, obj := range objs {
			// Assert that CommonLabels is included on all top level objects.
			require.Subset(t, obj.GetLabels(), expectedLabels, "%T %q", obj, obj.GetName())

			// For other objects (replication controllers) we want to assert
			// that common labels are also included on whatever object (Pod)
			// they generate/contain a template of.
			switch obj := obj.(type) {
			case *appsv1.StatefulSet:
				expectedLabels := maps.Clone(expectedLabels)
				expectedLabels["app.kubernetes.io/component"] += "-statefulset"
				require.Subset(t, obj.Spec.Template.GetLabels(), expectedLabels, "%T/%s's %T", obj, obj.Name, obj.Spec.Template)
			}
		}
	}
}

func TestAppVersion(t *testing.T) {
	const project = "charts/redpanda"
	output, err := exec.Command("changie", "latest", "-j", project).CombinedOutput()
	require.NoError(t, err)

	// Trim the project prefix to just get `x.y.z`
	expected := string(output[len(project+"/v"):])
	actual := redpanda.Chart.Metadata().Version

	require.Equalf(t, expected, actual, "Chart.yaml's version should be %q; got %q\nDid you forget to update Chart.yaml before minting a release?\nMake sure to bump appVersion as well!", expected, actual)
}

func TestControllersTag(t *testing.T) {
	chartBytes, err := os.ReadFile("../../operator/chart/Chart.yaml")
	require.NoError(t, err)

	valuesYAML, err := os.ReadFile("chart/values.yaml")
	require.NoError(t, err)

	var chart map[string]any
	require.NoError(t, yaml.Unmarshal(chartBytes, &chart))

	var values redpanda.Values
	require.NoError(t, yaml.Unmarshal(valuesYAML, &values))

	require.Equal(
		t,
		chart["appVersion"].(string),
		string(values.Statefulset.SideCars.Image.Tag),
		"the redpanda chart's values.yaml's sidecar tag should be equal to the operator chart's appVersion",
	)
}

func TestGoHelmEquivalence(t *testing.T) {
	tmp := testutil.TempDir(t)
	require.NoError(t, redpanda.Chart.Write(tmp))

	chartDir := filepath.Join(tmp, "redpanda")

	client, err := helm.New(helm.Options{ConfigHome: tmp})
	require.NoError(t, err)

	for _, tc := range CIGoldenTestCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			var values redpanda.PartialValues
			require.NoError(t, yaml.Unmarshal(tc.Data, &values), "input values are invalid YAML")

			// Make our values deterministic, otherwise the comparisons will be
			// thrown off.
			values.Tests = &struct {
				Enabled *bool "json:\"enabled,omitempty\""
			}{
				Enabled: ptr.To(false),
			}

			if values.Auth == nil {
				values.Auth = &redpanda.PartialAuth{}
			}
			if values.Auth.SASL == nil {
				values.Auth.SASL = &redpanda.PartialSASLAuth{}
			}
			if values.Auth.SASL.BootstrapUser == nil {
				values.Auth.SASL.BootstrapUser = &redpanda.PartialBootstrapUser{}
			}

			if values.Auth != nil && values.Auth.SASL != nil && values.Auth.SASL.BootstrapUser != nil {
				values.Auth.SASL.BootstrapUser.Password = ptr.To("bootstrapuser-p@ssw0rd")
			}

			values.Console = &consolechart.PartialValues{
				Enabled: ptr.To(true),
				Tests:   &consolechart.PartialEnableable{Enabled: ptr.To(false)},
				PartialRenderValues: console.PartialRenderValues{
					Ingress: &console.PartialIngressConfig{
						Enabled: ptr.To(true),
					},
					Secret: &console.PartialSecretConfig{
						Authentication: &console.PartialAuthenticationSecrets{
							JWTSigningKey: ptr.To("JWT_PLACEHOLDER"),
						},
					},
					// ServiceAccount and AutomountServiceAccountToken could be removed after Console helm chart release
					// Currently there is difference between dependency Console Deployment and ServiceAccount
					ServiceAccount: &console.PartialServiceAccountConfig{
						AutomountServiceAccountToken: ptr.To(false),
					},
					AutomountServiceAccountToken: ptr.To(false),
				},
			}

			goObjs, err := redpanda.Chart.Render(nil, helmette.Release{
				Name:      "gotohelm",
				Namespace: "mynamespace",
				Service:   "Helm",
			}, values)
			require.NoError(t, err)

			rendered, err := client.Template(context.Background(), chartDir, helm.TemplateOptions{
				Name:      "gotohelm",
				Namespace: "mynamespace",
				Values:    values,
			})
			require.NoError(t, err)

			helmObjs, err := kube.DecodeYAML(rendered, redpanda.Scheme)
			require.NoError(t, err)

			for _, obj := range append(helmObjs, goObjs...) {
				switch obj := obj.(type) {
				case *appsv1.StatefulSet:
					// resource.Quantity is a special object. To Ensure they compare correctly,
					// we'll round trip it through JSON so the internal representations will
					// match (assuming the values are actually equal).
					obj.Spec.Template.Spec.Containers[0].Resources, err = valuesutil.UnmarshalInto[corev1.ResourceRequirements](obj.Spec.Template.Spec.Containers[0].Resources)
					require.NoError(t, err)
				}
			}

			// This could be two sorts followed by an assert.Equal or
			// assert.EqualElements but either of those give subpar error
			// messages when dealing with the volume of helm charts. To
			// generate a more digestible error message, we "bucket" objects by
			// type and then name.

			helmObjsMap := map[reflect.Type]map[string]kube.Object{}
			for _, obj := range helmObjs {
				t := reflect.TypeOf(obj)
				if _, ok := helmObjsMap[t]; !ok {
					helmObjsMap[t] = map[string]kube.Object{}
				}
				helmObjsMap[t][obj.GetName()] = obj
			}

			goObjsMap := map[reflect.Type]map[string]kube.Object{}
			for _, obj := range goObjs {
				t := reflect.TypeOf(obj)
				if _, ok := goObjsMap[t]; !ok {
					goObjsMap[t] = map[string]kube.Object{}
				}
				goObjsMap[t][obj.GetName()] = obj
			}

			for typ := range helmObjsMap {
				assert.Equal(t, helmObjsMap[typ], goObjsMap[typ], "expected = helm\nactual = go")
			}
		})
	}
}

// TestMultiNamespaceInstall verifies that:
// - Multiple instances of the redpanda chart with different names may be installed in the same instance.
// - Multiple instances of the redpanda chart with the same name may be installed in different namespaces.
func TestMultiNamespaceInstall(t *testing.T) {
	ctl := kubetest.NewEnv(t)
	client, err := helm.New(helm.Options{
		KubeConfig: ctl.RestConfig(),
	})
	require.NoError(t, err)

	values := redpanda.PartialValues{
		// Disable NodePort services to avoid port conflicts.
		External: &redpanda.PartialExternalConfig{
			Type: ptr.To(corev1.ServiceTypeLoadBalancer),
		},
		// Disable cert-manager integration so we don't have to
		// install their CRDs.
		TLS: &redpanda.PartialTLS{
			Certs: redpanda.PartialTLSCertMap{
				"default": redpanda.PartialTLSCert{
					SecretRef: &corev1.LocalObjectReference{
						Name: "default",
					},
				},
				"external": redpanda.PartialTLSCert{
					SecretRef: &corev1.LocalObjectReference{
						Name: "default",
					},
				},
			},
		},
	}

	for i := 0; i < 2; i++ {
		namespace := fmt.Sprintf("namespace-%d", i)

		require.NoError(t, ctl.Apply(t.Context(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}))

		for j := 0; j < 2; j++ {
			_, err := client.Install(t.Context(), "./chart", helm.InstallOptions{
				Name:      fmt.Sprintf("redpanda-%d", j),
				Namespace: namespace,
				// Disable all forms of waits / checks. This isn't an actual
				// cluster, were just verifying that the API server and helm
				// don't report naming conflicts.
				NoHooks:       true,
				NoWait:        true,
				NoWaitForJobs: true,
				Timeout:       ptr.To(5 * time.Second),
				Values:        values,
			})
			require.NoError(t, err)
		}

		// One final check to show that conflicting names will result in an error.
		_, err := client.Install(t.Context(), "./chart", helm.InstallOptions{
			Name:      "redpanda-0",
			Namespace: namespace,
			// Disable all forms of waits / checks. This isn't an actual
			// cluster, were just verifying that the API server and helm
			// don't report naming conflicts.
			NoHooks:       true,
			NoWait:        true,
			NoWaitForJobs: true,
			Timeout:       ptr.To(5 * time.Second),
			Values:        values,
		})
		require.Error(t, err)
	}
}
