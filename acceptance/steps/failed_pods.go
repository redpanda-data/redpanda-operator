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
	"maps"
	"slices"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

type stuckPodKey struct{ cluster string }

type stuckPod struct {
	name string
	uid  types.UID
}

// aPodShouldBeUnableToStart waits for one of the cluster's broker pods to fail
// to start in a way the operator replaces, and records it for the steps below.
func aPodShouldBeUnableToStart(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	var stuck stuckPod
	require.Eventually(t, func() bool {
		pods, err := brokerPods(ctx, t, clusterName)
		if err != nil {
			t.Logf("failed to list pods: %v", err)
			return false
		}
		for _, pod := range pods {
			if reason := cannotStart(&pod); reason != "" {
				t.Logf("Pod %q of cluster %q can't start: %s", pod.Name, clusterName, reason)
				stuck = stuckPod{name: pod.Name, uid: pod.UID}
				return true
			}
		}
		return false
	}, 10*time.Minute, 5*time.Second, "no pod of cluster %q failed to start", clusterName)
	return context.WithValue(ctx, stuckPodKey{clusterName}, stuck)
}

// noPodShouldBeReplacedFor asserts no broker pod is recreated for the window:
// one that can't start on its current template is kubelet's to restart, and
// the roll waits on it rather than moving another pod onto that template.
func noPodShouldBeReplacedFor(ctx context.Context, t framework.TestingT, clusterName string, seconds int) {
	before := brokerPodUIDs(ctx, t, clusterName)
	var after map[string]types.UID
	require.Never(t, func() bool {
		pods, err := brokerPods(ctx, t, clusterName)
		if err != nil {
			t.Logf("failed to list pods: %v", err)
			return false
		}
		after = uidsOf(pods)
		return !maps.Equal(before, after)
	}, time.Duration(seconds)*time.Second, 5*time.Second, "pods of cluster %q were replaced: before %v, after %v", clusterName, before, after)
}

// theStuckPodShouldBeReplacedFirst waits for the pod that couldn't start to
// come back Ready on a new incarnation, failing if any other pod since the UID
// snapshot is recreated before that: a healthy broker must never be rolled
// while another is down.
func theStuckPodShouldBeReplacedFirst(ctx context.Context, t framework.TestingT, clusterName string) {
	snap, ok := ctx.Value(podUIDSnapshotKey{clusterName}).(map[string]string)
	require.True(t, ok, "no pod UID snapshot found for cluster %q", clusterName)
	stuck := stuckPodOf(ctx, t, clusterName)

	// Eventually runs the condition off the test goroutine, where FailNow
	// can't stop the test: a roll out of turn ends the wait instead.
	var rolled string
	require.Eventually(t, func() bool {
		pods, err := brokerPods(ctx, t, clusterName)
		if err != nil {
			t.Logf("failed to list pods: %v", err)
			return false
		}
		// The gated roll that follows the replacement may land in the same
		// poll: only a pod recreated before it turned Ready rolled out of turn.
		var ready *corev1.PodCondition
		for _, pod := range pods {
			if pod.Name == stuck.name && pod.UID != stuck.uid && utils.IsPodReady(&pod) {
				ready = utils.FindStatusPodCondition(pod.Status.Conditions, corev1.PodReady)
			}
		}
		for _, pod := range pods {
			if old, ok := snap[pod.Name]; ok && pod.Name != stuck.name && old != string(pod.UID) &&
				(ready == nil || pod.CreationTimestamp.Before(&ready.LastTransitionTime)) {
				rolled = pod.Name
				return true
			}
		}
		return ready != nil
	}, 10*time.Minute, 5*time.Second, "pod %q of cluster %q was never replaced", stuck.name, clusterName)
	require.Empty(t, rolled, "pod %q was rolled while %q, which couldn't start, wasn't yet replaced", rolled, stuck.name)
}

// onlyTheStuckPodShouldHaveBeenReplaced asserts, against the UID snapshot,
// that the pod that couldn't start was recreated and every other kept its UID.
func onlyTheStuckPodShouldHaveBeenReplaced(ctx context.Context, t framework.TestingT, clusterName string) {
	snap, ok := ctx.Value(podUIDSnapshotKey{clusterName}).(map[string]string)
	require.True(t, ok, "no pod UID snapshot found for cluster %q", clusterName)
	stuck := stuckPodOf(ctx, t, clusterName)

	current := brokerPodUIDs(ctx, t, clusterName)
	for name, old := range snap {
		uid, exists := current[name]
		require.True(t, exists, "pod %q is missing", name)
		if name == stuck.name {
			require.NotEqual(t, types.UID(old), uid, "pod %q, which couldn't start, wasn't replaced", name)
			continue
		}
		require.Equal(t, types.UID(old), uid, "pod %q was replaced, though it could start", name)
	}
}

// cannotStart returns why pod's redpanda container can't start, or "": an
// init container that can't run holds it back too, as the chart's share its
// image.
func cannotStart(pod *corev1.Pod) string {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			return "unschedulable"
		}
	}
	for _, cs := range slices.Concat(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses) {
		if cs.State.Waiting == nil {
			continue
		}
		switch reason := cs.State.Waiting.Reason; reason {
		case "CrashLoopBackOff", "ErrImagePull", "ImagePullBackOff", "CreateContainerConfigError":
			return cs.Name + ": " + reason
		}
	}
	return ""
}

func stuckPodOf(ctx context.Context, t framework.TestingT, clusterName string) stuckPod {
	stuck, ok := ctx.Value(stuckPodKey{clusterName}).(stuckPod)
	require.True(t, ok, "no pod of cluster %q was recorded as unable to start", clusterName)
	return stuck
}

func brokerPodUIDs(ctx context.Context, t framework.TestingT, clusterName string) map[string]types.UID {
	pods, err := brokerPods(ctx, t, clusterName)
	require.NoError(t, err)
	return uidsOf(pods)
}

func brokerPods(ctx context.Context, t framework.TestingT, clusterName string) ([]corev1.Pod, error) {
	var pods corev1.PodList
	err := t.List(ctx, &pods, runtimeclient.InNamespace(t.ResourceKey(clusterName).Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "redpanda",
	})
	return pods.Items, err
}

func uidsOf(pods []corev1.Pod) map[string]types.UID {
	uids := map[string]types.UID{}
	for _, pod := range pods {
		uids[pod.Name] = pod.UID
	}
	return uids
}
