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
	"strconv"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

// takeNonControllerRegionOffline picks a region whose Redpanda broker is NOT
// the current controller, then simulates a regional outage by deleting the
// k3d agent hosting that region. Keeping the controller alive keeps the
// partition balancer running steadily so the auto-decommission decision
// isn't delayed by a controller failover.
//
// Unlike takeRegionOffline (which scales the vcluster control plane to 0
// and deletes synced pods), this fully removes the host k3d node: kubelet,
// networking, and all pods pinned to it go away atomically — more faithful
// to a real regional outage and necessary for Redpanda's auto-decommission
// logic to reliably trigger in our vcluster-on-k3d setup.
func takeNonControllerRegionOffline(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	nodes := getNodes(ctx, clusterName)
	require.NotEmpty(t, nodes, "no vcluster nodes found")

	controllerRegion := discoverControllerRegionName(ctx, t, nodes[0])
	t.Logf("controller is in region %q, picking a non-controller region to take offline", controllerRegion)

	var target *vclusterNode
	for _, n := range nodes {
		if n.logicalName != controllerRegion {
			target = n
			break
		}
	}
	require.NotNil(t, target, "no non-controller region found")

	deleteK3dNodeForRegion(ctx, t, target)
	return ctx
}

// deleteK3dNodeForRegion simulates a regional outage by deleting the host
// worker node that the vcluster's workloads are pinned to. Uses the harpoon
// framework's provider-agnostic ShutdownNode helper, which also registers a
// t.Cleanup that re-adds a node with the same name and reloads all imported
// images onto it — so subsequent scenarios in the same test run see the
// cluster at its original size without ImagePullBackOff.
func deleteK3dNodeForRegion(ctx context.Context, t framework.TestingT, node *vclusterNode) {
	require.NotEmpty(t, node.k3dNodeName,
		"region %s is not pinned to a k3d node — pinningValues may not have been applied", node.logicalName)

	t.Logf("simulating regional outage for region %s by shutting down host node %q",
		node.logicalName, node.k3dNodeName)

	// ShutdownNode: delete via provider, re-add with the same name + reload
	// imported images on cleanup.
	t.ShutdownNode(ctx, node.k3dNodeName)

	// Wait for the Kubernetes Node to be gone or marked NotReady. The
	// provider only removes the underlying runtime container; the Node
	// object may linger until kube-controller-manager notices the missing
	// kubelet.
	hostClient, err := client.New(t.RestConfig(), client.Options{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var n corev1.Node
		err := hostClient.Get(ctx, client.ObjectKey{Name: node.k3dNodeName}, &n)
		if err != nil {
			t.Logf("node %s gone from kubernetes: %v", node.k3dNodeName, err)
			return true
		}
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
				t.Logf("node %s is %s (status=%s)", node.k3dNodeName, cond.Type, cond.Status)
				return true
			}
		}
		t.Logf("node %s still Ready", node.k3dNodeName)
		return false
	}, 3*time.Minute, 5*time.Second, "node %s did not become NotReady", node.k3dNodeName)

	// Mark the region offline so other helpers (ApplyAll, diagnostics,
	// expectEventualNodeCountInRemainingClusters) skip it.
	node.offline = true

	t.Logf("regional outage simulated: node %q is down, region %s is offline",
		node.k3dNodeName, node.logicalName)
}

// discoverControllerRegionName runs `rpk cluster health` and `rpk redpanda
// admin brokers list` on the provided region's Redpanda pod to determine which
// region hosts the current controller broker.
func discoverControllerRegionName(ctx context.Context, t framework.TestingT, node *vclusterNode) string {
	pfCfg, err := node.PortForwardedRESTConfig(ctx)
	require.NoError(t, err, "creating port-forwarded config for %s", node.Name())
	ctl, err := kube.FromRESTConfig(pfCfg)
	require.NoError(t, err)

	pod := findRedpandaPod(ctx, t, node)
	require.NotNil(t, pod, "no redpanda pod found in %s", node.Name())

	execRpk := func(command string) string {
		var out bytes.Buffer
		err := ctl.Exec(ctx, pod, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"/bin/bash", "-c", command},
			Stdout:    &out,
		})
		require.NoError(t, err, "executing %q in %s: %s", command, node.Name(), out.String())
		return out.String()
	}

	healthOut := execRpk("rpk cluster health")
	controllerID := parseControllerID(healthOut)
	require.GreaterOrEqual(t, controllerID, 0, "failed to parse Controller ID:\n%s", healthOut)
	t.Logf("controller broker id: %d", controllerID)

	brokersOut := execRpk("rpk redpanda admin brokers list")
	controllerHost := parseBrokerHost(brokersOut, controllerID)
	require.NotEmpty(t, controllerHost, "failed to find host for controller id %d:\n%s", controllerID, brokersOut)
	t.Logf("controller broker host: %s", controllerHost)

	// HOST column looks like "<stretchcluster>-<nodepool>-0.default" — look
	// for any nodepool name from nameMap as a substring.
	for vc, nodepoolName := range nameMap {
		if strings.Contains(controllerHost, "-"+nodepoolName+"-") || strings.HasPrefix(controllerHost, nodepoolName+"-") {
			return vc
		}
	}
	require.FailNowf(t, "unknown host", "could not map host %q to a region (known nodepools: %v)", controllerHost, nameMap)
	return ""
}

func findRedpandaPod(ctx context.Context, _ framework.TestingT, node *vclusterNode) *corev1.Pod {
	var stsList appsv1.StatefulSetList
	if err := node.List(ctx, &stsList, client.InNamespace("default"), client.MatchingLabels{redpandaLabel: redpandaLabelValue}); err != nil {
		return nil
	}
	if len(stsList.Items) == 0 {
		return nil
	}
	selector, err := metav1.LabelSelectorAsSelector(stsList.Items[0].Spec.Selector)
	if err != nil {
		return nil
	}
	var pods corev1.PodList
	if err := node.List(ctx, &pods, client.InNamespace("default"), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil
	}
	if len(pods.Items) == 0 {
		return nil
	}
	return &pods.Items[0]
}

// parseControllerID finds "Controller ID: N" in rpk cluster health output.
func parseControllerID(output string) int {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Controller ID:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return -1
		}
		id, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			return -1
		}
		return id
	}
	return -1
}

// parseBrokerHost looks up the HOST column for the given broker ID in
// `rpk redpanda admin brokers list` output.
func parseBrokerHost(output string, brokerID int) string {
	idStr := strconv.Itoa(brokerID)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == idStr {
			return fields[1]
		}
	}
	return ""
}

// parseHealthNodeIDs parses the "All nodes" line from `rpk cluster health`
// output and returns the list of node IDs.
//
// Example line:
//
//	All nodes:                        [0 1 2]
func parseHealthNodeIDs(output string) ([]int, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "All nodes:") {
			continue
		}
		start := strings.Index(line, "[")
		end := strings.Index(line, "]")
		if start < 0 || end < 0 || end <= start+1 {
			return nil, nil
		}
		fields := strings.Fields(line[start+1 : end])
		ids := make([]int, 0, len(fields))
		for _, f := range fields {
			id, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("parsing node ID %q: %w", f, err)
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	return nil, fmt.Errorf("no 'All nodes:' line found in output")
}

// expectClusterHealthNodeCount asserts that every `rpk cluster health` result
// stashed in ctx (via executeCommandInStatefulsetContainers) reports the
// expected number of node IDs in its "All nodes" line.
func expectClusterHealthNodeCount(ctx context.Context, t framework.TestingT, expectedNodes int32, clusterName string) {
	_ = clusterName // accepted for step symmetry, results are from last exec
	results := ctx.Value(rpkResultsKey{}).([]rpkExecResult)
	require.NotEmpty(t, results, "no execution results found")

	for _, result := range results {
		ids, err := parseHealthNodeIDs(result.rawOutput)
		require.NoError(t, err, "failed to parse output from %s:\n%s", result.clusterName, result.rawOutput)
		t.Logf("cluster %s has nodes: %v", result.clusterName, ids)
		require.Equal(t, int(expectedNodes), len(ids),
			"expected %d nodes in cluster %s but got %d: %v",
			expectedNodes, result.clusterName, len(ids), ids)
	}
}

// expectEventualNodeCountInRemainingClusters polls `rpk cluster health` on
// each still-reachable region and waits until all report the expected node
// count (i.e. the ghost node has been auto-ejected). Offline regions are
// skipped so we don't block on unreachable vclusters.
func expectEventualNodeCountInRemainingClusters(ctx context.Context, t framework.TestingT, expectedNodes int32, clusterName string) {
	nodes := getNodes(ctx, clusterName)

	// Skip vclusters that were taken offline earlier in the scenario.
	var remaining []*vclusterNode
	for _, n := range nodes {
		if n.offline {
			t.Logf("skipping offline region %q", n.logicalName)
			continue
		}
		remaining = append(remaining, n)
	}
	require.NotEmpty(t, remaining, "no reachable regions found")
	t.Logf("polling %d reachable regions for ghost node ejection", len(remaining))

	type execConfig struct {
		node *vclusterNode
		ctl  *kube.Ctl
	}
	configs := make([]execConfig, 0, len(remaining))
	for _, node := range remaining {
		pfCfg, err := node.PortForwardedRESTConfig(ctx)
		require.NoError(t, err, "creating port-forwarded config for %s", node.Name())
		ctl, err := kube.FromRESTConfig(pfCfg)
		require.NoError(t, err, "creating kube ctl for %s", node.Name())
		configs = append(configs, execConfig{node: node, ctl: ctl})
	}

	execInPod := func(cfg execConfig, pod *corev1.Pod, command string) string {
		var out bytes.Buffer
		if err := cfg.ctl.Exec(ctx, pod, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"/bin/bash", "-c", command},
			Stdout:    &out,
		}); err != nil {
			return fmt.Sprintf("error: %v (output: %s)", err, out.String())
		}
		return out.String()
	}

	require.Eventually(t, func() bool {
		for _, cfg := range configs {
			pod := findRedpandaPod(ctx, t, cfg.node)
			if pod == nil {
				t.Logf("no redpanda pod found in %s", cfg.node.Name())
				return false
			}

			healthOutput := execInPod(cfg, pod, "rpk cluster health")
			t.Logf("region %s rpk cluster health:\n%s", cfg.node.logicalName, healthOutput)

			ids, err := parseHealthNodeIDs(healthOutput)
			if err != nil {
				t.Logf("failed to parse health output from %s: %v", cfg.node.logicalName, err)
				t.Logf("region %s broker list:\n%s", cfg.node.logicalName,
					execInPod(cfg, pod, "rpk redpanda admin brokers list"))
				return false
			}

			if len(ids) != int(expectedNodes) {
				t.Logf("region %s has %d nodes %v, expected %d", cfg.node.logicalName, len(ids), ids, expectedNodes)
				t.Logf("region %s broker list:\n%s", cfg.node.logicalName,
					execInPod(cfg, pod, "rpk redpanda admin brokers list"))
				return false
			}
		}
		return true
	}, 10*time.Minute, 5*time.Second, "expected %d nodes in remaining regions after ghost node ejection", expectedNodes)

	t.Logf("all remaining regions report %d nodes as expected", expectedNodes)
}
