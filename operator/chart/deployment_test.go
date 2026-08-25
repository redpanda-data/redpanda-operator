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
	"strings"
	"testing"

	"github.com/spf13/pflag"
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
	dot, err := Chart.Dot(nil, helmette.Release{
		Name:      "rp-op",
		Namespace: "redpanda-operator",
		Service:   "Helm",
	}, partial)
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

// TestEnableShadowLinksFlag covers the crds.enabled -> --enable-shadowlinks
// coupling: when this chart installs the (stable) ShadowLink CRD it must also
// keep the ShadowLink controller registered, or upgrading a release holding
// live ShadowLink CRs would silently stop their reconciliation and deletion
// would hang on the controller-managed finalizer. Installs that manage CRDs
// out-of-band keep the operator's opt-in default, and additionalCmdFlags wins
// over the chart-rendered default in both directions.
func TestEnableShadowLinksFlag(t *testing.T) {
	t.Run("default render omits the flag", func(t *testing.T) {
		spec := renderDeployment(t, nil).Spec.Template.Spec
		for _, arg := range spec.Containers[0].Args {
			assert.NotContains(t, arg, "--enable-shadowlinks")
		}
	})

	t.Run("crds.enabled renders --enable-shadowlinks=true", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{
			"crds": map[string]any{"enabled": true},
		}).Spec.Template.Spec
		assert.Contains(t, spec.Containers[0].Args, "--enable-shadowlinks=true")
	})

	t.Run("additionalCmdFlags wins over the crds.enabled default", func(t *testing.T) {
		spec := renderDeployment(t, map[string]any{
			"crds":               map[string]any{"enabled": true},
			"additionalCmdFlags": []string{"--enable-shadowlinks=false"},
		}).Spec.Template.Spec
		assert.Contains(t, spec.Containers[0].Args, "--enable-shadowlinks=false")
	})
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

// TestCommonAnnotationsFlagRoundTrip proves the rendered --common-annotations
// value parses back through the exact pflag machinery the operator binary
// uses. pflag's StringToString splits multi-pair values with encoding/csv, so
// an unquoted comma inside an annotation value used to split mid-pair and
// crash-loop the operator at startup — a values-only breakage.
func TestCommonAnnotationsFlagRoundTrip(t *testing.T) {
	annotations := map[string]string{
		"owner":       "platform-team@example.com",
		"description": "primary, staging, and dev clusters", // commas — the regression
		"expr":        "a=b",                                // '=' in value
	}

	spec := renderDeployment(t, map[string]any{
		"commonAnnotations": annotations,
	}).Spec.Template.Spec

	var rendered string
	for _, arg := range spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--common-annotations=") {
			rendered = strings.TrimPrefix(arg, "--common-annotations=")
		}
	}
	require.NotEmpty(t, rendered, "expected a --common-annotations argument")

	// Parse with the same flag type the operator binary registers.
	parsed := map[string]string{}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.StringToStringVar(&parsed, "common-annotations", nil, "")
	require.NoError(t, fs.Parse([]string{"--common-annotations=" + rendered}),
		"the rendered flag value must survive pflag's CSV parsing")
	assert.Equal(t, annotations, parsed)
}

// TestQuoteFlagMapPair pins the quoting rules used for pflag StringToString
// flag values.
func TestQuoteFlagMapPair(t *testing.T) {
	assert.Equal(t, "k=v", quoteFlagMapPair("k=v"), "plain pairs pass through")
	assert.Equal(t, "\"k=a,b\"", quoteFlagMapPair("k=a,b"), "commas force CSV quoting")
	assert.Equal(t, "\"k=a\"\"b\"", quoteFlagMapPair("k=a\"b"), "quotes are doubled per CSV")
}
