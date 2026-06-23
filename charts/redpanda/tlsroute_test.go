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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TestRenderResourcesGatewayTLSRouteMatchesTypes is the operator-path regression
// for the syncer invariant: every object returned by RenderResources must have a
// Go type present in Types(), or the operator's kube.Syncer rejects the reconcile
// (".Render returned %T which isn't present in .Types"). The chart renders
// TLSRoutes as the lightweight *redpanda.TLSRoute (for gotohelm); the operator
// path must convert them to the upstream gatewayv1.TLSRoute that Types()
// declares. The helm/golden tests bypass the syncer, which is why a gateway
// cluster reconcile previously failed despite passing golden tests.
func TestRenderResourcesGatewayTLSRouteMatchesTypes(t *testing.T) {
	values := map[string]any{
		"external": map[string]any{
			"enabled": true,
			"domain":  "test.local",
			"gateway": map[string]any{
				"enabled":        true,
				"advertisedPort": 9094,
				"parentRefs":     []any{map[string]any{"name": "redpanda-gateway", "sectionName": "kafka"}},
			},
		},
		"tls": map[string]any{
			"enabled": true,
			"certs":   map[string]any{"default": map[string]any{"caEnabled": true}},
		},
		"statefulset": map[string]any{"replicas": 2},
		"listeners": map[string]any{
			"kafka": map[string]any{
				"external": map[string]any{
					"default": map[string]any{
						"port":         9094,
						"type":         "tlsroute",
						"host":         "redpanda.test.local",
						"hostTemplate": "redpanda-$POD_ORDINAL.test.local",
						"tls":          map[string]any{"enabled": true, "cert": "default"},
					},
				},
			},
		},
	}

	helmValues, err := Chart.LoadValues(values)
	require.NoError(t, err)
	dot, err := Chart.Dot(nil, helmette.Release{Name: "rp", Namespace: "rp", Service: "Helm"}, helmValues)
	require.NoError(t, err)
	state, err := RenderStateFromDot(dot)
	require.NoError(t, err)

	resources, err := RenderResources(state)
	require.NoError(t, err)

	allowed := map[reflect.Type]bool{}
	for _, typ := range Types() {
		allowed[reflect.TypeOf(typ)] = true
	}

	sawUpstreamTLSRoute := false
	for _, obj := range resources {
		require.Truef(t, allowed[reflect.TypeOf(obj)],
			"RenderResources returned %T which is not in Types(); the operator syncer would reject this reconcile", obj)
		switch obj.(type) {
		case *TLSRoute:
			t.Fatalf("lightweight *redpanda.TLSRoute leaked into RenderResources output; the operator syncer would reject it")
		case *gatewayv1.TLSRoute:
			sawUpstreamTLSRoute = true
		}
	}
	require.True(t, sawUpstreamTLSRoute, "expected at least one upstream gatewayv1.TLSRoute for a gateway-enabled cluster")
}

// TestValidateGatewayListener covers the upfront validation that replaced the
// inline panics in tlsRoutesForListener (see validateGatewayListeners). It
// documents the per-protocol per-broker routing policy: Kafka requires
// hostTemplate for multi-broker clusters, while HTTP/Admin/Schema Registry are
// load-balanceable and may run bootstrap-only.
func TestValidateGatewayListener(t *testing.T) {
	// gatewayConfigured=true throughout unless stated otherwise.
	// host is required for every gateway listener, regardless of protocol.
	for _, tag := range []string{"kafka", "http", "admin", "schema"} {
		require.PanicsWithValue(t,
			"external gateway listener "+tag+"/default requires `host` (the bootstrap SNI hostname) when type: tlsroute",
			func() {
				validateGatewayListener(tag, "default", true /*isGateway*/, true /*enabled*/, true /*gatewayConfigured*/, "" /*host*/, "", 1, tag == "kafka")
			},
			"%s: missing host must fail", tag,
		)
	}

	// A type: tlsroute listener while the global gateway is not enabled/configured
	// must fail closed (no NodePort/LoadBalancer fallback) — this is the fail-open
	// exposure gap from the re-review.
	require.PanicsWithValue(t,
		"external listener kafka/default sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs",
		func() {
			validateGatewayListener("kafka", "default", true /*isGateway*/, true /*enabled*/, false /*gatewayConfigured*/, "redpanda.example.com", "", 1, true)
		},
	)

	// Kafka with >1 broker and no hostTemplate is an error (clients need
	// per-broker SNI hosts).
	require.PanicsWithValue(t,
		"external gateway listener kafka/default requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname",
		func() {
			validateGatewayListener("kafka", "default", true, true, true, "redpanda.example.com", "", 3 /*replicas*/, true)
		},
	)

	// These must NOT panic:
	require.NotPanics(t, func() {
		// Kafka, single broker, no hostTemplate — bootstrap-only is fine.
		validateGatewayListener("kafka", "default", true, true, true, "redpanda.example.com", "", 1, true)
		// HTTP/Admin/Schema, multi-broker, no hostTemplate — load-balanceable,
		// bootstrap-only is a valid configuration.
		validateGatewayListener("http", "default", true, true, true, "proxy.example.com", "", 3, false)
		validateGatewayListener("admin", "default", true, true, true, "admin.example.com", "", 3, false)
		validateGatewayListener("schema", "default", true, true, true, "sr.example.com", "", 3, false)
		// Not a gateway listener / disabled — skipped entirely (global gateway
		// state is irrelevant when the listener isn't a gateway listener).
		validateGatewayListener("kafka", "default", false, true, false, "", "", 3, true)
		validateGatewayListener("kafka", "default", true, false, false, "", "", 3, true)
	})
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
		APIVersion: "gateway.networking.k8s.io/v1",
		Kind:       "TLSRoute",
	}, routes[0].TypeMeta)
	require.Equal(t, []string{"proxy.example.com"}, routes[0].Spec.Hostnames)
}
