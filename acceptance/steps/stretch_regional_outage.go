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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// getNodeByName returns the vclusterNode whose logicalName matches regionName,
// failing the test if not found.  The logical name is the stable identifier
// used in feature files (e.g. "vc-2") and is independent of the unique host
// namespace name generated at test creation time.
func getNodeByName(ctx context.Context, t framework.TestingT, clusterName, regionName string) *vclusterNode {
	for _, node := range getNodes(ctx, clusterName) {
		if node.logicalName == regionName {
			return node
		}
	}
	t.Fatalf("region %q not found in cluster %q", regionName, clusterName)
	return nil
}

// takeRegionOffline simulates a regional outage by:
//  1. Scaling the vcluster StatefulSet (the region's control plane) to 0 replicas.
//  2. Deleting the Redpanda broker pod that was synced to the host cluster, so
//     the data plane also disappears immediately rather than waiting for the
//     vcluster controller to terminate it.
func takeRegionOffline(ctx context.Context, t framework.TestingT, regionName, clusterName string) context.Context {
	node := getNodeByName(ctx, t, clusterName, regionName)

	hostClient, err := client.New(t.RestConfig(), client.Options{})
	require.NoError(t, err)

	// The StatefulSet name and namespace both match the actual (unique) vcluster
	// name, not the logical region name used in feature files.
	actualName := node.Name()
	ssKey := types.NamespacedName{Name: actualName, Namespace: actualName}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var ss appsv1.StatefulSet
		if err := hostClient.Get(ctx, ssKey, &ss); err != nil {
			return err
		}
		ss.Spec.Replicas = ptr.To(int32(0))
		return hostClient.Update(ctx, &ss)
	})
	require.NoError(t, err, "scaling vcluster StatefulSet %s to 0", actualName)
	t.Logf("scaled vcluster StatefulSet %s to 0", actualName)

	// Wait for the StatefulSet to have no ready replicas.
	require.Eventually(t, func() bool {
		var ss appsv1.StatefulSet
		if err := hostClient.Get(ctx, ssKey, &ss); err != nil {
			return false
		}
		t.Logf("vcluster %s: readyReplicas=%d (waiting for 0)", actualName, ss.Status.ReadyReplicas)
		return ss.Status.ReadyReplicas == 0
	}, 2*time.Minute, 2*time.Second, "vcluster %s never reached 0 ready replicas", actualName)

	// Delete the pods that vcluster synced to the host cluster. In vcluster-pro,
	// pods inside the vcluster are synced to the host namespace with their labels
	// intact. We delete both the Redpanda broker pods and the operator pod so
	// that the remaining operators lose their gRPC connection to this region and
	// can correctly detect it as unreachable.
	for _, labelValue := range []string{redpandaLabelValue, "operator"} {
		var pods corev1.PodList
		require.NoError(t, hostClient.List(ctx, &pods,
			client.InNamespace(actualName),
			client.MatchingLabels{redpandaLabel: labelValue},
		))
		for i := range pods.Items {
			t.Logf("deleting synced pod %s (label %s=%s) in host namespace %s", pods.Items[i].Name, redpandaLabel, labelValue, regionName)
			// Ignore errors – the pod may already be terminating.
			_ = hostClient.Delete(ctx, &pods.Items[i])
		}
	}

	node.offline = true
	t.Logf("region %q is now offline", regionName)
	return ctx
}

// bringRegionOnline restores a region by scaling its vcluster StatefulSet back
// to 1 and waiting for the control plane pod to become ready.
func bringRegionOnline(ctx context.Context, t framework.TestingT, regionName, clusterName string) context.Context {
	node := getNodeByName(ctx, t, clusterName, regionName)

	hostClient, err := client.New(t.RestConfig(), client.Options{})
	require.NoError(t, err)

	actualName := node.Name()
	ssKey := types.NamespacedName{Name: actualName, Namespace: actualName}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var ss appsv1.StatefulSet
		if err := hostClient.Get(ctx, ssKey, &ss); err != nil {
			return err
		}
		ss.Spec.Replicas = ptr.To(int32(1))
		return hostClient.Update(ctx, &ss)
	})
	require.NoError(t, err, "scaling vcluster StatefulSet %s back to 1", actualName)
	t.Logf("scaled vcluster StatefulSet %s back to 1", actualName)

	require.Eventually(t, func() bool {
		var ss appsv1.StatefulSet
		if err := hostClient.Get(ctx, ssKey, &ss); err != nil {
			return false
		}
		t.Logf("vcluster %s: readyReplicas=%d (waiting for >=1)", actualName, ss.Status.ReadyReplicas)
		return ss.Status.ReadyReplicas >= 1
	}, 5*time.Minute, 5*time.Second, "vcluster %s never came back online", actualName)

	node.offline = false
	t.Logf("region %q is back online", regionName)
	return ctx
}

// remainingRegionsReportSpecSynced asserts that all online (non-offline) regions
// in the cluster eventually have a SpecSynced condition with the given reason.
func remainingRegionsReportSpecSynced(ctx context.Context, t framework.TestingT, clusterName, expectedReason string) {
	scKey := types.NamespacedName{Name: "cluster", Namespace: "default"}
	for _, node := range getNodes(ctx, clusterName) {
		if node.offline {
			continue
		}
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := node.Get(ctx, scKey, &sc); err != nil {
				t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, "SpecSynced")
			if cond == nil {
				t.Logf("SpecSynced not yet set on %s", node.Name())
				return false
			}
			t.Logf("SpecSynced on %s: reason=%s (want %s)", node.Name(), cond.Reason, expectedReason)
			return cond.Reason == expectedReason
		}, 5*time.Minute, 2*time.Second, "SpecSynced never became %q on %s", expectedReason, node.Name())
	}
}

// allRegionsReportSpecSynced asserts that every region (including any that
// returned from an outage) eventually has a SpecSynced condition with the
// given reason.
func allRegionsReportSpecSynced(ctx context.Context, t framework.TestingT, clusterName, expectedReason string) {
	scKey := types.NamespacedName{Name: "cluster", Namespace: "default"}
	for _, node := range getNodes(ctx, clusterName) {
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := node.Get(ctx, scKey, &sc); err != nil {
				t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, "SpecSynced")
			if cond == nil {
				t.Logf("SpecSynced not yet set on %s", node.Name())
				return false
			}
			t.Logf("SpecSynced on %s: reason=%s (want %s)", node.Name(), cond.Reason, expectedReason)
			return cond.Reason == expectedReason
		}, 10*time.Minute, 2*time.Second, "SpecSynced never became %q on %s", expectedReason, node.Name())
	}
}

// remainingRegionsReportBrokerUnavailable asserts that the broker belonging to
// the downed region shows as IS-ALIVE=false in the Admin API broker list, as
// seen from each surviving region.
func remainingRegionsReportBrokerUnavailable(ctx context.Context, t framework.TestingT, clusterName, regionName string) {
	poolName := nameMap[regionName]
	expectedHost := fmt.Sprintf("%s-%s-0.default", stretchClusterResourceName, poolName)

	for _, node := range getNodes(ctx, clusterName) {
		if node.offline {
			continue
		}
		require.Eventually(t, func() bool {
			out, err := execInNodeRedpandaPod(ctx, node, "rpk redpanda admin brokers list")
			if err != nil {
				t.Logf("error running broker list on %s: %v", node.Name(), err)
				return false
			}
			aliveness := parseBrokerAliveness(out)
			alive, present := aliveness[expectedHost]
			if !present {
				t.Logf("broker %s not yet in list on %s", expectedHost, node.Name())
				return false
			}
			t.Logf("broker %s IS-ALIVE=%v on %s", expectedHost, alive, node.Name())
			return !alive
		}, 5*time.Minute, 5*time.Second,
			"broker %q never reported as unavailable from %s", expectedHost, node.Name())
	}
}

// reachableRegionsReflectUpdatedSpec asserts that all online regions have the
// updated StretchCluster spec (specifically the log_segment_size_min property
// added during the outage test).
func reachableRegionsReflectUpdatedSpec(ctx context.Context, t framework.TestingT, clusterName string) {
	scKey := types.NamespacedName{Name: "cluster", Namespace: "default"}
	for _, node := range getNodes(ctx, clusterName) {
		if node.offline {
			continue
		}
		require.Eventually(t, func() bool {
			return stretchClusterHasOutageTestConfig(ctx, t, node, scKey)
		}, 5*time.Minute, 2*time.Second,
			"reachable region %s never reflected updated StretchCluster spec", node.Name())
	}
}

// regionReflectsUpdatedSpec asserts that the named region (typically the one
// that just returned from an outage) has received the spec change that was
// applied while it was offline.
func regionReflectsUpdatedSpec(ctx context.Context, t framework.TestingT, regionName, clusterName string) {
	node := getNodeByName(ctx, t, clusterName, regionName)
	scKey := types.NamespacedName{Name: "cluster", Namespace: "default"}
	require.Eventually(t, func() bool {
		return stretchClusterHasOutageTestConfig(ctx, t, node, scKey)
	}, 10*time.Minute, 2*time.Second,
		"region %s never received the spec update after returning from outage", regionName)
}

// operatorInRegionRecovering waits for the operator pod inside the named
// vcluster to be Running and for the StretchCluster to have an actively
// reconciling condition set by that operator.
func operatorInRegionRecovering(ctx context.Context, t framework.TestingT, regionName, clusterName string) {
	node := getNodeByName(ctx, t, clusterName, regionName)
	scKey := types.NamespacedName{Name: "cluster", Namespace: "default"}

	// First wait for the operator pod inside the vcluster to be Running.
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		if err := node.List(ctx, &pods, client.MatchingLabels{
			"app.kubernetes.io/name":     "operator",
			"app.kubernetes.io/instance": "redpanda",
		}); err != nil {
			t.Logf("error listing operator pods in %s: %v", regionName, err)
			return false
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				t.Logf("operator pod %s is Running in %s", pod.Name, regionName)
				return true
			}
			t.Logf("operator pod %s in %s: phase=%s", pod.Name, regionName, pod.Status.Phase)
		}
		return false
	}, 5*time.Minute, 5*time.Second, "operator pod never reached Running in region %s", regionName)

	// Then wait for the operator to have written a fresh reconcile condition —
	// indicated by SpecSynced having a non-empty reason on this region's object.
	require.Eventually(t, func() bool {
		var sc redpandav1alpha2.StretchCluster
		if err := node.Get(ctx, scKey, &sc); err != nil {
			t.Logf("error fetching StretchCluster from %s: %v", regionName, err)
			return false
		}
		cond := apimeta.FindStatusCondition(sc.Status.Conditions, "SpecSynced")
		if cond == nil || cond.Reason == "NotReconciled" {
			t.Logf("StretchCluster on %s has not yet been reconciled by its operator", regionName)
			return false
		}
		t.Logf("StretchCluster on %s reconciled: SpecSynced reason=%s", regionName, cond.Reason)
		return true
	}, 5*time.Minute, 2*time.Second,
		"operator in %s never reconciled the StretchCluster after restart", regionName)
}

// execInNodeRedpandaPod executes a shell command inside the Redpanda container
// of the first ready pod in the given vcluster node and returns stdout.
func execInNodeRedpandaPod(ctx context.Context, node *vclusterNode, command string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pfCfg, err := node.PortForwardedRESTConfig(execCtx)
	if err != nil {
		return "", fmt.Errorf("port-forwarded REST config for %s: %w", node.Name(), err)
	}
	ctl, err := kube.FromRESTConfig(pfCfg)
	if err != nil {
		return "", fmt.Errorf("kube ctl for %s: %w", node.Name(), err)
	}

	var stsList appsv1.StatefulSetList
	if err := node.List(execCtx, &stsList,
		client.InNamespace("default"),
		client.MatchingLabels{redpandaLabel: redpandaLabelValue},
	); err != nil {
		return "", fmt.Errorf("listing statefulsets in %s: %w", node.Name(), err)
	}
	if len(stsList.Items) == 0 {
		return "", fmt.Errorf("no redpanda StatefulSets in %s", node.Name())
	}

	sts := stsList.Items[0]
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err != nil {
		return "", err
	}

	var pods corev1.PodList
	if err := node.List(execCtx, &pods,
		client.InNamespace("default"),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return "", fmt.Errorf("listing pods for %s: %w", sts.Name, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods for StatefulSet %s in %s", sts.Name, node.Name())
	}

	var out bytes.Buffer
	if err := ctl.Exec(execCtx, &pods.Items[0], kube.ExecOptions{
		Container: "redpanda",
		Command:   []string{"/bin/bash", "-c", command},
		Stdout:    &out,
	}); err != nil {
		return "", fmt.Errorf("exec in %s: %w", node.Name(), err)
	}
	return out.String(), nil
}

// parseBrokerAliveness parses the tabular output of `rpk redpanda admin brokers list`
// and returns a map of HOST → isAlive.
//
// Example line (after header):
//
//	0  cluster-first-0.default  33145  -  1  active  true  25.2.1  8a0511ca-...
func parseBrokerAliveness(output string) map[string]bool {
	result := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return result
	}
	// Columns: ID HOST PORT RACK CORES MEMBERSHIP IS-ALIVE VERSION UUID
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		host := fields[1]
		result[host] = fields[6] == "true"
	}
	return result
}

// stretchClusterHasOutageTestConfig checks whether the StretchCluster on a
// given node has the log_segment_size_min property set — the sentinel config
// change applied during the regional outage test.
func stretchClusterHasOutageTestConfig(ctx context.Context, t framework.TestingT, node *vclusterNode, key types.NamespacedName) bool {
	var sc redpandav1alpha2.StretchCluster
	if err := node.Get(ctx, key, &sc); err != nil {
		t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
		return false
	}
	if sc.Spec.Config == nil || sc.Spec.Config.Cluster == nil {
		t.Logf("StretchCluster on %s has no config.cluster yet", node.Name())
		return false
	}
	var clusterCfg map[string]any
	if err := json.Unmarshal(sc.Spec.Config.Cluster.Raw, &clusterCfg); err != nil {
		t.Logf("error unmarshalling config.cluster on %s: %v", node.Name(), err)
		return false
	}
	val, ok := clusterCfg["log_segment_size_min"]
	t.Logf("StretchCluster on %s: log_segment_size_min=%v (present=%v)", node.Name(), val, ok)
	return ok
}
