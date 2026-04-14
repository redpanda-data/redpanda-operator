// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodCheck finds the operator pod and validates it is running and ready.
// Populates cc.Pod for downstream checks.
type PodCheck struct{}

func (c *PodCheck) Name() string { return "pod" }

func (c *PodCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	var pods corev1.PodList
	if err := cc.Ctl.List(ctx, cc.Namespace, &pods, client.MatchingLabels{
		"app.kubernetes.io/name": cc.ServiceName,
	}); err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("listing pods: %v", err))}
	}
	if len(pods.Items) == 0 {
		return []Result{Fail(c.Name(), fmt.Sprintf("no pods found with label app.kubernetes.io/name=%s in namespace %s", cc.ServiceName, cc.Namespace))}
	}

	// Prefer running pods.
	pod := &pods.Items[0]
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			pod = &pods.Items[i]
			break
		}
	}
	cc.Pod = pod

	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}

	ready := podReady(pod)
	if !ready {
		return []Result{Fail(c.Name(), fmt.Sprintf("pod %s is not ready (phase: %s, restarts: %d)", pod.Name, pod.Status.Phase, restarts))}
	}

	msg := fmt.Sprintf("pod %s is running and ready", pod.Name)
	if restarts > 0 {
		msg += fmt.Sprintf(" (%d restarts)", restarts)
	}
	return []Result{Pass(c.Name(), msg)}
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
