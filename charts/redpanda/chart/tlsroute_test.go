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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TestRenderResourcesGatewayTLSRouteMatchesTypes is the operator-path regression
// for the syncer invariant: every object returned by RenderResources must have a
// Go type present in Types(), or the operator's kube.Syncer rejects the reconcile
// (".Render returned %T which isn't present in .Types"). The helm/golden tests
// bypass the syncer, which is why a gateway cluster reconcile once failed despite
// passing golden tests.
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
		if _, ok := obj.(*gatewayv1.TLSRoute); ok {
			sawUpstreamTLSRoute = true
		}
	}
	require.True(t, sawUpstreamTLSRoute, "expected at least one upstream gatewayv1.TLSRoute for a gateway-enabled cluster")
}

// TestValidateGatewayListener covers the upfront validation that replaced the
// inline panics in TLSRoute rendering (see validateGatewayListeners). It
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
				validateGatewayListener(tag, "default", "tlsroute" /*listenerType*/, true /*enabled*/, true /*gatewayConfigured*/, "" /*host*/, "", 1, tag == "kafka")
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
			validateGatewayListener("kafka", "default", "tlsroute" /*listenerType*/, true /*enabled*/, false /*gatewayConfigured*/, "redpanda.example.com", "", 1, true)
		},
	)

	// Kafka with >1 broker and no hostTemplate is an error (clients need
	// per-broker SNI hosts).
	require.PanicsWithValue(t,
		"external gateway listener kafka/default requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname",
		func() {
			validateGatewayListener("kafka", "default", "tlsroute", true, true, "redpanda.example.com", "", 3 /*replicas*/, true)
		},
	)

	// These must NOT panic:
	require.NotPanics(t, func() {
		// Kafka, single broker, no hostTemplate — bootstrap-only is fine.
		validateGatewayListener("kafka", "default", "tlsroute", true, true, "redpanda.example.com", "", 1, true)
		// HTTP/Admin/Schema, multi-broker, no hostTemplate — load-balanceable,
		// bootstrap-only is a valid configuration.
		validateGatewayListener("http", "default", "tlsroute", true, true, "proxy.example.com", "", 3, false)
		validateGatewayListener("admin", "default", "tlsroute", true, true, "admin.example.com", "", 3, false)
		validateGatewayListener("schema", "default", "tlsroute", true, true, "sr.example.com", "", 3, false)
		// Not a gateway listener / disabled — skipped entirely (global gateway
		// state is irrelevant when the listener isn't a gateway listener).
		validateGatewayListener("kafka", "default", "", true, false, "", "", 3, true)
		validateGatewayListener("kafka", "default", "tlsroute", false, false, "", "", 3, true)
	})

	// type: tcproute: host is the shared advertised host; per-broker SNI hosts
	// don't apply, so a multi-broker Kafka listener needs no hostTemplate.
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp requires `host` (the advertised host every broker shares) when type: tcproute",
		func() {
			validateGatewayListener("kafka", "tcp", "tcproute", true, true, "", "", 3, true)
		},
	)
	require.NotPanics(t, func() {
		validateGatewayListener("kafka", "tcp", "tcproute", true, true, "gw.example.com", "", 3, true)
	})

	// TCPRoute ports: bootstrap and (multi-broker Kafka) per-broker ports are
	// required, in range, and unique per Gateway across every tcproute listener.
	gwA := []gatewayv1.ParentReference{{Name: "gw-a", Namespace: ptr.To(gatewayv1.Namespace("infra"))}}
	gwB := []gatewayv1.ParentReference{{Name: "gw-b"}}
	fresh := func() map[string]map[string]string { return map[string]map[string]string{} }
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp requires `networkPort` (the Gateway listener port of the bootstrap TCPRoute) when type: tcproute",
		func() { validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 0, 9200, 3, true) },
	)
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp requires `brokerNetworkPortBase` when replicas > 1: TCPRoutes carry no hostname, so each broker needs its own Gateway port",
		func() { validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 0, 3, true) },
	)
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp: broker 1 network port 65536 is outside 1-65535",
		func() {
			validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 65535, 2, true)
		},
	)
	claimed := fresh()
	validateTCPRouteListener(claimed, 64, gwA, "redpanda", "http", "tcp", true, 9201, 0, 3, false) // bootstrap-only HTTP
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp: broker 1 network port 9201 is already used by http/tcp bootstrap; every TCPRoute needs its own Gateway listener port",
		func() {
			validateTCPRouteListener(claimed, 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 9200, 3, true)
		},
	)
	// Another Gateway (another load balancer) can reuse the same port numbers,
	// e.g. TLS and plaintext Kafka listeners on separate Gateways.
	claimed = fresh()
	require.NotPanics(t, func() {
		validateTCPRouteListener(claimed, 64, gwA, "redpanda", "kafka", "tls", true, 9199, 9200, 3, true)
		validateTCPRouteListener(claimed, 64, gwB, "redpanda", "kafka", "plain", true, 9199, 9200, 3, true)
	})
	// 64 ports per Gateway (the Gateway API listener cap): 1 bootstrap + 63
	// brokers fit, a 64th broker doesn't.
	require.NotPanics(t, func() {
		validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 9200, 63, true)
	})
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp: broker 63 needs more than 64 TCPRoute ports on Gateway infra/gw-a (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs",
		func() {
			validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 9200, 64, true)
		},
	)
	// A lower budget for the load balancer behind the Gateway, e.g. an AWS
	// NLB's 50: 1 bootstrap + 49 brokers fit, a 50th broker doesn't.
	require.NotPanics(t, func() {
		validateTCPRouteListener(fresh(), 50, gwA, "redpanda", "kafka", "tcp", true, 9199, 9200, 49, true)
	})
	require.PanicsWithValue(t,
		"external gateway listener kafka/tcp: broker 49 needs more than 50 TCPRoute ports on Gateway infra/gw-a (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs",
		func() {
			validateTCPRouteListener(fresh(), 50, gwA, "redpanda", "kafka", "tcp", true, 9199, 9200, 50, true)
		},
	)
	require.NotPanics(t, func() {
		validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", true, 9199, 0, 1, true) // single broker
		validateTCPRouteListener(fresh(), 64, gwA, "redpanda", "kafka", "tcp", false, 0, 0, 3, true)   // not tcproute
	})
}

func TestTLSRoutesForHTTPListenerAllowsBootstrapOnlyHost(t *testing.T) {
	helmValues, err := Chart.LoadValues(map[string]any{
		"external": map[string]any{
			"enabled": true,
			"gateway": map[string]any{
				"enabled":    true,
				"parentRefs": []any{map[string]any{"name": "shared-gateway"}},
			},
		},
		"statefulset": map[string]any{"replicas": 2},
		"listeners": map[string]any{
			"kafka": map[string]any{"external": map[string]any{"default": map[string]any{"enabled": false}}},
			"http": map[string]any{"external": map[string]any{"default": map[string]any{
				"port": 8082, "type": "tlsroute", "host": "proxy.example.com",
			}}},
		},
	})
	require.NoError(t, err)
	dot, err := Chart.Dot(nil, helmette.Release{Name: "redpanda", Namespace: "default", Service: "Helm"}, helmValues)
	require.NoError(t, err)
	state, err := RenderStateFromDot(dot)
	require.NoError(t, err)

	routes := TLSRoutes(state)
	require.Len(t, routes, 1)
	require.Equal(t, metav1.TypeMeta{
		APIVersion: "gateway.networking.k8s.io/v1",
		Kind:       "TLSRoute",
	}, routes[0].TypeMeta)
	require.Equal(t, []gatewayv1.Hostname{"proxy.example.com"}, routes[0].Spec.Hostnames)
}
