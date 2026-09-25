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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
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

// TestValidateGatewayListener tests the routing policy of each API. Kafka
// requires a hostname for each broker if there is more than one broker. HTTP,
// Admin and Schema Registry can use only the bootstrap hostname.
//
// A nil gateway means that the listener does not use Gateway API. A gateway
// without BrokerHosts means that the listener has no hostTemplate.
func TestValidateGatewayListener(t *testing.T) {
	// gatewayConfigured=true throughout unless stated otherwise.
	// host is required for every gateway listener, regardless of protocol.
	for _, tag := range []redpanda.APIKind{redpanda.KafkaAPI, redpanda.HTTPAPI, redpanda.AdminAPI, redpanda.SchemaRegistryAPI} {
		require.PanicsWithValue(t,
			"external gateway listener "+string(tag)+"/default requires `host` (the bootstrap SNI hostname) when type: tlsroute",
			func() {
				validateGatewayListener(tag, "default", &redpanda.GatewayRoute{}, true /*exposed*/, true /*gatewayConfigured*/, 1, tag == redpanda.KafkaAPI)
			},
			"%s: missing host must fail", tag,
		)
	}

	// A tlsroute listener without a global gateway must fail. It must not use a
	// NodePort or LoadBalancer Service.
	require.PanicsWithValue(t,
		"external listener kafka/default sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs",
		func() {
			validateGatewayListener("kafka", "default", &redpanda.GatewayRoute{Host: "redpanda.example.com"}, true /*exposed*/, false /*gatewayConfigured*/, 1, true)
		},
	)

	require.PanicsWithValue(t,
		"external gateway listener kafka/default requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname",
		func() {
			validateGatewayListener("kafka", "default", &redpanda.GatewayRoute{Host: "redpanda.example.com"}, true, true, 3 /*replicas*/, true)
		},
	)

	require.NotPanics(t, func() {
		// Kafka, one broker, no broker hostnames.
		validateGatewayListener("kafka", "default", &redpanda.GatewayRoute{Host: "redpanda.example.com"}, true, true, 1, true)
		// Kafka, three brokers, with broker hostnames.
		validateGatewayListener("kafka", "default", &redpanda.GatewayRoute{
			Host:        "redpanda.example.com",
			BrokerHosts: []string{"b-0.example.com", "b-1.example.com", "b-2.example.com"},
		}, true, true, 3, true)
		// HTTP, Admin and Schema Registry, three brokers, no broker hostnames.
		validateGatewayListener("http", "default", &redpanda.GatewayRoute{Host: "proxy.example.com"}, true, true, 3, false)
		validateGatewayListener("admin", "default", &redpanda.GatewayRoute{Host: "admin.example.com"}, true, true, 3, false)
		validateGatewayListener("schema", "default", &redpanda.GatewayRoute{Host: "sr.example.com"}, true, true, 3, false)
		// Validation skips a listener that does not use Gateway API or is not
		// exposed. The global gateway has no effect.
		validateGatewayListener("kafka", "default", nil, true, false, 3, true)
		validateGatewayListener("kafka", "default", &redpanda.GatewayRoute{}, false /*exposed*/, false, 3, true)
	})
}

func TestTLSRoutesForListener(t *testing.T) {
	parentRefs := []gatewayv1.ParentReference{{Name: "shared-gateway"}}
	pods := []string{"redpanda-0", "redpanda-1"}
	labels := map[string]string{"app": "redpanda"}
	annotations := map[string]string{"my.co/team": "platform"}

	// No broker hostnames, thus only the bootstrap route.
	routes := tlsRoutesForListener(
		"redpanda", "default", labels, annotations, parentRefs, pods, redpanda.HTTPAPI,
		redpanda.Listener{
			Name:    "default",
			Port:    8082,
			Gateway: &redpanda.GatewayRoute{Host: "proxy.example.com"},
		},
	)

	require.Len(t, routes, 1)
	require.Equal(t, metav1.TypeMeta{
		APIVersion: "gateway.networking.k8s.io/v1",
		Kind:       "TLSRoute",
	}, routes[0].TypeMeta)
	require.Equal(t, "redpanda-http-default-bootstrap", routes[0].Name)
	require.Equal(t, annotations, routes[0].Annotations)
	require.Equal(t, []gatewayv1.Hostname{"proxy.example.com"}, routes[0].Spec.Hostnames)

	// The broker hostnames and the pods use the same global ordinal. Thus, each
	// SNI name has the backend Service of its broker.
	routes = tlsRoutesForListener(
		"redpanda", "default", labels, annotations, parentRefs, pods, redpanda.KafkaAPI,
		redpanda.Listener{
			Name: "default",
			Port: 9094,
			Gateway: &redpanda.GatewayRoute{
				Host:        "redpanda.example.com",
				BrokerHosts: []string{"b-0.example.com", "b-1.example.com"},
			},
		},
	)

	require.Len(t, routes, 3)
	require.Equal(t, []gatewayv1.Hostname{"redpanda.example.com"}, routes[0].Spec.Hostnames)
	require.Equal(t, "redpanda-kafka-default-0", routes[1].Name)
	require.Equal(t, []gatewayv1.Hostname{"b-0.example.com"}, routes[1].Spec.Hostnames)
	require.Equal(t, gatewayv1.ObjectName("gw-redpanda-0"), routes[1].Spec.Rules[0].BackendRefs[0].Name)
	require.Equal(t, "redpanda-kafka-default-1", routes[2].Name)
	require.Equal(t, []gatewayv1.Hostname{"b-1.example.com"}, routes[2].Spec.Hostnames)
	require.Equal(t, gatewayv1.ObjectName("gw-redpanda-1"), routes[2].Spec.Rules[0].BackendRefs[0].Name)
}

// TestValidateGatewayListenersReadsValues makes sure that validation finds a
// gateway listener with `port: 0`. resolveListeners removes this listener.
func TestValidateGatewayListenersReadsValues(t *testing.T) {
	state := func(port int32) *RenderState {
		return &RenderState{
			Release: &helmette.Release{Name: "redpanda", Namespace: "default"},
			Values: Values{
				External: ExternalConfig{
					Enabled: true,
					Gateway: &GatewayConfig{
						Enabled:    true,
						ParentRefs: []gatewayv1.ParentReference{{Name: "shared-gateway"}},
					},
				},
				Listeners: Listeners{
					Kafka: ListenerConfig[KafkaAuthenticationMethod]{
						External: map[string]ExternalListener[KafkaAuthenticationMethod]{
							"default": {
								Port: port,
								Type: ptr.To(ExternalListenerTypeTLSRoute),
							},
						},
					},
				},
			},
		}
	}

	for _, port := range []int32{9094, 0} {
		require.PanicsWithValue(t,
			"external gateway listener kafka/default requires `host` (the bootstrap SNI hostname) when type: tlsroute",
			func() { validateGatewayListeners(state(port)) },
			"port %d", port,
		)
	}
}
