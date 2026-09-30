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

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// tlsRouteListener is an enabled `type: tlsroute` external listener of any API.
type tlsRouteListener struct {
	Tag          string
	Name         string
	Port         int32
	Host         string
	HostTemplate string
	ParentRefs   []gatewayv1.ParentReference
}

// tlsRouteHost is one TLSRoute of a tlsroute listener: the bootstrap route, or
// one per broker. Section names the route's ListenerSet entry.
type tlsRouteHost struct {
	Name     string
	Section  string
	Hostname string
	Backend  string
}

// tlsRouteListeners flattens the enabled tlsroute listeners of every API, in
// render order.
func tlsRouteListeners(state *RenderState) []tlsRouteListener {
	var defaults []gatewayv1.ParentReference
	if state.Values.External.Gateway != nil {
		defaults = state.Values.External.Gateway.ParentRefs
	}
	var out []tlsRouteListener
	for name, l := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTLSRouteListener() {
			out = append(out, tlsRouteListener{Tag: "kafka", Name: name, Port: l.Port, Host: ptr.Deref(l.Host, ""), HostTemplate: ptr.Deref(l.HostTemplate, ""), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTLSRouteListener() {
			out = append(out, tlsRouteListener{Tag: "http", Name: name, Port: l.Port, Host: ptr.Deref(l.Host, ""), HostTemplate: ptr.Deref(l.HostTemplate, ""), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTLSRouteListener() {
			out = append(out, tlsRouteListener{Tag: "admin", Name: name, Port: l.Port, Host: ptr.Deref(l.Host, ""), HostTemplate: ptr.Deref(l.HostTemplate, ""), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTLSRouteListener() {
			out = append(out, tlsRouteListener{Tag: "schema", Name: name, Port: l.Port, Host: ptr.Deref(l.Host, ""), HostTemplate: ptr.Deref(l.HostTemplate, ""), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	return out
}

// tlsRouteHosts returns the bootstrap route and, when hostTemplate is set, one
// route per broker. validateGatewayListeners guarantees host, and hostTemplate
// for multi-broker Kafka; other APIs may be bootstrap-only.
func tlsRouteHosts(state *RenderState, pods []string, l tlsRouteListener) []tlsRouteHost {
	fullname := Fullname(state)
	section := fmt.Sprintf("%s-%s-bootstrap", l.Tag, l.Name)
	hosts := []tlsRouteHost{
		{Name: fmt.Sprintf("%s-%s", fullname, section), Section: section, Hostname: l.Host, Backend: fmt.Sprintf("%s-gateway-bootstrap", fullname)},
	}
	if l.HostTemplate == "" {
		return hosts
	}
	for i, podname := range pods {
		section := fmt.Sprintf("%s-%s-%d", l.Tag, l.Name, i)
		hosts = append(hosts, tlsRouteHost{Name: fmt.Sprintf("%s-%s", fullname, section), Section: section, Hostname: renderBrokerHost(l.HostTemplate, i, podname), Backend: gatewayBrokerServiceName(podname)})
	}
	return hosts
}

// TLSRoutes returns Gateway API TLSRoutes for `type: tlsroute` listeners: a
// bootstrap route plus one per broker, routed by SNI hostname.
func TLSRoutes(state *RenderState) []*gatewayv1.TLSRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	gw := state.Values.External.Gateway
	pods := gatewayPodNames(state)

	var routes []*gatewayv1.TLSRoute
	for _, l := range tlsRouteListeners(state) {
		for _, h := range tlsRouteHosts(state, pods, l) {
			parentRefs := l.ParentRefs
			if gw.IsListenerSetEnabled() {
				// As for TCPRoutes: attached to both, the move to a ListenerSet
				// is graceful (see the ListenerSets doc comment).
				lsRefs := listenerSetParentRefs(state, l.ParentRefs, h.Section)
				if gw.AttachesRoutesToGateway() {
					parentRefs = append(append([]gatewayv1.ParentReference{}, l.ParentRefs...), lsRefs...)
				} else {
					parentRefs = lsRefs
				}
			}
			routes = append(routes, tlsRoute(state, h, parentRefs, l.Port))
		}
	}
	return routes
}

func tlsRoute(state *RenderState, h tlsRouteHost, parentRefs []gatewayv1.ParentReference, port int32) *gatewayv1.TLSRoute {
	return &gatewayv1.TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        h.Name,
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(h.Hostname)},
			Rules: []gatewayv1.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(h.Backend),
								Port: ptr.To(gatewayv1.PortNumber(port)),
							},
						},
					},
				},
			},
		},
	}
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
	pods := gatewayPodNames(state)
	replicas := len(pods)
	gatewayConfigured := state.Values.External.IsGatewayEnabled()
	var defaultRefs []gatewayv1.ParentReference
	maxPorts := int32(maxGatewayListeners)
	if state.Values.External.Gateway != nil {
		defaultRefs = state.Values.External.Gateway.ParentRefs
		maxPorts = state.Values.External.Gateway.GatewayMaxPorts()
	}
	if maxPorts < 1 || maxPorts > maxGatewayListeners {
		panic(fmt.Sprintf("external.gateway.maxPorts must be between 1 and %d, got %d", maxGatewayListeners, maxPorts))
	}
	// Gateway listener ports claimed by tcproute listeners, per Gateway; every
	// TCPRoute needs its own (see validateTCPRouteListener).
	claimed := map[string]map[string]string{}

	for name, l := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		enabled := ptr.Deref(l.Enabled, state.Values.External.Enabled)
		validateGatewayListener("kafka", name, ptr.Deref(l.Type, ""), enabled, gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, true)
		validateTCPRouteListener(claimed, maxPorts, l.GatewayParentRefs(defaultRefs), state.Release.Namespace, "kafka", name, enabled && l.IsTCPRouteListener(), ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0), replicas, true)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		enabled := ptr.Deref(l.Enabled, state.Values.External.Enabled)
		validateGatewayListener("http", name, ptr.Deref(l.Type, ""), enabled, gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
		validateTCPRouteListener(claimed, maxPorts, l.GatewayParentRefs(defaultRefs), state.Release.Namespace, "http", name, enabled && l.IsTCPRouteListener(), ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0), replicas, false)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		enabled := ptr.Deref(l.Enabled, state.Values.External.Enabled)
		validateGatewayListener("admin", name, ptr.Deref(l.Type, ""), enabled, gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
		validateTCPRouteListener(claimed, maxPorts, l.GatewayParentRefs(defaultRefs), state.Release.Namespace, "admin", name, enabled && l.IsTCPRouteListener(), ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0), replicas, false)
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		enabled := ptr.Deref(l.Enabled, state.Values.External.Enabled)
		validateGatewayListener("schema", name, ptr.Deref(l.Type, ""), enabled, gatewayConfigured, ptr.Deref(l.Host, ""), ptr.Deref(l.HostTemplate, ""), replicas, false)
		validateTCPRouteListener(claimed, maxPorts, l.GatewayParentRefs(defaultRefs), state.Release.Namespace, "schema", name, enabled && l.IsTCPRouteListener(), ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0), replicas, false)
	}

	if gatewayConfigured && state.Values.External.Gateway.IsListenerSetEnabled() {
		validateTLSRouteListenerSet(state, claimed, maxPorts, pods)
	}
}

// validateTLSRouteListenerSet checks the TLS-passthrough entries ListenerSet
// mode renders for tlsroute hostnames: they share the Gateway's TLS port, which
// no tcproute route may use; a hostname can back only one entry per Gateway;
// and each ListenerSet holds at most maxGatewayListeners entries.
func validateTLSRouteListenerSet(state *RenderState, claimed map[string]map[string]string, maxPorts int32, pods []string) {
	tlsPort := state.Values.External.Gateway.GatewayAdvertisedPort()
	hosts := map[string]map[string]string{}
	for _, l := range tlsRouteListeners(state) {
		for _, ref := range l.ParentRefs {
			gw := gatewayKey(ref, state.Release.Namespace)
			claimTLSPort(claimed, maxPorts, gw, l.Tag, l.Name, tlsPort)
			if !helmette.HasKey(hosts, gw) {
				hosts[gw] = map[string]string{}
			}
			gwHosts := hosts[gw]
			for _, h := range tlsRouteHosts(state, pods, l) {
				if helmette.HasKey(gwHosts, h.Hostname) {
					panic(fmt.Sprintf("external gateway listener %s/%s: hostname %s is already used by %s on %s; each ListenerSet TLS entry needs its own hostname", l.Tag, l.Name, h.Hostname, gwHosts[h.Hostname], gw))
				}
				gwHosts[h.Hostname] = fmt.Sprintf("%s/%s", l.Tag, l.Name)
			}
		}
	}
	// One entry per tcproute port, plus one per TLS hostname (their shared TLS
	// port is claimed once).
	for gw, ports := range helmette.SortedMap(claimed) {
		entries := len(ports)
		if helmette.HasKey(hosts, gw) {
			entries = entries - 1 + len(hosts[gw])
		}
		if entries > maxGatewayListeners {
			panic(fmt.Sprintf("external.gateway.listenerSet: the ListenerSet for %s would need %d entries, more than the %d a ListenerSet allows; move listeners to another Gateway with per-listener parentRefs", gw, entries, maxGatewayListeners))
		}
	}
}

// gatewayKey identifies a parent Gateway (or ListenerSet) for port accounting.
func gatewayKey(ref gatewayv1.ParentReference, namespace string) string {
	return fmt.Sprintf("%s %s/%s", ptr.Deref(ref.Kind, gatewayv1.Kind("Gateway")), ptr.Deref(ref.Namespace, gatewayv1.Namespace(namespace)), ref.Name)
}

func validateGatewayListener(tag string, name string, listenerType string, enabled bool, gatewayConfigured bool, host string, hostTemplate string, replicas int, requirePerBroker bool) {
	if !enabled || (listenerType != ExternalListenerTypeTLSRoute && listenerType != ExternalListenerTypeTCPRoute) {
		return
	}
	// A listener opted into gateway mode but the global gateway can't back it
	// (external.gateway missing/disabled, or no parentRefs). Fail closed rather
	// than silently dropping the listener or letting it leak onto a Service.
	if !gatewayConfigured {
		panic(fmt.Sprintf("external listener %s/%s sets type: %s but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs", tag, name, listenerType))
	}
	if listenerType == ExternalListenerTypeTCPRoute {
		// TCPRoutes carry no hostname; host is only what the brokers advertise.
		if host == "" {
			panic(fmt.Sprintf("external gateway listener %s/%s requires `host` (the advertised host every broker shares) when type: tcproute", tag, name))
		}
		return
	}
	if host == "" {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute", tag, name))
	}
	if requirePerBroker && replicas > 1 && hostTemplate == "" {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname", tag, name))
	}
}

// validateTCPRouteListener checks the Gateway ports of a tcproute listener. A
// TCPRoute has nothing to match on but its port, and when several routes share
// a Gateway listener only the oldest receives traffic, so every route (bootstrap
// and per broker, across all tcproute listeners) must claim a distinct port.
func validateTCPRouteListener(claimed map[string]map[string]string, maxPorts int32, parentRefs []gatewayv1.ParentReference, namespace string, tag string, name string, active bool, networkPort int32, base int32, replicas int, requirePerBroker bool) {
	if !active {
		return
	}
	if networkPort == 0 {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `networkPort` (the Gateway listener port of the bootstrap TCPRoute) when type: tcproute", tag, name))
	}
	if requirePerBroker && replicas > 1 && base == 0 {
		panic(fmt.Sprintf("external gateway listener %s/%s requires `brokerNetworkPortBase` when replicas > 1: TCPRoutes carry no hostname, so each broker needs its own Gateway port", tag, name))
	}
	for _, ref := range parentRefs {
		gw := gatewayKey(ref, namespace)
		claimNetworkPort(claimed, maxPorts, gw, tag, name, "bootstrap", networkPort)
		if base == 0 {
			continue
		}
		for _, i := range helmette.Until(replicas) {
			claimNetworkPort(claimed, maxPorts, gw, tag, name, fmt.Sprintf("broker %d", i), base+int32(i))
		}
	}
}

// maxGatewayListeners is the Gateway API cap on listeners per Gateway and per
// ListenerSet (maxItems), the default and ceiling for external.gateway.maxPorts.
// Cloud load balancers may cap lower, e.g. 50 on an AWS NLB.
const maxGatewayListeners = 64

// claimTLSPort claims the Gateway's TLS port for tlsroute ListenerSet entries,
// which share it; a tcproute route on that port is a collision.
func claimTLSPort(claimed map[string]map[string]string, maxPorts int32, gw string, tag string, name string, port int32) {
	if !helmette.HasKey(claimed, gw) {
		claimed[gw] = map[string]string{}
	}
	ports := claimed[gw]
	key := fmt.Sprintf("%d", port)
	if helmette.HasKey(ports, key) {
		if !strings.HasPrefix(ports[key], "tlsroute ") {
			panic(fmt.Sprintf("external gateway listener %s/%s: the Gateway TLS port %d (external.gateway.advertisedPort) is already used by %s; every TCPRoute needs its own Gateway listener port", tag, name, port, ports[key]))
		}
		return
	}
	ports[key] = fmt.Sprintf("tlsroute %s/%s", tag, name)
	if int32(len(ports)) > maxPorts {
		panic(fmt.Sprintf("external gateway listener %s/%s: the TLS port needs more than %d ports on %s (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs", tag, name, maxPorts, gw))
	}
}

func claimNetworkPort(claimed map[string]map[string]string, maxPorts int32, gw string, tag string, name string, what string, port int32) {
	if port < 1 || port > 65535 {
		panic(fmt.Sprintf("external gateway listener %s/%s: %s network port %d is outside 1-65535", tag, name, what, port))
	}
	if !helmette.HasKey(claimed, gw) {
		claimed[gw] = map[string]string{}
	}
	ports := claimed[gw]
	key := fmt.Sprintf("%d", port)
	if helmette.HasKey(ports, key) {
		panic(fmt.Sprintf("external gateway listener %s/%s: %s network port %d is already used by %s; every TCPRoute needs its own Gateway listener port", tag, name, what, port, ports[key]))
	}
	ports[key] = fmt.Sprintf("%s/%s %s", tag, name, what)
	if int32(len(ports)) > maxPorts {
		panic(fmt.Sprintf("external gateway listener %s/%s: %s needs more than %d TCPRoute ports on %s (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs", tag, name, what, maxPorts, gw))
	}
}
