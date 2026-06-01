// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pvcunbinder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func podWithSidecarArgs(args ...string) *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "redpanda"},
				{Name: "sidecar", Args: args},
			},
		},
	}
}

func TestRunsPVCUnbinderSidecar(t *testing.T) {
	require.True(t, runsPVCUnbinderSidecar(podWithSidecarArgs("--run-pvc-unbinder")))
	require.True(t, runsPVCUnbinderSidecar(podWithSidecarArgs("--run-pvc-unbinder=true")))
	require.False(t, runsPVCUnbinderSidecar(podWithSidecarArgs("--run-decommissioner")))
	require.False(t, runsPVCUnbinderSidecar(podWithSidecarArgs()))
	require.False(t, runsPVCUnbinderSidecar(podWithSidecarArgs("--run-pvc-unbinder-extra")))
}

// TestShouldRemediateDefersToSidecar verifies the operator-wide PVCUnbinder
// backs off for a Pod whose cluster already runs the pvc-unbinder sidecar, even
// when the Pod would otherwise be a remediation candidate.
func TestShouldRemediateDefersToSidecar(t *testing.T) {
	r := &Controller{}

	pod := podWithSidecarArgs("--run-pvc-unbinder")
	pod.Status.Phase = corev1.PodPending

	ok, requeue := r.ShouldRemediate(context.Background(), pod)
	require.False(t, ok)
	require.Zero(t, requeue)
}
