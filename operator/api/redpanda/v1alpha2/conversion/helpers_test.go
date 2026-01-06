// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestYAMLConversion(t *testing.T) {
	dot, err := redpanda.Chart.Dot(nil, helmette.Release{
		Name:      "redpanda",
		Namespace: "redpanda",
		Service:   "Helm",
	}, struct{}{})
	require.NoError(t, err)

	var mounts []applycorev1.VolumeMountApplyConfiguration
	require.NoError(t, convertAndAppendYAMLNotNil(&redpanda.RenderState{
		Dot: dot,
	}, ptr.To(`- name: foo
  mountPath: foo`), &mounts))

	require.GreaterOrEqual(t, 1, len(mounts))
	require.NotNil(t, mounts[0].MountPath)
	require.Equal(t, "foo", *mounts[0].MountPath)
}
