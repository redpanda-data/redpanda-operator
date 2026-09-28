// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_tcproute.go.tpl
package chart

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// tcpRouteListener is an enabled `type: tcproute` external listener of any API.
type tcpRouteListener struct {
	Tag         string
	Name        string
	Port        int32
	NetworkPort int32
	Base        int32
	ParentRefs  []gatewayv1.ParentReference
}

// tcpRoutePort is one route of a tcproute listener: the bootstrap route, or one
// per broker. Section names the route's ListenerSet entry.
type tcpRoutePort struct {
	Name        string
	Section     string
	NetworkPort int32
	Backend     string
}

// tcpRouteListeners flattens the enabled tcproute listeners of every API, in
// render order.
func tcpRouteListeners(state *RenderState) []tcpRouteListener {
	defaults := state.Values.External.Gateway.ParentRefs
	var out []tcpRouteListener
	for name, l := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			out = append(out, tcpRouteListener{Tag: "kafka", Name: name, Port: l.Port, NetworkPort: ptr.Deref(l.NetworkPort, 0), Base: ptr.Deref(l.BrokerNetworkPortBase, 0), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			out = append(out, tcpRouteListener{Tag: "http", Name: name, Port: l.Port, NetworkPort: ptr.Deref(l.NetworkPort, 0), Base: ptr.Deref(l.BrokerNetworkPortBase, 0), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			out = append(out, tcpRouteListener{Tag: "admin", Name: name, Port: l.Port, NetworkPort: ptr.Deref(l.NetworkPort, 0), Base: ptr.Deref(l.BrokerNetworkPortBase, 0), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			out = append(out, tcpRouteListener{Tag: "schema", Name: name, Port: l.Port, NetworkPort: ptr.Deref(l.NetworkPort, 0), Base: ptr.Deref(l.BrokerNetworkPortBase, 0), ParentRefs: l.GatewayParentRefs(defaults)})
		}
	}
	return out
}

// tcpRoutePorts returns the bootstrap route and, when base is set, one route
// per broker (validateGatewayListeners guarantees base for multi-broker Kafka).
// Broker i attaches to Gateway port base+i.
func tcpRoutePorts(state *RenderState, pods []string, l tcpRouteListener) []tcpRoutePort {
	fullname := Fullname(state)
	section := fmt.Sprintf("%s-%s-bootstrap", l.Tag, l.Name)
	ports := []tcpRoutePort{
		{Name: fmt.Sprintf("%s-%s", fullname, section), Section: section, NetworkPort: l.NetworkPort, Backend: fmt.Sprintf("%s-gateway-bootstrap", fullname)},
	}
	if l.Base == 0 {
		return ports
	}
	for i, podname := range pods {
		section := fmt.Sprintf("%s-%s-%d", l.Tag, l.Name, i)
		ports = append(ports, tcpRoutePort{Name: fmt.Sprintf("%s-%s", fullname, section), Section: section, NetworkPort: l.Base + int32(i), Backend: gatewayBrokerServiceName(podname)})
	}
	return ports
}

// TCPRoutes returns Gateway API TCPRoutes for `type: tcproute` listeners: a
// bootstrap route plus one per broker, each on its own Gateway listener port.
// The backends are the same Services the TLSRoutes use.
func TCPRoutes(state *RenderState) []*gatewayv1.TCPRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	listenerSet := state.Values.External.Gateway.IsListenerSetEnabled()
	pods := gatewayPodNames(state)

	var routes []*gatewayv1.TCPRoute
	for _, l := range tcpRouteListeners(state) {
		for _, p := range tcpRoutePorts(state, pods, l) {
			parentRefs := tcpParentRefs(l.ParentRefs, p.NetworkPort)
			if listenerSet {
				parentRefs = listenerSetParentRefs(state, l.ParentRefs, p.Section)
			}
			routes = append(routes, tcpRoute(state, p.Name, parentRefs, p.Backend, l.Port))
		}
	}
	return routes
}

// ListenerSets returns, when external.gateway.listenerSet is enabled, one
// ListenerSet per parent Gateway holding a TCP listener for every tcproute
// route on it, so scaling adds and removes Gateway ports with the routes.
func ListenerSets(state *RenderState) []*gatewayv1.ListenerSet {
	if !state.Values.External.IsGatewayEnabled() || !state.Values.External.Gateway.IsListenerSetEnabled() {
		return nil
	}

	pods := gatewayPodNames(state)
	listeners := tcpRouteListeners(state)

	// Distinct parent Gateways, in first-use order.
	seen := map[string]bool{}
	var gateways []gatewayv1.ParentReference
	for _, l := range listeners {
		for _, ref := range l.ParentRefs {
			key := listenerSetName(state, ref)
			if !helmette.HasKey(seen, key) {
				seen[key] = true
				gateways = append(gateways, ref)
			}
		}
	}

	var sets []*gatewayv1.ListenerSet
	for _, gw := range gateways {
		name := listenerSetName(state, gw)
		var entries []gatewayv1.ListenerEntry
		for _, l := range listeners {
			onGateway := false
			for _, ref := range l.ParentRefs {
				if listenerSetName(state, ref) == name {
					onGateway = true
				}
			}
			if !onGateway {
				continue
			}
			for _, p := range tcpRoutePorts(state, pods, l) {
				entries = append(entries, gatewayv1.ListenerEntry{
					Name:     gatewayv1.SectionName(p.Section),
					Port:     gatewayv1.PortNumber(p.NetworkPort),
					Protocol: gatewayv1.ProtocolType("TCP"),
					AllowedRoutes: &gatewayv1.AllowedRoutes{
						Namespaces: &gatewayv1.RouteNamespaces{From: ptr.To(gatewayv1.FromNamespaces("Same"))},
						Kinds:      []gatewayv1.RouteGroupKind{{Kind: gatewayv1.Kind("TCPRoute")}},
					},
				})
			}
		}
		sets = append(sets, &gatewayv1.ListenerSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "ListenerSet",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   state.Release.Namespace,
				Labels:      FullLabels(state),
				Annotations: FullAnnotations(state),
			},
			Spec: gatewayv1.ListenerSetSpec{
				ParentRef: gatewayv1.ParentGatewayReference{
					Group:     gw.Group,
					Kind:      gw.Kind,
					Name:      gw.Name,
					Namespace: ptr.To(ptr.Deref(gw.Namespace, gatewayv1.Namespace(state.Release.Namespace))),
				},
				Listeners: entries,
			},
		})
	}
	return sets
}

// listenerSetName names the release's ListenerSet on a parent Gateway. The
// Gateway namespace is part of it so same-named Gateways never collide.
func listenerSetName(state *RenderState, ref gatewayv1.ParentReference) string {
	return fmt.Sprintf("%s-%s-%s", Fullname(state), ptr.Deref(ref.Namespace, gatewayv1.Namespace(state.Release.Namespace)), ref.Name)
}

func tcpRoute(state *RenderState, name string, parentRefs []gatewayv1.ParentReference, backend string, port int32) *gatewayv1.TCPRoute {
	return &gatewayv1.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "TCPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Spec: gatewayv1.TCPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: []gatewayv1.TCPRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(backend),
								Port: ptr.To(gatewayv1.PortNumber(port)),
							},
						},
					},
				},
			},
		},
	}
}

// tcpParentRefs pins every parent reference to one Gateway listener port. An
// unpinned TCPRoute attaches to every TCP listener on the Gateway, so the port
// is overwritten and sectionName dropped (one section name can't select N+1
// listeners). References are rebuilt, not copied, so routes never share them.
func tcpParentRefs(parentRefs []gatewayv1.ParentReference, networkPort int32) []gatewayv1.ParentReference {
	out := []gatewayv1.ParentReference{}
	for _, ref := range parentRefs {
		out = append(out, gatewayv1.ParentReference{
			Group:     ref.Group,
			Kind:      ref.Kind,
			Namespace: ref.Namespace,
			Name:      ref.Name,
			Port:      ptr.To(gatewayv1.PortNumber(networkPort)),
		})
	}
	return out
}

// listenerSetParentRefs attaches a route to its entry in the release's
// ListenerSet on each parent Gateway.
func listenerSetParentRefs(state *RenderState, parentRefs []gatewayv1.ParentReference, section string) []gatewayv1.ParentReference {
	out := []gatewayv1.ParentReference{}
	for _, ref := range parentRefs {
		out = append(out, gatewayv1.ParentReference{
			Group:       ptr.To(gatewayv1.Group("gateway.networking.k8s.io")),
			Kind:        ptr.To(gatewayv1.Kind("ListenerSet")),
			Name:        gatewayv1.ObjectName(listenerSetName(state, ref)),
			SectionName: ptr.To(gatewayv1.SectionName(section)),
		})
	}
	return out
}
