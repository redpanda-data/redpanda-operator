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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestNotesForLoadBalancerService(t *testing.T) {
	dot, err := Chart.Dot(nil, helmette.Release{
		Name:      "console",
		Namespace: "test-namespace",
		Service:   "Helm",
	}, PartialValues{
		PartialRenderValues: console.PartialRenderValues{
			Service: &console.PartialServiceConfig{
				Type: ptr.To(corev1.ServiceTypeLoadBalancer),
				Port: ptr.To[int32](8081),
			},
		},
	})
	require.NoError(t, err)

	notes := Notes(dot)

	assert.Contains(t, notes, `    NOTE: It may take a few minutes for the LoadBalancer IP to be available.`)
	assert.Contains(t, notes, `          You can watch the status of by running 'kubectl get --namespace test-namespace svc -w console'`)
	assert.Contains(t, notes, `  export SERVICE_IP=$(kubectl get svc --namespace test-namespace console --template "{{ range (index .status.loadBalancer.ingress 0) }}{{.}}{{ end }}")`)
	assert.Contains(t, notes, `  echo http://$SERVICE_IP:8081`)
	assert.NotContains(t, notes, `  export NODE_PORT=`)
}
