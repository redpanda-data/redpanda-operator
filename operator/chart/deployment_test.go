// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package operator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// renderDeployment renders the operator Deployment with the given partial
// values merged over the chart defaults.
func renderDeployment(t *testing.T, partial map[string]any) *appsv1.Deployment {
	t.Helper()
	values, err := Chart.LoadValues(partial)
	require.NoError(t, err)
	dot, err := Chart.Dot(nil, helmette.Release{
		Name:      "rp-op",
		Namespace: "redpanda-operator",
		Service:   "Helm",
	}, values)
	require.NoError(t, err)
	return Deployment(dot)
}

func TestChangeDefaultFlag(t *testing.T) {
	t.Run("change default enable console flag", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{
			"additionalCmdFlags": []string{
				"--enable-console=false",
			},
		}).Spec.Template.Spec
		assert.Contains(t, spec.Containers[0].Args, "--enable-console=false")
	})
}
