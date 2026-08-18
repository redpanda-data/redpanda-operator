// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_tlsroute.go.tpl
package redpanda

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TLSRoutes returns Gateway API TLSRoute resources for external access.
func TLSRoutes(state *RenderState) []*gatewayv1.TLSRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	gw := state.Values.External.Gateway
	parentRefs := toTLSRouteParentRefs(gw.ParentRefs)
	labels := FullLabels(state)
	fullname := Fullname(state)

	pods := gatewayPodNames(state)

	var routes []*gatewayv1.TLSRoute

	for name, listener := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		rs := tlsRoutesForListener(fullname, state.Release.Namespace, labels, parentRefs, pods, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), name, "kafka", listener.Port)
		routes = append(routes, rs...)
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		rs := tlsRoutesForListener(fullname, state.Release.Namespace, labels, parentRefs, pods, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), name, "http", listener.Port)
		routes = append(routes, rs...)
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		rs := tlsRoutesForListener(fullname, state.Release.Namespace, labels, parentRefs, pods, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), name, "admin", listener.Port)
		routes = append(routes, rs...)
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		rs := tlsRoutesForListener(fullname, state.Release.Namespace, labels, parentRefs, pods, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), name, "schema", listener.Port)
		routes = append(routes, rs...)
	}

	return routes
}

func tlsRoutesForListener(fullname string, namespace string, labels map[string]string, parentRefs []gatewayv1.ParentReference, pods []string, host string, hostTemplate string, name string, listenerTag string, port int32) []*gatewayv1.TLSRoute {
	// Invariants (host present; kafka multi-broker requires hostTemplate) are
	// enforced upfront by validateGatewayListeners so misconfigurations surface
	// as a single clear error before any rendering. By the time we get here the
	// config is valid; a non-kafka listener without hostTemplate intentionally
	// emits only the bootstrap route (see validateGatewayListeners for why).
	var routes []*gatewayv1.TLSRoute

	bootstrapSvcName := fmt.Sprintf("%s-gateway-bootstrap", fullname)

	bootstrap := &gatewayv1.TLSRoute{
		// NB: TypeMeta is set explicitly because the upstream type marshals an
		// empty TypeMeta; helm needs apiVersion/kind in the rendered manifest.
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s-bootstrap", fullname, listenerTag, name),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(host)},
			Rules: []gatewayv1.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(bootstrapSvcName),
								Port: ptr.To(gatewayv1.PortNumber(port)),
							},
						},
					},
				},
			},
		},
	}
	routes = append(routes, bootstrap)

	if hostTemplate == "" {
		return routes
	}

	for i, podname := range pods {
		brokerHost := renderBrokerHost(hostTemplate, i, podname)
		brokerSvcName := gatewayBrokerServiceName(podname)

		route := &gatewayv1.TLSRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "TLSRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%s-%d", fullname, listenerTag, name, i),
				Namespace: namespace,
				Labels:    labels,
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
									Port: ptr.To(gatewayv1.PortNumber(port)),
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

// toTLSRouteParentRefs maps the chart's values type onto the upstream one. It's
// field by field rather than a struct conversion because upstream gives each
// field its own named string type (Group, Kind, ObjectName, ...), so
// GatewayParentRef isn't convertible to ParentReference.
func toTLSRouteParentRefs(refs []GatewayParentRef) []gatewayv1.ParentReference {
	var parentRefs []gatewayv1.ParentReference
	for _, ref := range refs {
		converted := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(ref.Name),
		}
		if ref.Group != nil {
			converted.Group = ptr.To(gatewayv1.Group(*ref.Group))
		}
		if ref.Kind != nil {
			converted.Kind = ptr.To(gatewayv1.Kind(*ref.Kind))
		}
		if ref.Namespace != nil {
			converted.Namespace = ptr.To(gatewayv1.Namespace(*ref.Namespace))
		}
		if ref.SectionName != nil {
			converted.SectionName = ptr.To(gatewayv1.SectionName(*ref.SectionName))
		}
		parentRefs = append(parentRefs, converted)
	}
	return parentRefs
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
	// NB: this does NOT early-return on a disabled global gateway. A listener
	// can set `type: tlsroute` while the global external.gateway block is
	// off/absent; that listener is already excluded from the conventional
	// NodePort/LoadBalancer Service (the per-listener type is authoritative
	// there), so without this validation it would render no TLSRoute and no
	// Service — silently unreachable, or worse if the skip were ever relaxed.
	// We therefore fail render whenever an enabled gateway listener is present
	// but the global gateway config can't actually back it.
	replicas := len(gatewayPodNames(state))
	gatewayConfigured := state.Values.External.IsGatewayEnabled()

	for name, l := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		validateGatewayListener("kafka", name, l.IsGatewayListener(), ptr.Deref(l.Enabled, state.Values.External.Enabled), gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, true)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		validateGatewayListener("http", name, l.IsGatewayListener(), ptr.Deref(l.Enabled, state.Values.External.Enabled), gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		validateGatewayListener("admin", name, l.IsGatewayListener(), ptr.Deref(l.Enabled, state.Values.External.Enabled), gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		validateGatewayListener("schema", name, l.IsGatewayListener(), ptr.Deref(l.Enabled, state.Values.External.Enabled), gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
	}
}

func validateGatewayListener(tag string, name string, isGateway bool, enabled bool, gatewayConfigured bool, host string, hostTemplate string, replicas int, requirePerBroker bool) {
	if !enabled || !isGateway {
		return
	}
	// A listener opted into gateway mode but the global gateway can't back it
	// (external.gateway missing/disabled, or no parentRefs). Fail closed rather
	// than silently dropping the listener or letting it leak onto a Service.
	if !gatewayConfigured {
		panic(fmt.Sprintf("external listener %s/%s sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs", tag, name))
	}
	if host == "" {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute", tag, name))
	}
	if requirePerBroker && replicas > 1 && hostTemplate == "" {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname", tag, name))
	}
}
