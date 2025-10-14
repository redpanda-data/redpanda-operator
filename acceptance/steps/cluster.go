// Copyright 2025 Redpanda Data, Inc.
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
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

func checkClusterAvailability(ctx context.Context, t framework.TestingT, version, clusterName string) {
	if getVersion(t, version) == "vectorized" {
		checkV1ClusterAvailability(ctx, t, clusterName)
		return
	}
	checkV2ClusterAvailability(ctx, t, clusterName)
}

func checkV1ClusterAvailability(ctx context.Context, t framework.TestingT, clusterName string) {
	var cluster vectorizedv1alpha1.Cluster

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q is ready", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasConditionQuiescent := hasV1Condition(vectorizedv1alpha1.ClusterCondition{
			Type:   vectorizedv1alpha1.OperatorQuiescentConditionType,
			Status: corev1.ConditionTrue,
		}, cluster.Status.Conditions)

		hasCondition := hasConditionQuiescent

		t.Logf(`Checking cluster resource conditions contains "OperatorQuiescent"? %v`, hasCondition)
		return hasCondition
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained the condition reason "OperatorQuiescent", final Conditions: %+v`, key.String(), cluster.Status.Conditions)
	}))
	t.Logf("Cluster %q is ready!", clusterName)
}

func hasV1Condition(expected vectorizedv1alpha1.ClusterCondition, conditions []vectorizedv1alpha1.ClusterCondition) bool {
	for _, condition := range conditions {
		if expected.Type == condition.Type && expected.Status == condition.Status {
			return true
		}
	}
	return false
}

func checkV2ClusterAvailability(ctx context.Context, t framework.TestingT, clusterName string) {
	var cluster redpandav1alpha2.Redpanda

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q is ready", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasConditionReady := t.HasCondition(metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		}, cluster.Status.Conditions)
		// legacy status condition below
		hasConditionDeployed := t.HasCondition(metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "RedpandaClusterDeployed",
		}, cluster.Status.Conditions)

		hasCondition := hasConditionDeployed || hasConditionReady

		t.Logf(`Checking cluster resource conditions contains "Ready"? %v`, hasCondition)
		return hasCondition
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained the condition reason "Ready", final Conditions: %+v`, key.String(), cluster.Status.Conditions)
	}))
	t.Logf("Cluster %q is ready!", clusterName)
}

func redpandaClusterIsHealthy(ctx context.Context, t framework.TestingT, cluster string) {
	clients := clientsForCluster(ctx, cluster)
	var health rpadmin.ClusterHealthOverview
	var err error

	c := clients.RedpandaAdmin(ctx)

	require.Eventually(t, func() bool {
		health, err = c.GetHealthOverview(ctx)
		require.NoError(t, err)

		t.Logf("Cluster health: %v", health.IsHealthy)
		return health.IsHealthy
	}, 5*time.Minute, 5*time.Second, `Cluster %q never become healthy: %+v`, cluster, health)
}

func checkClusterUnhealthy(ctx context.Context, t framework.TestingT, clusterName string) {
	checkClusterHealthCondition(ctx, t, clusterName, "NotHealthy", metav1.ConditionFalse)
}

func checkClusterHealthy(ctx context.Context, t framework.TestingT, clusterName string) {
	checkClusterHealthCondition(ctx, t, clusterName, "Healthy", metav1.ConditionTrue)
}

func checkClusterHealthCondition(ctx context.Context, t framework.TestingT, clusterName, reason string, status metav1.ConditionStatus) {
	var cluster redpandav1alpha2.Redpanda

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q Healthy reason %q", clusterName, reason)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasCondition := t.HasCondition(metav1.Condition{
			Type:   "Healthy",
			Status: status,
			Reason: reason,
		}, cluster.Status.Conditions)

		t.Logf(`Checking cluster conditions contains Healthy reason %q? %v`, reason, hasCondition)
		return hasCondition
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained the condition reason %q, final Conditions: %+v`, key.String(), reason, cluster.Status.Conditions)
	}))
	t.Logf("Cluster %q contains Healthy reason %q!", clusterName, reason)
}

func shutdownRandomClusterNode(ctx context.Context, t framework.TestingT, clusterName string) {
	var clusterSet appsv1.StatefulSet

	key := t.ResourceKey(clusterName)

	require.NoError(t, t.Get(ctx, key, &clusterSet))

	selector, err := metav1.LabelSelectorAsSelector(clusterSet.Spec.Selector)
	require.NoError(t, err)

	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, client.MatchingLabelsSelector{
		Selector: selector,
	}))

	require.Greater(t, len(pods.Items), 0)

	index := rand.IntN(len(pods.Items)) // nolint:gosec
	pod := pods.Items[index]

	t.ShutdownNode(ctx, pod.Spec.NodeName)
}

func shutdownNodeOfPod(ctx context.Context, t framework.TestingT, podName string) {
	t.ResourceKey(podName)

	var pod corev1.Pod
	require.NoError(t, t.Get(ctx, t.ResourceKey(podName), &pod))

	var node corev1.Node
	require.NoError(t, t.Get(ctx, t.ResourceKey(pod.Spec.NodeName), &node))

	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
		Key:    "node.kubernetes.io/out-of-service",
		Effect: corev1.TaintEffectNoExecute,
	})

	require.NoError(t, t.Update(ctx, &node))

	t.ShutdownNode(ctx, pod.Spec.NodeName)
}

func deleteNotReadyKubernetesNodes(ctx context.Context, t framework.TestingT) {
	var nodes corev1.NodeList
	require.NoError(t, t.List(ctx, &nodes))
	for _, node := range nodes.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && (condition.Status == corev1.ConditionFalse || condition.Status == corev1.ConditionUnknown) {
				t.Logf("Deleting Kubernetes node: %q", node.Name)
				t.DeleteNode(ctx, node.Name)
			}
		}
	}
}

func checkClusterNodeCount(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int32) {
	var cluster redpandav1alpha2.Redpanda
	var actualNodeCount int32

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q node count is %d", clusterName, nodeCount)
	require.Eventually(t, func() bool {
		actualNodeCount = 0
		require.NoError(t, t.Get(ctx, key, &cluster))

		for _, pool := range cluster.Status.NodePools {
			actualNodeCount += pool.UpToDateReplicas
		}

		matchesCount := nodeCount == actualNodeCount
		t.Logf("Checking cluster %q has %d total nodes? %v (%d)", clusterName, nodeCount, matchesCount, actualNodeCount)

		return matchesCount
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never had a matching node count, node Count: %d`, key.String(), actualNodeCount)
	}))
	t.Logf("Cluster %q has %d nodes!", clusterName, nodeCount)
}

func checkClusterStableWithCount(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int32) {
	var cluster redpandav1alpha2.Redpanda
	var actualNodeCount int32

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q is stable", clusterName)
	require.Eventually(t, func() bool {
		actualNodeCount = 0
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasCondition := t.HasCondition(metav1.Condition{
			Type:   "Stable",
			Status: metav1.ConditionTrue,
			Reason: "Stable",
		}, cluster.Status.Conditions)

		t.Logf(`Checking cluster resource conditions contains "Stable"? %v`, hasCondition)
		for _, pool := range cluster.Status.NodePools {
			if pool.DesiredReplicas != pool.Replicas {
				t.Logf("Pool %q has %d nodes which does not match desired number of nodes: %d", pool.Name, pool.Replicas, pool.DesiredReplicas)
				return false
			}
			if pool.UpToDateReplicas != pool.Replicas {
				t.Logf("Pool %q has %d nodes that are out-of-date", pool.Name, pool.OutOfDateReplicas)
				return false
			}
			if pool.ReadyReplicas != pool.Replicas {
				t.Logf("Pool %q has %d nodes that are not ready", pool.Name, pool.Replicas-pool.ReadyReplicas)
				return false
			}
			actualNodeCount += pool.UpToDateReplicas
		}
		matchesCount := nodeCount == actualNodeCount
		t.Logf("Checking cluster %q has %d total nodes? %v (%d)", clusterName, nodeCount, matchesCount, actualNodeCount)

		return hasCondition && matchesCount
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained the condition reason "Ready" with a matching node count, node Count: %d, final Conditions: %+v`, key.String(), actualNodeCount, cluster.Status.Conditions)
	}))
	t.Logf("Cluster %q is stable with %d nodes!", clusterName, nodeCount)
}

func enableDevelopmentFeatureOn(ctx context.Context, t framework.TestingT, feature, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	clients.EnableFeature(ctx, feature)
}

func setLogLevelOn(ctx context.Context, t framework.TestingT, level, logger, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	clients.SetLogLevel(ctx, level, logger)
}
