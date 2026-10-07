// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_tlsroute.go.tpl
package chart

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TLSRoutes returns the Gateway API TLSRoutes for the listeners of the
// gateway Services.
func TLSRoutes(state *RenderState, network *redpanda.Network) []*gatewayv1.TLSRoute {
	gateway := network.Service(redpanda.ServiceKindGateway)
	if gateway == nil {
		return nil
	}

	gw := state.Values.External.Gateway
	labels := FullLabels(state)
	annotations := FullAnnotations(state)
	fullname := Fullname(state)

	pods := gatewayPodNames(state)

	var routes []*gatewayv1.TLSRoute

	for _, api := range gateway.Listeners.InOrder([]redpanda.APIKind{redpanda.KafkaAPI, redpanda.HTTPAPI, redpanda.AdminAPI, redpanda.SchemaRegistryAPI}) {
		for _, listener := range api.Listeners() {
			routes = append(routes, tlsRoutesForListener(fullname, state.Release.Namespace, labels, annotations, gw.ParentRefs, pods, api.Kind, listener, network.Route(api.Kind, listener.Name))...)
		}
	}

	return routes
}

func tlsRoutesForListener(fullname string, namespace string, labels map[string]string, annotations map[string]string, parentRefs []gatewayv1.ParentReference, pods []string, kind redpanda.APIKind, listener redpanda.Listener, gateway *redpanda.GatewayRoute) []*gatewayv1.TLSRoute {
	// Invariants (host present; kafka multi-broker requires hostTemplate) are
	// enforced upfront by validateGatewayListeners so misconfigurations surface
	// as a single clear error before any rendering. By the time we get here the
	// config is valid; a non-kafka listener without hostTemplate intentionally
	// emits only the bootstrap route (see validateGatewayListeners for why).
	var routes []*gatewayv1.TLSRoute

	bootstrapSvcName := fmt.Sprintf("%s-gateway-bootstrap", fullname)

	bootstrap := &gatewayv1.TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s-%s-bootstrap", fullname, kind, listener.Name),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(gateway.Host)},
			Rules: []gatewayv1.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(bootstrapSvcName),
								Port: ptr.To(gatewayv1.PortNumber(listener.Port)),
							},
						},
					},
				},
			},
		},
	}
	routes = append(routes, bootstrap)

	if len(gateway.BrokerHosts) == 0 {
		return routes
	}

	for i, podname := range pods {
		brokerHost := gateway.BrokerHosts[i]
		brokerSvcName := gatewayBrokerServiceName(podname)

		route := &gatewayv1.TLSRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "TLSRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-%s-%s-%d", fullname, kind, listener.Name, i),
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: gatewayv1.TLSRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: parentRefs,
				},
				Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(brokerHost)},
				Rules: []gatewayv1.TLSRouteRule{
					{
						BackendRefs: []gatewayv1.BackendRef{
							{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(brokerSvcName),
									Port: ptr.To(gatewayv1.PortNumber(listener.Port)),
								},
							},
						},
					},
				},
			},
		}
		routes = append(routes, route)
	}

	return routes
}

func renderBrokerHost(tmpl string, ordinal int, podName string) string {
	result := strings.ReplaceAll(tmpl, "$POD_ORDINAL", fmt.Sprintf("%d", ordinal))
	result = strings.ReplaceAll(result, "$POD_NAME", podName)
	return result
}

// gatewayPodNames returns the global, ordered list of broker pod names across
// the main StatefulSet and every additional node pool. This order is the single
// source of truth for Gateway API rendering: the per-broker TLSRoutes
// ([TLSRoutes]), the per-broker ClusterIP services ([GatewayServices]), and the
// per-broker advertised SNI hostnames ([advertisedHostJSONGateway]) all index
// into this same slice, so a broker's advertised address is guaranteed to match
// the TLSRoute hostname and backend service that route to it. The slice index
// is the global ordinal substituted for $POD_ORDINAL in hostTemplate.
func gatewayPodNames(state *RenderState) []string {
	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}
	return pods
}

// gatewayBrokerServiceName returns the name of the per-broker ClusterIP service
// that a per-broker TLSRoute targets as its backend. The TLSRoute backendRef and
// the Service must use this same name so the route resolves; centralizing the
// name here keeps them in sync. Service names are RFC 1035 labels, so this fails
// render (rather than emitting a dangling backendRef) if the name would exceed
// the 63-character limit — long fullnameOverride / node-pool suffixes can push it
// over once "gw-" is prepended to an already-long pod name.
func gatewayBrokerServiceName(podName string) string {
	name := fmt.Sprintf("gw-%s", podName)
	if len(name) > 63 {
		panic(fmt.Sprintf("gateway per-broker service name %q exceeds the 63-character RFC 1035 limit for Service names; shorten fullnameOverride/nameOverride or the node-pool suffix so that \"gw-\"+<pod name> fits", name))
	}
	return name
}

// validateGatewayListeners fails render early — with a single clear message —
// when a gateway listener is misconfigured, instead of panicking deep inside
// per-listener rendering. In the operator this surfaces as a clean
// "chart execution failed: ..." error (RenderResources recovers the panic),
// which is the validated-values UX we want.
//
// Per-broker routing policy differs by protocol, by design:
//   - Kafka clients reconnect directly to individual brokers by SNI after
//     metadata discovery, so a multi-broker Kafka gateway listener MUST set
//     hostTemplate — without it, clients could only ever reach the bootstrap
//     route. This is an error.
//   - HTTP (pandaproxy), Admin, and Schema Registry are stateless and
//     load-balanceable across brokers, so the bootstrap route alone is a valid
//     configuration. hostTemplate is optional for them; when omitted, only the
//     bootstrap route is emitted (per-broker routes are added if it is set).
//
// host (the bootstrap SNI name) is required for every gateway listener.
func validateGatewayListeners(state *RenderState) {
	// NB: Do not return early if the global gateway is disabled. A listener
	// can set `type: tlsroute` while external.gateway is disabled or absent.
	// The NodePort and LoadBalancer Services do not include that listener.
	// Without this validation, the listener has no TLSRoute and no Service,
	// and clients cannot connect to it. Thus, the render fails if an enabled
	// gateway listener exists and the global gateway cannot support it.
	//
	// NB: Read the values, not [resolveListeners]. The resolver removes a
	// listener with `port: 0`, but the schema accepts that value. Validation of
	// the resolved listeners does not find this error.
	replicas := len(gatewayPodNames(state))
	gatewayConfigured := state.Values.External.IsGatewayEnabled()

	for kind, listener := range helmette.SortedMap(map[redpanda.APIKind]ListenerConfig[string]{
		redpanda.AdminAPI:          state.Values.Listeners.Admin.AsString(),
		redpanda.KafkaAPI:          state.Values.Listeners.Kafka.AsString(),
		redpanda.HTTPAPI:           state.Values.Listeners.HTTP.AsString(),
		redpanda.SchemaRegistryAPI: state.Values.Listeners.SchemaRegistry.AsString(),
	}) {
		// NB: Only kafka needs a hostname for each broker, because kafka clients
		// connect to each broker by SNI.
		requirePerBroker := kind == redpanda.KafkaAPI

		for name, external := range helmette.SortedMap(listener.External) {
			exposed := ptr.Deref(external.Enabled, state.Values.External.Enabled)
			validateGatewayListener(kind, name, resolveGateway(state, external), exposed, gatewayConfigured, replicas, requirePerBroker)
		}
	}
}

func validateGatewayListener(tag redpanda.APIKind, name string, gateway *redpanda.GatewayRoute, exposed bool, gatewayConfigured bool, replicas int, requirePerBroker bool) {
	if !exposed || gateway == nil {
		return
	}
	// A listener opted into gateway mode but the global gateway can't back it
	// (external.gateway missing/disabled, or no parentRefs). Fail closed rather
	// than silently dropping the listener or letting it leak onto a Service.
	if !gatewayConfigured {
		panic(fmt.Sprintf("external listener %s/%s sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs", tag, name))
	}
	if gateway.Host == "" {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute", tag, name))
	}
	if requirePerBroker && replicas > 1 && len(gateway.BrokerHosts) == 0 {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname", tag, name))
	}
}
