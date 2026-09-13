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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRankWorkerNodes(t *testing.T) {
	node := func(name string, ready corev1.ConditionStatus, labels map[string]string) corev1.Node {
		return corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: ready}}},
		}
	}
	pod := func(nodeName string, phase corev1.PodPhase) corev1.Pod {
		return corev1.Pod{Spec: corev1.PodSpec{NodeName: nodeName}, Status: corev1.PodStatus{Phase: phase}}
	}

	nodes := []corev1.Node{
		node("k3d-harpoon-server-0", corev1.ConditionTrue, map[string]string{"node-role.kubernetes.io/control-plane": "true"}),
		node("k3d-harpoon-agent-0", corev1.ConditionTrue, nil),
		node("k3d-harpoon-agent-1", corev1.ConditionFalse, nil),
		node("k3d-harpoon-agent-2", corev1.ConditionTrue, nil),
		node("k3d-harpoon-agent-3", corev1.ConditionTrue, nil),
	}
	pods := []corev1.Pod{
		pod("k3d-harpoon-agent-0", corev1.PodRunning),
		pod("k3d-harpoon-agent-0", corev1.PodRunning),
		pod("k3d-harpoon-agent-0", corev1.PodPending),
		pod("k3d-harpoon-agent-2", corev1.PodRunning),
		pod("k3d-harpoon-agent-2", corev1.PodSucceeded),
		pod("k3d-harpoon-agent-2", corev1.PodFailed),
		pod("k3d-harpoon-server-0", corev1.PodRunning),
	}

	// The control-plane node and the NotReady agent are excluded; the rest
	// are ordered by live pods, so an idle agent beats a busy lower-numbered one.
	require.Equal(t, []string{"k3d-harpoon-agent-3", "k3d-harpoon-agent-2", "k3d-harpoon-agent-0"}, rankWorkerNodes(nodes, pods))

	// Idle nodes fall back to name order for a deterministic choice.
	require.Equal(t, []string{"k3d-harpoon-agent-0", "k3d-harpoon-agent-2", "k3d-harpoon-agent-3"}, rankWorkerNodes(nodes, nil))
}
