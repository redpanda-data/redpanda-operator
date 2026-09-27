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

// TCPRoutes returns Gateway API TCPRoutes for `type: tcproute` listeners: a
// bootstrap route plus one per broker, each pinned to its own Gateway listener
// port. The backends are the same Services the TLSRoutes use.
func TCPRoutes(state *RenderState) []*gatewayv1.TCPRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	gw := state.Values.External.Gateway
	pods := gatewayPodNames(state)

	var routes []*gatewayv1.TCPRoute

	for name, l := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			routes = append(routes, tcpRoutesForListener(state, l.GatewayParentRefs(gw.ParentRefs), pods, "kafka", name, l.Port, ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0))...)
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			routes = append(routes, tcpRoutesForListener(state, l.GatewayParentRefs(gw.ParentRefs), pods, "http", name, l.Port, ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0))...)
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			routes = append(routes, tcpRoutesForListener(state, l.GatewayParentRefs(gw.ParentRefs), pods, "admin", name, l.Port, ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0))...)
		}
	}
	for name, l := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if ptr.Deref(l.Enabled, state.Values.External.Enabled) && l.IsTCPRouteListener() {
			routes = append(routes, tcpRoutesForListener(state, l.GatewayParentRefs(gw.ParentRefs), pods, "schema", name, l.Port, ptr.Deref(l.NetworkPort, 0), ptr.Deref(l.BrokerNetworkPortBase, 0))...)
		}
	}

	return routes
}

// tcpRoutesForListener renders the bootstrap route and, when base is set, one
// route per broker (validateGatewayListeners guarantees base for multi-broker
// Kafka). Broker i attaches to Gateway port base+i.
func tcpRoutesForListener(state *RenderState, parentRefs []gatewayv1.ParentReference, pods []string, tag string, name string, port int32, networkPort int32, base int32) []*gatewayv1.TCPRoute {
	fullname := Fullname(state)
	routes := []*gatewayv1.TCPRoute{
		tcpRoute(state, fmt.Sprintf("%s-%s-%s-bootstrap", fullname, tag, name), parentRefs, networkPort, fmt.Sprintf("%s-gateway-bootstrap", fullname), port),
	}
	if base == 0 {
		return routes
	}
	for i, podname := range pods {
		routes = append(routes, tcpRoute(state, fmt.Sprintf("%s-%s-%s-%d", fullname, tag, name, i), parentRefs, base+int32(i), gatewayBrokerServiceName(podname), port))
	}
	return routes
}

func tcpRoute(state *RenderState, name string, parentRefs []gatewayv1.ParentReference, networkPort int32, backend string, port int32) *gatewayv1.TCPRoute {
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
				ParentRefs: tcpParentRefs(parentRefs, networkPort),
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
