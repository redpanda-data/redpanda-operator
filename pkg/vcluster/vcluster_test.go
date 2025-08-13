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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
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
