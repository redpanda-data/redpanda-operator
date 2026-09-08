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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TestAnnotations asserts that commonAnnotations and annotations reach every
// object the chart renders except Pod templates, that no object is rendered
// with a nil annotations map (#1085), and that neither value can change the
// Helm hook annotations on the chart's hook Jobs, ServiceAccounts and RBAC.
func TestAnnotations(t *testing.T) {
	hookKeys := []string{"helm.sh/hook", "helm.sh/hook-weight", "helm.sh/hook-delete-policy"}

	render := func(t *testing.T, values PartialValues) map[string]map[string]string {
		values.CRDs = &PartialCRDs{Enabled: ptr.To(true)}
		values.Monitoring = &PartialMonitoringConfig{Enabled: ptr.To(true), RulesEnabled: ptr.To(true)}
		values.Webhook = &PartialWebhook{Enabled: ptr.To(true)}

		objs, err := Chart.Render(nil, helmette.Release{Name: "operator", Namespace: "redpanda"}, values)
		require.NoError(t, err)

		annotations := map[string]map[string]string{}
		for _, obj := range objs {
			key := fmt.Sprintf("%T %q", obj, obj.GetName())
			got := obj.GetAnnotations()
			require.NotNil(t, got, "%s has nil annotations", key)
			annotations[key] = got

			switch obj := obj.(type) {
			case *appsv1.Deployment:
				require.NotContains(t, obj.Spec.Template.Annotations, "my.co/team", "%s pod template", key)
			case *batchv1.Job:
				require.NotContains(t, obj.Spec.Template.Annotations, "my.co/team", "%s pod template", key)
			}
		}
		return annotations
	}

	base := render(t, PartialValues{})
	got := render(t, PartialValues{
		CommonAnnotations: map[string]string{
			"my.co/team":                 "platform",
			"shared":                     "common",
			"helm.sh/hook":               "bogus",
			"helm.sh/hook-weight":        "999",
			"helm.sh/hook-delete-policy": "bogus",
		},
		Annotations: map[string]string{
			"my.co/owner":                "ops",
			"shared":                     "annotations",
			"helm.sh/hook":               "bogus",
			"helm.sh/hook-weight":        "999",
			"helm.sh/hook-delete-policy": "bogus",
		},
	})
	require.Equal(t, len(base), len(got))

	var hooks int
	for key, annotations := range got {
		require.Equal(t, "platform", annotations["my.co/team"], key)
		require.Equal(t, "ops", annotations["my.co/owner"], key)
		require.Equal(t, "annotations", annotations["shared"], "%s: annotations must win over commonAnnotations", key)

		if _, isHook := base[key]["helm.sh/hook"]; isHook {
			hooks++
			for _, hookKey := range hookKeys {
				require.Equal(t, base[key][hookKey], annotations[hookKey], "%s: %s", key, hookKey)
			}
		}
	}
	require.NotZero(t, hooks, "expected hook objects to be rendered")
}
