// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func iCreateABasicClusterWithNodes(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int) {
	key := t.ResourceKey(clusterName)
	image := &redpandav1alpha2.RedpandaImage{
		Tag:        ptr.To("dev"),
		Repository: ptr.To("localhost/redpanda-operator"),
	}

	require.NoError(t, t.Create(ctx, &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(nodeCount),
					SideCars: &redpandav1alpha2.SideCars{
						Image: image,
						Controllers: &redpandav1alpha2.RPControllers{
							Image: image,
						},
					},
				},
			},
		},
	}))

	t.Cleanup(func(ctx context.Context) {
		t := framework.T(ctx)

		t.Log("cleaning up Redpanda cluster")
		require.NoError(t, t.Delete(ctx, &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}))

		var cluster redpandav1alpha2.Redpanda
		require.Eventually(t, func() bool {
			// this can take some time
			deleted := false
			if err := t.Get(ctx, key, &cluster); err != nil && apierrors.IsNotFound(err) {
				deleted = true
			}

			t.Logf("checking that Redpanda cluster %q is fully deleted: %v", clusterName, deleted)

			return deleted
		}, 2*time.Minute, 5*time.Second, `Cluster %q still exists`, clusterName)
	})
}

func iScaleToNodes(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int) {
	var cluster redpandav1alpha2.Redpanda
	var err error

	key := t.ResourceKey(clusterName)

	require.Eventually(t, func() bool {
		// do this in a loop in case we are racing with the controller
		require.NoError(t, t.Get(ctx, key, &cluster))
		cluster.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(nodeCount)

		err = t.Update(ctx, &cluster)
		return err == nil
	}, 20*time.Second, 2*time.Second, `Cluster %q was unable to scale, last error: %v`, key.String(), err)
}

// theInPodRPKSeedListMatchesCurrentPods asserts the K8S-755 fix end-to-end on
// the v2 sidecar path: after a topology change, every broker pod's in-pod rpk
// configuration (/etc/redpanda/redpanda.yaml, kept fresh by the --watch-rpk-profile
// sidecar) must list exactly the current pods and nothing else — no entry left
// pointing at a removed broker whose DNS no longer resolves — and the refresh
// must happen without restarting any pod.
//
// Checking *every* pod (not just one) is deliberate: it is what distinguishes a
// per-pod watcher from a leader-elected one, which would converge only a single
// pod and leave the rest stale.
func theInPodRPKSeedListMatchesCurrentPods(ctx context.Context, t framework.TestingT, clusterName string) {
	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	var sts appsv1.StatefulSet
	require.NoError(t, t.Get(ctx, t.ResourceKey(clusterName), &sts))
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var pods corev1.PodList
		if !assert.NoError(collect, t.List(ctx, &pods, client.MatchingLabelsSelector{Selector: selector})) {
			return
		}
		if !assert.NotEmpty(collect, pods.Items, "no broker pods found for cluster %q", clusterName) {
			return
		}

		// The set of broker hostnames the in-pod rpk config should converge to
		// is exactly the current pod names (StatefulSet pod name == broker
		// hostname). Compare on that segment so the exact FQDN/port form does
		// not matter.
		want := make([]string, 0, len(pods.Items))
		for i := range pods.Items {
			want = append(want, pods.Items[i].Name)
		}
		sort.Strings(want)

		for i := range pods.Items {
			pod := &pods.Items[i]

			// No restart: the refresh must not have rolled any container.
			for _, cs := range pod.Status.ContainerStatuses {
				assert.Zerof(collect, cs.RestartCount,
					"container %q in pod %q restarted (%d) during the rpk refresh", cs.Name, pod.Name, cs.RestartCount)
			}

			var stdout bytes.Buffer
			if !assert.NoError(collect, ctl.Exec(ctx, pod, kube.ExecOptions{
				Container: "redpanda",
				Command:   []string{"cat", "/etc/redpanda/redpanda.yaml"},
				Stdout:    &stdout,
			})) {
				continue
			}

			got, err := rpkBrokerHostnames(stdout.Bytes())
			if !assert.NoErrorf(collect, err, "parsing rpk brokers from pod %q", pod.Name) {
				continue
			}
			assert.Equalf(collect, want, got,
				"pod %q in-pod rpk broker list has not converged to the current pods", pod.Name)
		}
	}, 5*time.Minute, 10*time.Second,
		"in-pod rpk broker list never converged to the current pods on every broker of %q", clusterName)
}

// rpkBrokerHostnames extracts the sorted, de-duplicated set of broker hostnames
// (the pod-name segment, port and domain stripped) from the rpk.kafka_api.brokers
// list of a rendered redpanda.yaml.
func rpkBrokerHostnames(redpandaYAML []byte) ([]string, error) {
	var cfg struct {
		RPK struct {
			KafkaAPI struct {
				Brokers []string `json:"brokers"`
			} `json:"kafka_api"`
		} `json:"rpk"`
	}
	if err := yaml.Unmarshal(redpandaYAML, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling redpanda.yaml: %w", err)
	}

	seen := map[string]struct{}{}
	var hosts []string
	for _, broker := range cfg.RPK.KafkaAPI.Brokers {
		host := broker
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx] // strip :port
		}
		host, _, _ = strings.Cut(host, ".") // pod-name segment of the FQDN
		if host == "" {
			continue
		}
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts, nil
}
