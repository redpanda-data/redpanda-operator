// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTLSRoutesForListenerRequiresHost(t *testing.T) {
	require.PanicsWithValue(t,
		"gateway listener kafka/default requires host",
		func() {
			tlsRoutesForListener(
				"redpanda",
				"default",
				map[string]string{"app": "redpanda"},
				[]TLSRouteParentRef{{Name: "shared-gateway"}},
				[]string{"redpanda-0"},
				"",
				"",
				"default",
				"kafka",
				9094,
			)
		},
	)
}

func TestTLSRoutesForKafkaListenerRequiresHostTemplateForMultiBroker(t *testing.T) {
	require.PanicsWithValue(t,
		"gateway listener kafka/default requires hostTemplate when replicas > 1",
		func() {
			tlsRoutesForListener(
				"redpanda",
				"default",
				map[string]string{"app": "redpanda"},
				[]TLSRouteParentRef{{Name: "shared-gateway"}},
				[]string{"redpanda-0", "redpanda-1"},
				"redpanda.example.com",
				"",
				"default",
				"kafka",
				9094,
			)
		},
	)
}

func TestTLSRoutesForHTTPListenerAllowsBootstrapOnlyHost(t *testing.T) {
	routes := tlsRoutesForListener(
		"redpanda",
		"default",
		map[string]string{"app": "redpanda"},
		[]TLSRouteParentRef{{Name: "shared-gateway"}},
		[]string{"redpanda-0", "redpanda-1"},
		"proxy.example.com",
		"",
		"default",
		"http",
		8082,
	)

	require.Len(t, routes, 1)
	require.Equal(t, metav1.TypeMeta{
		APIVersion: "gateway.networking.k8s.io/v1alpha2",
		Kind:       "TLSRoute",
	}, routes[0].TypeMeta)
	require.Equal(t, []string{"proxy.example.com"}, routes[0].Spec.Hostnames)
}
