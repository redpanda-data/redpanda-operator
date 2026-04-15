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
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TLSRoutes returns Gateway API TLSRoute resources for external access.
//
// For each enabled external listener (across Kafka, HTTP, Admin,
// SchemaRegistry), this creates:
//   - A bootstrap TLSRoute pointing to the bootstrap ClusterIP service
//   - Per-broker TLSRoutes pointing to per-broker ClusterIP services
//
// SNI hostnames on each TLSRoute enable the Gateway to route traffic to the
// correct broker based on the client-requested hostname.
func TLSRoutes(state *RenderState) []*gatewayv1alpha2.TLSRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	gw := state.Values.External.Gateway
	parentRefs := toGatewayParentRefs(gw.ParentRefs)
	labels := FullLabels(state)
	fullname := Fullname(state)

	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	var routes []*gatewayv1alpha2.TLSRoute

	// Kafka listeners
	for name, listener := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) {
			continue
		}
		rs := tlsRoutesForListener(tlsRouteParams{
			fullname:     fullname,
			namespace:    state.Release.Namespace,
			labels:       labels,
			parentRefs:   parentRefs,
			pods:         pods,
			host:         ptr.Deref(listener.Host, ""),
			hostTemplate: ptr.Deref(listener.HostTemplate, ""),
			name:         name,
			listenerTag:  "kafka",
			port:         listener.Port,
		})
		routes = append(routes, rs...)
	}

	// HTTP/Pandaproxy listeners
	for name, listener := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) {
			continue
		}
		rs := tlsRoutesForListener(tlsRouteParams{
			fullname:     fullname,
			namespace:    state.Release.Namespace,
			labels:       labels,
			parentRefs:   parentRefs,
			pods:         pods,
			host:         ptr.Deref(listener.Host, ""),
			hostTemplate: ptr.Deref(listener.HostTemplate, ""),
			name:         name,
			listenerTag:  "http",
			port:         listener.Port,
		})
		routes = append(routes, rs...)
	}

	// Admin listeners
	for name, listener := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) {
			continue
		}
		rs := tlsRoutesForListener(tlsRouteParams{
			fullname:     fullname,
			namespace:    state.Release.Namespace,
			labels:       labels,
			parentRefs:   parentRefs,
			pods:         pods,
			host:         ptr.Deref(listener.Host, ""),
			hostTemplate: ptr.Deref(listener.HostTemplate, ""),
			name:         name,
			listenerTag:  "admin",
			port:         listener.Port,
		})
		routes = append(routes, rs...)
	}

	// Schema Registry listeners
	for name, listener := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) {
			continue
		}
		rs := tlsRoutesForListener(tlsRouteParams{
			fullname:     fullname,
			namespace:    state.Release.Namespace,
			labels:       labels,
			parentRefs:   parentRefs,
			pods:         pods,
			host:         ptr.Deref(listener.Host, ""),
			hostTemplate: ptr.Deref(listener.HostTemplate, ""),
			name:         name,
			listenerTag:  "schema",
			port:         listener.Port,
		})
		routes = append(routes, rs...)
	}

	return routes
}

type tlsRouteParams struct {
	fullname     string
	namespace    string
	labels       map[string]string
	parentRefs   []gatewayv1.ParentReference
	pods         []string
	host         string
	hostTemplate string
	name         string
	listenerTag  string
	port         int32
}

func tlsRoutesForListener(p tlsRouteParams) []*gatewayv1alpha2.TLSRoute {
	var routes []*gatewayv1alpha2.TLSRoute

	if p.host == "" {
		return nil
	}

	bootstrapSvcName := fmt.Sprintf("%s-gateway-bootstrap", p.fullname)

	// Bootstrap TLSRoute: routes initial client connections to any broker.
	bootstrap := &gatewayv1alpha2.TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s-bootstrap", p.fullname, p.listenerTag, p.name),
			Namespace: p.namespace,
			Labels:    p.labels,
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: p.parentRefs,
			},
			Hostnames: []gatewayv1.Hostname{
				gatewayv1.Hostname(p.host),
			},
			Rules: []gatewayv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(bootstrapSvcName),
								Port: portPtr(p.port),
							},
						},
					},
				},
			},
		},
	}
	routes = append(routes, bootstrap)

	// Per-broker TLSRoutes: each broker gets a unique SNI hostname so the
	// Gateway can route directly to the correct pod.
	if p.hostTemplate == "" {
		return routes
	}

	for i, podname := range p.pods {
		brokerHost := renderBrokerHost(p.hostTemplate, i, podname)
		brokerSvcName := fmt.Sprintf("gw-%s", podname)

		route := &gatewayv1alpha2.TLSRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Kind:       "TLSRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%s-%d", p.fullname, p.listenerTag, p.name, i),
				Namespace: p.namespace,
				Labels:    p.labels,
			},
			Spec: gatewayv1alpha2.TLSRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: p.parentRefs,
				},
				Hostnames: []gatewayv1.Hostname{
					gatewayv1.Hostname(brokerHost),
				},
				Rules: []gatewayv1alpha2.TLSRouteRule{
					{
						BackendRefs: []gatewayv1.BackendRef{
							{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(brokerSvcName),
									Port: portPtr(p.port),
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

// toGatewayParentRefs converts the chart's GatewayParentRef values into the
// Gateway API ParentReference type.
func toGatewayParentRefs(refs []GatewayParentRef) []gatewayv1.ParentReference {
	var parentRefs []gatewayv1.ParentReference
	for _, ref := range refs {
		pr := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(ref.Name),
		}
		if ref.Group != nil {
			g := gatewayv1.Group(*ref.Group)
			pr.Group = &g
		}
		if ref.Kind != nil {
			k := gatewayv1.Kind(*ref.Kind)
			pr.Kind = &k
		}
		if ref.Namespace != nil {
			ns := gatewayv1.Namespace(*ref.Namespace)
			pr.Namespace = &ns
		}
		if ref.SectionName != nil {
			sn := gatewayv1.SectionName(*ref.SectionName)
			pr.SectionName = &sn
		}
		parentRefs = append(parentRefs, pr)
	}
	return parentRefs
}

// renderBrokerHost interpolates the host template with broker-specific values.
// Supports $POD_ORDINAL and $POD_NAME.
func renderBrokerHost(tmpl string, ordinal int, podName string) string {
	result := strings.ReplaceAll(tmpl, "$POD_ORDINAL", fmt.Sprintf("%d", ordinal))
	result = strings.ReplaceAll(result, "$POD_NAME", podName)
	return result
}

func portPtr(port int32) *gatewayv1.PortNumber {
	p := gatewayv1.PortNumber(port)
	return &p
}
