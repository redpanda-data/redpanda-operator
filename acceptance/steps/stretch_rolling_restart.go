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
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const sentinelTopicName = "sentinel-rolling"

type (
	upgradeStateKey      struct{}
	sentinelStateKey     struct{}
	kafkaFactoryStateKey struct{}
)

// kafkaFactoryState holds the reusable multicluster manager and factory so
// that multiple steps (sentinel create, sentinel verify) share one set of
// informer caches instead of creating competing managers.
type kafkaFactoryState struct {
	factory *internalclient.Factory
	nodes   []*vclusterNode
}

type upgradeState struct {
	initialPodUIDs map[string]types.UID
}

type sentinelState struct {
	clusterName string
	topicName   string
	messages    []string
}

// vclusterPodDialer returns a multicluster-aware DialContextFunc that resolves
// service names to pod names via the Endpoints API across all vclusters, then
// dials through the matching cluster's port-forwarded PodDialer.
func vclusterPodDialer(nodes []*vclusterNode, pfCfgs map[string]*rest.Config) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		// Parse "svc", "svc.ns", "svc.ns.svc.cluster.local", etc.
		parts := strings.SplitN(host, ".", 3)
		svcName := parts[0]
		ns := "default"
		if len(parts) >= 2 && parts[1] != "svc" {
			ns = parts[1]
		}

		for _, node := range nodes {
			pfCfg := pfCfgs[node.Name()]
			var ep corev1.Endpoints //nolint:staticcheck
			if err := node.Get(ctx, client.ObjectKey{Name: svcName, Namespace: ns}, &ep); err != nil {
				continue
			}
			for _, subset := range ep.Subsets {
				for _, addr := range subset.Addresses {
					if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
						podAddr := net.JoinHostPort(addr.TargetRef.Name+"."+ns, port)
						return kube.NewPodDialer(pfCfg).DialContext(ctx, network, podAddr)
					}
				}
			}
		}

		// Fallback: try each cluster's PodDialer directly.
		var lastErr error
		for _, node := range nodes {
			conn, err := kube.NewPodDialer(pfCfgs[node.Name()]).DialContext(ctx, network, address)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("multicluster dial %s failed: %w", address, lastErr)
	}
}

// getKafkaFactory returns the shared client Factory for the named stretch
// cluster. The factory must have been initialized earlier by initKafkaFactory
// (called from createSentinelTopicInStretchCluster).
func getKafkaFactory(ctx context.Context, t framework.TestingT, clusterName string) *kafkaFactoryState {
	if state, ok := ctx.Value(kafkaFactoryStateKey{}).(*kafkaFactoryState); ok {
		return state
	}
	t.Fatalf("no kafka factory in context for %q — was createSentinelTopicInStretchCluster called first?", clusterName)
	return nil
}

func initKafkaFactory(ctx context.Context, _ framework.TestingT, clusterName string) (context.Context, *kafkaFactoryState) {
	nodes := getNodes(ctx, clusterName)

	mgr, pfCfgs := setupMulticlusterManager(ctx, nodes)
	dialer := vclusterPodDialer(nodes, pfCfgs)
	factory := internalclient.NewFactory(mgr, nil).WithDialer(dialer)

	state := &kafkaFactoryState{factory: factory, nodes: nodes}
	return context.WithValue(ctx, kafkaFactoryStateKey{}, state), state
}

// stretchClusterKafkaClient builds a kgo.Client for the named stretch cluster
// using the operator's client Factory. TLS and broker discovery are handled
// by the factory via the StretchCluster spec; the multicluster-aware dialer
// routes connections through per-vcluster port-forwarded PodDialers.
func stretchClusterKafkaClient(ctx context.Context, t framework.TestingT, clusterName string, extraOpts ...kgo.Opt) *kgo.Client {
	state := getKafkaFactory(ctx, t, clusterName)

	// Fetch the StretchCluster from the first available node.
	var sc redpandav1alpha2.StretchCluster
	require.Eventually(t, func() bool {
		for _, node := range state.nodes {
			var list redpandav1alpha2.StretchClusterList
			if err := node.List(ctx, &list, client.InNamespace("default")); err != nil {
				continue
			}
			if len(list.Items) > 0 {
				sc = list.Items[0]
				return true
			}
		}
		return false
	}, 30*time.Second, 1*time.Second, "no StretchCluster found in vclusters for %q", clusterName)

	// Building the client lists RedpandaBrokerPools through the factory's
	// cached multicluster client. This call is the first use of a
	// freshly-built manager whose informers haven't started yet, so the
	// List starts the RedpandaBrokerPool informer and blocks on its initial
	// cache sync — which crosses the port-forwarded vcluster connections and
	// can exceed the factory's per-call RemoteCallTimeout on the first
	// attempt, surfacing as "finding representative broker pool: Timeout:
	// failed waiting for *v1alpha2.RedpandaBrokerPool Informer to sync".
	// That is transient: the informer keeps syncing in the background, so a
	// later attempt succeeds. Retry rather than failing the scenario on a
	// cold-cache timeout (the sentinel/consume steps that follow already
	// retry their own RPCs the same way).
	var cl *kgo.Client
	require.Eventually(t, func() bool {
		var err error
		cl, err = state.factory.KafkaClient(ctx, &sc, extraOpts...)
		if err != nil {
			t.Logf("creating stretch cluster kafka client for %q: %v", clusterName, err)
			return false
		}
		return true
	}, 2*time.Minute, 5*time.Second, "creating stretch cluster kafka client for %q", clusterName)
	return cl
}

func createSentinelTopicInStretchCluster(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	ctx, _ = initKafkaFactory(ctx, t, clusterName)

	cl := stretchClusterKafkaClient(ctx, t, clusterName)
	defer cl.Close()

	admin := kadm.NewClient(cl)

	// CreateTopics often fires before every broker is fully through TLS
	// startup — the BrokerPool `Deployed=True` condition only signals that
	// all StatefulSet replicas are ready, not that brokers accept Kafka
	// connections. First requests can fail with `EOF` mid-TLS-handshake
	// while the broker is still loading its config or certs. Retry the
	// whole RPC until the cluster is actually serving; tolerate
	// `TopicAlreadyExists` so an in-flight success that loses its
	// response on a tore-down connection doesn't trip the next attempt.
	require.Eventually(t, func() bool {
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		resp, err := admin.CreateTopics(attemptCtx, 3, 3, nil, sentinelTopicName)
		if err != nil {
			t.Logf("creating sentinel topic %q: %v", sentinelTopicName, err)
			return false
		}
		for _, r := range resp {
			if r.Err == nil || errors.Is(r.Err, kerr.TopicAlreadyExists) {
				continue
			}
			t.Logf("creating topic %q: %v", sentinelTopicName, r.Err)
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second, "creating sentinel topic %q", sentinelTopicName)
	t.Logf("created sentinel topic %q (3 partitions, 3 replicas)", sentinelTopicName)

	messages := []string{"sentinel-msg-1", "sentinel-msg-2", "sentinel-msg-3"}
	for _, msg := range messages {
		// franz-go's producer retries internally but only after the client
		// has discovered live brokers; right after CreateTopics the leader
		// election for the new partitions can still be pending. Wrap the
		// produce in Eventually so a transient leader-not-available or
		// connection error doesn't immediately fail the test.
		require.Eventually(t, func() bool {
			produceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			result := cl.ProduceSync(produceCtx, &kgo.Record{Topic: sentinelTopicName, Value: []byte(msg)})
			if err := result.FirstErr(); err != nil {
				t.Logf("producing sentinel message %q: %v", msg, err)
				return false
			}
			return true
		}, 2*time.Minute, 5*time.Second, "producing sentinel message %q", msg)
	}
	t.Logf("produced %d sentinel messages to %q", len(messages), sentinelTopicName)

	return context.WithValue(ctx, sentinelStateKey{}, &sentinelState{
		clusterName: clusterName,
		topicName:   sentinelTopicName,
		messages:    messages,
	})
}

func upgradeBrokerPoolsToImage(ctx context.Context, t framework.TestingT, clusterName, image string) context.Context {
	nodes := getNodes(ctx, clusterName)

	// Record current pod UIDs so we can detect when rolling restarts finish.
	initialPodUIDs := make(map[string]types.UID)
	for _, node := range nodes {
		var pods corev1.PodList
		require.NoError(t, node.List(ctx, &pods,
			client.InNamespace("default"),
			client.MatchingLabels{redpandaLabel: redpandaLabelValue},
		))
		for _, pod := range pods.Items {
			initialPodUIDs[pod.Name] = pod.UID
		}
	}
	t.Logf("recorded %d initial pod UIDs", len(initialPodUIDs))

	// Parse "repo:tag".
	repo, tag, _ := strings.Cut(image, ":")

	// Patch every NodePool with the new image.
	for _, node := range nodes {
		var pools redpandav1alpha2.RedpandaBrokerPoolList
		require.NoError(t, node.List(ctx, &pools, client.InNamespace("default")))

		for i := range pools.Items {
			pool := &pools.Items[i]
			poolKey := client.ObjectKeyFromObject(pool)

			require.Eventually(t, func() bool {
				var latest redpandav1alpha2.RedpandaBrokerPool
				if err := node.Get(ctx, poolKey, &latest); err != nil {
					t.Logf("error fetching NodePool %s in %s: %v", pool.Name, node.Name(), err)
					return false
				}
				latest.Spec.Image = &redpandav1alpha2.RedpandaImage{
					Repository: ptr.To(repo),
					Tag:        ptr.To(tag),
				}
				if err := node.Update(ctx, &latest); err != nil {
					t.Logf("conflict updating RedpandaBrokerPool %s in %s, retrying: %v", pool.Name, node.Name(), err)
					return false
				}
				return true
			}, 30*time.Second, 2*time.Second, "failed to update RedpandaBrokerPool %s in %s", pool.Name, node.Name())

			t.Logf("updated RedpandaBrokerPool %s in %s to image %s", pool.Name, node.Name(), image)
		}
	}

	return context.WithValue(ctx, upgradeStateKey{}, &upgradeState{
		initialPodUIDs: initialPodUIDs,
	})
}

// upgradeCompletesWithAtMostOneUnavailable polls pod readiness across all
// clusters during the rolling upgrade and asserts that at most 1 pod is
// unavailable at any polling interval. Blocks until all NodePools have
// Deployed=True and all pods have been replaced (new UIDs).
func upgradeCompletesWithAtMostOneUnavailable(ctx context.Context, t framework.TestingT, clusterName string) {
	nodes := getNodes(ctx, clusterName)

	state, _ := ctx.Value(upgradeStateKey{}).(*upgradeState)
	initialUIDs := map[string]types.UID{}
	if state != nil {
		initialUIDs = state.initialPodUIDs
	}

	totalPods := int32(len(nodes)) // 1 replica per cluster

	var maxUnavailable int32

	require.Eventually(t, func() bool {
		// Count ready pods across all clusters.
		readyCount := int32(0)
		for _, node := range nodes {
			var pods corev1.PodList
			if err := node.List(ctx, &pods,
				client.InNamespace("default"),
				client.MatchingLabels{redpandaLabel: redpandaLabelValue},
			); err != nil {
				t.Logf("error listing pods in %s: %v", node.Name(), err)
				return false
			}
			for _, pod := range pods.Items {
				if isPodRunning(&pod) {
					readyCount++
				}
			}
		}

		unavailable := totalPods - readyCount
		if unavailable < 0 {
			unavailable = 0
		}
		if unavailable > maxUnavailable {
			maxUnavailable = unavailable
			t.Logf("new max unavailable: %d (ready %d/%d)", maxUnavailable, readyCount, totalPods)
		}

		// Check completion: all RedpandaBrokerPools Deployed=True and all pods replaced.
		allDeployed := true
		allReplaced := len(initialUIDs) > 0 // only check if we have UIDs to compare
		for _, node := range nodes {
			var pools redpandav1alpha2.RedpandaBrokerPoolList
			if err := node.List(ctx, &pools, client.InNamespace("default")); err != nil {
				return false
			}
			for _, pool := range pools.Items {
				cond := apimeta.FindStatusCondition(pool.Status.Conditions, "Deployed")
				if cond == nil || cond.Status != metav1.ConditionTrue {
					allDeployed = false
				}
			}

			var pods corev1.PodList
			if err := node.List(ctx, &pods,
				client.InNamespace("default"),
				client.MatchingLabels{redpandaLabel: redpandaLabelValue},
			); err != nil {
				return false
			}
			for _, pod := range pods.Items {
				if oldUID, ok := initialUIDs[pod.Name]; ok && pod.UID == oldUID {
					allReplaced = false
					t.Logf("pod %s in %s has not been replaced yet", pod.Name, node.Name())
				}
			}
		}

		if allDeployed && allReplaced {
			t.Logf("upgrade complete: max unavailable was %d/%d", maxUnavailable, totalPods)
			return true
		}
		t.Logf("upgrade in progress: deployed=%v replaced=%v ready=%d/%d", allDeployed, allReplaced, readyCount, totalPods)
		return false
	}, 15*time.Minute, 2*time.Second, "rolling upgrade of %q did not complete", clusterName)

	require.LessOrEqual(t, maxUnavailable, int32(1),
		"expected at most 1 pod unavailable at a time during rolling upgrade, got max %d", maxUnavailable)
}

func sentinelDataIsReadable(ctx context.Context, t framework.TestingT, clusterName string) {
	state, ok := ctx.Value(sentinelStateKey{}).(*sentinelState)
	require.True(t, ok && state != nil, "no sentinel state in context; was createSentinelTopicInStretchCluster called?")

	cl := stretchClusterKafkaClient(ctx, t, clusterName,
		kgo.ConsumeTopics(state.topicName),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	defer cl.Close()

	found := make(map[string]bool)

	require.Eventually(t, func() bool {
		pollCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		fetches := cl.PollFetches(pollCtx)
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, fe := range errs {
				if fe.Err != context.DeadlineExceeded {
					t.Logf("fetch error topic=%s partition=%d: %v", fe.Topic, fe.Partition, fe.Err)
				}
			}
		}
		fetches.EachRecord(func(r *kgo.Record) {
			found[string(r.Value)] = true
		})

		t.Logf("found %d/%d sentinel messages", len(found), len(state.messages))
		for _, msg := range state.messages {
			if !found[msg] {
				return false
			}
		}
		return true
	}, 2*time.Minute, 500*time.Millisecond, "sentinel messages not all readable after upgrade")

	t.Logf("all %d sentinel messages readable after upgrade", len(state.messages))
}

func isPodRunning(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning
}
