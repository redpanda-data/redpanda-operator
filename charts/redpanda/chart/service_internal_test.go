// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TestSteerEndpoints pins the shape endpoint steering gives a Service: no
// selector, so the native EndpointSlice controller leaves it alone, and the
// annotation naming the cluster for the operator's controller. Only the
// operator calls it, so a chart render is never affected.
func TestSteerEndpoints(t *testing.T) {
	values, err := Chart.LoadValues(map[string]any{
		"service": map[string]any{
			"internal": map[string]any{
				"annotations": map[string]any{
					"example.com/keep": "me",
					// A user-supplied value for the steering annotation is
					// overridden: the controller needs it to name the cluster.
					EndpointSteeringAnnotation: "someone-else",
				},
			},
		},
	})
	require.NoError(t, err)
	dot, err := Chart.Dot(nil, helmette.Release{Name: "rp", Namespace: "rp", Service: "Helm"}, values)
	require.NoError(t, err)
	state, err := RenderStateFromDot(dot)
	require.NoError(t, err)

	svc := ServiceInternal(state)
	require.Equal(t, ClusterPodLabelsSelector(state), svc.Spec.Selector)
	require.Equal(t, "someone-else", svc.Annotations[EndpointSteeringAnnotation])

	SteerEndpoints(svc, "rp")
	require.Nil(t, svc.Spec.Selector)
	require.Equal(t, "rp", svc.Annotations[EndpointSteeringAnnotation])
	require.Equal(t, "me", svc.Annotations["example.com/keep"])
	require.True(t, svc.Spec.PublishNotReadyAddresses, "broker discovery still needs not-ready addresses")
}
