// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vcluster_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

func TestIntegrationVCluster(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx := context.Background()

	host, err := k3d.GetShared()
	require.NoError(t, err)

	cluster, err := vcluster.New(ctx, host.RESTConfig())
	require.NoError(t, err)

	t.Cleanup(func() {
		if !testutil.Retain() {
			require.NoError(t, cluster.Delete())
		}
	})

	c, err := cluster.Client(client.Options{})
	require.NoError(t, err)

	var nodes corev1.NodeList
	assert.NoError(t, c.List(ctx, &nodes))
	assert.Len(t, nodes.Items, 4)

	require.NoError(t, c.Delete(ctx, &nodes.Items[2]))

	t.Run("pod dialer", func(t *testing.T) {
		// We deploy cert-manager automatically. Assert that it has a running Pod
		// as we'll be using it to test dialing.
		var pod *corev1.Pod
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var pods corev1.PodList
			require.NoError(t, c.List(ctx, &pods, client.MatchingLabels{
				"app.kubernetes.io/component": "controller",
				"app.kubernetes.io/name":      "cert-manager",
			}, client.MatchingFields{
				"status.phase": string(corev1.PodRunning),
			}))
			require.Len(t, pods.Items, 1)
			pod = &pods.Items[0]
		}, time.Minute, time.Second)

		// Assert that dialing into vCluster Pods works as expected by pulling
		// cert-manager's metrics endpoint.
		dialer := kube.NewPodDialer(cluster.RESTConfig())

		httpClient := http.Client{
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
			},
		}

		resp, err := httpClient.Get(fmt.Sprintf("http://%s.%s:9402/metrics", pod.Name, pod.Namespace))
		require.NoError(t, err)

		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("cert-manager metrics response:\n%s", out)
		require.Contains(t, string(out), "# HELP certmanager_clock_time_seconds_gauge")
	})

	t.Run("portfowarded config", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)

		cfg, err := cluster.PortForwardedRESTConfig(ctx)
		require.NoError(t, err)

		dir := t.TempDir()
		kubeconfig := filepath.Join(dir, "kubeconfig")
		require.NoError(t, kube.WriteToFile(kube.RestToConfig(cfg), kubeconfig))

		kubectl := func(args ...string) ([]byte, error) {
			cmd := exec.Command("kubectl", args...)
			cmd.Env = append(
				os.Environ(),
				"KUBECONFIG="+kubeconfig,
			)

			return cmd.CombinedOutput()
		}

		// -v provides useful logging output for discovering any errors. e.g.
		// HTTP -> HTTPS
		out, err := kubectl("get", "nodes", "-v=9")
		t.Logf("kubectl output:\n%s", out)
		require.NoError(t, err)

		cancel() // Kill the reverse proxy

		// Requests now fail.
		// NB: This takes ~5s as kubectl internally performs retries.
		_, err = kubectl("get", "nodes")
		require.Error(t, err)
	})
}

func TestValuesWithClusterDomain(t *testing.T) {
	for name, tc := range map[string]struct {
		values helm.RawYAML
	}{
		"empty values":              {values: nil},
		"values without networking": {values: helm.RawYAML(vcluster.DefaultValues)},
		"values with networking":    {values: helm.RawYAML("networking:\n  replicateServices:\n    toHost:\n      - from: a\n        to: b\n")},
		"values with the key set":   {values: helm.RawYAML("networking:\n  advanced:\n    clusterDomain: old.example\n")},
	} {
		t.Run(name, func(t *testing.T) {
			merged, err := vcluster.ValuesWithClusterDomain(tc.values, "k8s.example")
			require.NoError(t, err)

			// Duplicate top-level keys are invalid YAML, so a merge that
			// appended a second networking: block would fail to parse here.
			var doc map[string]any
			require.NoError(t, sigsyaml.UnmarshalStrict(merged, &doc))

			networking, ok := doc["networking"].(map[string]any)
			require.True(t, ok, "networking: %#v", doc["networking"])
			advanced, ok := networking["advanced"].(map[string]any)
			require.True(t, ok, "advanced: %#v", networking["advanced"])
			require.Equal(t, "k8s.example", advanced["clusterDomain"])

			// The round-trip must not drop or rewrite anything but the one
			// key the merge owns. Strip just that key from both sides rather
			// than the whole networking block, or the rows that bring their
			// own networking keys cannot fail here.
			before := map[string]any{}
			require.NoError(t, sigsyaml.Unmarshal(tc.values, &before))
			stripClusterDomain(before)
			stripClusterDomain(doc)
			require.Equal(t, before, doc)
		})
	}
}

// stripClusterDomain removes networking.advanced.clusterDomain, pruning the
// maps it empties so a document that only ever held that key compares equal to
// one that never had a networking block.
func stripClusterDomain(doc map[string]any) {
	networking, ok := doc["networking"].(map[string]any)
	if !ok {
		return
	}
	advanced, ok := networking["advanced"].(map[string]any)
	if !ok {
		return
	}

	delete(advanced, "clusterDomain")
	if len(advanced) == 0 {
		delete(networking, "advanced")
	}
	if len(networking) == 0 {
		delete(doc, "networking")
	}
}

func TestValuesWithClusterDomainRejectsMultipleDocuments(t *testing.T) {
	_, err := vcluster.ValuesWithClusterDomain(helm.RawYAML("sync:\n  fromHost:\n    nodes:\n      enabled: true\n---\ncontrolPlane:\n  distro:\n    k8s:\n      image:\n        tag: v1.36.1\n"), "k8s.example")
	require.ErrorContains(t, err, "single YAML document")
}

func TestDecodeManifest(t *testing.T) {
	manifest := `---
apiVersion: cluster.redpanda.com/v1alpha2
kind: StretchCluster
metadata:
  name: cluster
  namespace: default
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster
  namespace: default`

	type gvkNamed struct {
		gvk            string
		namespacedName string
	}

	expected := []gvkNamed{{
		gvk:            "cluster.redpanda.com/v1alpha2, Kind=StretchCluster",
		namespacedName: "default/cluster",
	}, {
		gvk:            "/v1, Kind=ConfigMap",
		namespacedName: "default/cluster",
	}}
	actual := []gvkNamed{}
	require.NoError(t, vcluster.DecodeManifest([]byte(manifest), func(decoded *unstructured.Unstructured) error {
		actual = append(actual, gvkNamed{
			gvk:            decoded.GroupVersionKind().String(),
			namespacedName: types.NamespacedName{Namespace: decoded.GetNamespace(), Name: decoded.GetName()}.String(),
		})
		return nil
	}))

	require.ElementsMatch(t, expected, actual)
}
