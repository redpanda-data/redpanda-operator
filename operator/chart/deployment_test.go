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
	corev1 "k8s.io/api/core/v1"

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

// TestDeploymentSchedulingFields covers the pod-scheduling values
// (priorityClassName, nodeSelector, tolerations) flowing onto the operator
// Deployment's pod spec. priorityClassName is the field added in this change;
// the default render must leave it empty (a no-op, no PriorityClass), and a
// set value must land on the pod spec so the operator can be made
// non-preemptible under node pressure.
func TestDeploymentSchedulingFields(t *testing.T) {
	t.Run("priorityClassName: empty by default", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{}).Spec.Template.Spec
		assert.Empty(t, spec.PriorityClassName)
	})

	t.Run("priorityClassName: set value lands on the pod spec", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{
			"priorityClassName": "high-priority",
		}).Spec.Template.Spec
		assert.Equal(t, "high-priority", spec.PriorityClassName)
	})

	t.Run("nodeSelector and tolerations flow through alongside it", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{
			"priorityClassName": "high-priority",
			"nodeSelector":      map[string]any{"disktype": "ssd"},
			"tolerations": []map[string]any{{
				"key":      "dedicated",
				"operator": "Equal",
				"value":    "operator",
				"effect":   "NoSchedule",
			}},
		}).Spec.Template.Spec

		assert.Equal(t, "high-priority", spec.PriorityClassName)
		assert.Equal(t, map[string]string{"disktype": "ssd"}, spec.NodeSelector)
		require.Len(t, spec.Tolerations, 1)
		assert.Equal(t, corev1.Toleration{
			Key:      "dedicated",
			Operator: corev1.TolerationOpEqual,
			Value:    "operator",
			Effect:   corev1.TaintEffectNoSchedule,
		}, spec.Tolerations[0])
	})
}

// TestAddControllerSyncIntervalArgs covers the controllers.<resource>.interval ->
// --<resource>-sync-interval rendering shared by the single-cluster and
// multicluster operator deployments. A set value renders its flag; an empty
// value renders nothing (so the operator's built-in default applies and the
// default chart render stays flag-free).
func TestAddControllerSyncIntervalArgs(t *testing.T) {
	t.Run("renders set intervals and omits empty ones", func(t *testing.T) {
		defaults := map[string]string{}
		addControllerSyncIntervalArgs(defaults, Values{Controllers: Controllers{
			Topic: ControllerSyncConfig{Interval: "45s"},
			User:  ControllerSyncConfig{Interval: "20s"},
			// group/schema/role/shadowLink intentionally left empty
		}})

		assert.Equal(t, "45s", defaults["--topic-sync-interval"])
		assert.Equal(t, "20s", defaults["--user-sync-interval"])
		for _, flag := range []string{
			"--group-sync-interval",
			"--schema-sync-interval",
			"--role-sync-interval",
			"--shadowlink-sync-interval",
		} {
			_, ok := defaults[flag]
			assert.Falsef(t, ok, "%s should be omitted when its interval is empty", flag)
		}
	})

	t.Run("all empty renders no flags", func(t *testing.T) {
		defaults := map[string]string{}
		addControllerSyncIntervalArgs(defaults, Values{})
		assert.Empty(t, defaults)
	})
}
