// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func TestDialer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := k3s.Run(ctx, "rancher/k3s:v1.27.1-k3s1", k3sOptions(t)...)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	config, err := container.GetKubeConfig(ctx)
	require.NoError(t, err)
	restcfg, err := clientcmd.RESTConfigFromKubeConfig(config)
	require.NoError(t, err)
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	client, err := client.New(restcfg, client.Options{Scheme: s})
	require.NoError(t, err)

	// k3s serves the API before the node is schedulable.
	require.Eventually(t, func() bool {
		var nodes corev1.NodeList
		if err := client.List(ctx, &nodes); err != nil || len(nodes.Items) == 0 {
			return false
		}
		return slices.ContainsFunc(nodes.Items[0].Status.Conditions, func(c corev1.NodeCondition) bool {
			return c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue
		})
	}, 2*time.Minute, 250*time.Millisecond)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "default",
			Labels: map[string]string{
				"service": "label",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: "caddy:2.10.2",
					Name:  "caddy",
					Command: []string{
						"caddy",
					},
					Args: []string{
						"file-server",
						"--domain",
						// use localhost so we don't reach out to an
						// ACME server
						"localhost",
					},
					Ports: []corev1.ContainerPort{{
						Name:          "http",
						ContainerPort: 80,
					}, {
						Name:          "https",
						ContainerPort: 443,
					}},
				},
			},
		},
	}
	err = client.Create(ctx, pod)
	require.NoError(t, err)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"service": "label",
			},
			Ports: []corev1.ServicePort{{
				Name: "http",
				Port: 8080,
			}},
		},
	}
	err = client.Create(ctx, service)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		var ready corev1.Pod
		err := client.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &ready)
		if err != nil {
			return false
		}

		return slices.ContainsFunc(ready.Status.Conditions, func(c corev1.PodCondition) bool {
			return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
		})
	}, 2*time.Minute, 10*time.Millisecond)

	dialer := NewPodDialer(restcfg)
	// Set the `ServerName` to match what Caddy generates for the localhost domain,
	// otherwise it fails due to an SNI mismatch.
	tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
			DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}

				// add in some deadline calls to make sure we don't panic/error

				if err := conn.SetReadDeadline(time.Now().Add(1 * time.Minute)); err != nil {
					return nil, err
				}

				if err := conn.SetWriteDeadline(time.Now().Add(1 * time.Minute)); err != nil {
					return nil, err
				}

				if err := conn.SetDeadline(time.Now().Add(1 * time.Minute)); err != nil {
					return nil, err
				}

				return conn, nil
			},
		},
	}

	for _, host := range []string{
		// http service-based DNS
		"http://name.service.default.svc.cluster.local",
		"http://name.service.default.svc",
		"http://name.service.default",
		// https pod-based DNS
		"http://name.default",
		"http://name",
		// https service-based DNS
		"https://name.service.default.svc.cluster.local",
		"https://name.service.default.svc",
		"https://name.service.default",
		// https pod-based DNS
		"https://name.default",
		"https://name",
		// trailing dots
		"http://name.service.default.svc.cluster.local.",
	} {
		t.Run(host, func(t *testing.T) {
			// Test the pooling behavior of HTTPClient by making requests to
			// the same hosts a few times.
			for i := 0; i < 5; i++ {
				func() {
					// Run in a closure so we have a context scoped to the life
					// time of each request we make, which is distinct from the
					// lifetime of the connection due to http.Transport's
					// connection pooling.
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()

					req, err := http.NewRequestWithContext(ctx, http.MethodGet, host, nil)
					require.NoError(t, err)

					resp, err := httpClient.Do(req)
					require.NoError(t, err)

					defer resp.Body.Close()

					_, err = io.ReadAll(resp.Body)
					require.NoError(t, err)

					require.Equal(t, http.StatusOK, resp.StatusCode)
				}()
			}
		})
	}

	t.Run("roundTripperFor", func(t *testing.T) {
		const canary = "You've used the customized dialer!"

		// Reuse the existing kube-apiserver so the initial request succeeds
		// and goes on to the SPDY upgrade process.
		cfg := rest.CopyConfig(restcfg)

		// Prior to our own implementation of roundTripperFor, our custom
		// dialer would only get called once. Though that's a bit difficult to
		// showcase with a test...
		var calls int32
		cfg.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			switch atomic.AddInt32(&calls, 1) {
			case 0:
				return net.Dial(network, address)
			case 1:
				return nil, errors.New(canary)
			}
			t.Fatalf("shouldn't get called more than twice")
			panic("unreachable")
		}

		dialer := NewPodDialer(cfg)

		_, err := dialer.DialContext(context.Background(), "tcp", "name:80")
		require.Error(t, err)
		require.Contains(t, err.Error(), canary)
	})
}

// k3sOptions adjusts the testcontainers k3s defaults for this test.
func k3sOptions(t *testing.T) []testcontainers.ContainerCustomizer {
	opts := []testcontainers.ContainerCustomizer{
		// The module runs k3s with cgroupns=host. Under that the kubelet tears
		// down and recreates pod sandboxes while port-forwards still target the
		// old one; k3d runs k3s in a private cgroup namespace, so do the same.
		testcontainers.WithHostConfigModifier(func(hc *dockercontainer.HostConfig) {
			hc.Privileged = true
			hc.Tmpfs = map[string]string{"/run": "", "/var/run": ""}
		}),
		// None of the bundled add-ons are needed; skipping them avoids pulling
		// their images.
		testcontainers.WithCmdArgs("--disable=coredns,servicelb,local-storage,metrics-server"),
	}

	// Authenticate containerd's Docker Hub pulls with the CI credentials, as
	// pkg/k3d does, so the pod image isn't subject to the anonymous rate limit.
	user, token := os.Getenv("DOCKERHUB_USER"), os.Getenv("DOCKERHUB_TOKEN")
	if user != "" && token != "" {
		auth := map[string]any{"auth": map[string]string{"username": user, "password": token}}
		registries, err := yaml.Marshal(map[string]any{
			"configs": map[string]any{"docker.io": auth, "registry-1.docker.io": auth},
		})
		require.NoError(t, err)
		opts = append(opts, testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            bytes.NewReader(registries),
			ContainerFilePath: "/etc/rancher/k3s/registries.yaml",
			FileMode:          0o600,
		}))
	}

	return opts
}
