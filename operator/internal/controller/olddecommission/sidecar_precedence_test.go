// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package olddecommission

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestRunsDecommissionerSidecar(t *testing.T) {
	spec := func(args ...string) *corev1.PodSpec {
		return &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "redpanda"},
				{Name: "sidecar", Args: args},
			},
		}
	}

	require.True(t, runsDecommissionerSidecar(spec("run", "--run-decommissioner")))
	require.True(t, runsDecommissionerSidecar(spec("--run-decommissioner=true")))
	require.False(t, runsDecommissionerSidecar(spec("--run-pvc-unbinder")))
	require.False(t, runsDecommissionerSidecar(spec()))
	// A flag that is a prefix of the container's arg must not match.
	require.False(t, runsDecommissionerSidecar(spec("--run-decommissioner-extra")))
}
