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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Lightweight TLSRoute types that produce the correct YAML for
// gateway.networking.k8s.io/v1alpha2 TLSRoute resources.
// We define these locally because the upstream Gateway API Go types use type
// aliases (v1alpha2.X = v1.X) that the gotohelm transpiler cannot handle.

// TLSRoute mirrors the Gateway API TLSRoute resource.
type TLSRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TLSRouteSpec `json:"spec"`
}

// TLSRouteSpec defines the desired state of a TLSRoute.
type TLSRouteSpec struct {
	ParentRefs []TLSRouteParentRef `json:"parentRefs"`
	Hostnames  []string            `json:"hostnames,omitempty"`
	Rules      []TLSRouteRule      `json:"rules"`
}

// TLSRouteParentRef identifies a parent resource (typically a Gateway).
type TLSRouteParentRef struct {
	Group       *string `json:"group,omitempty"`
	Kind        *string `json:"kind,omitempty"`
	Name        string  `json:"name"`
	Namespace   *string `json:"namespace,omitempty"`
	SectionName *string `json:"sectionName,omitempty"`
}

// TLSRouteRule configures a routing rule.
type TLSRouteRule struct {
	BackendRefs []TLSRouteBackendRef `json:"backendRefs,omitempty"`
}

// TLSRouteBackendRef identifies a backend to route traffic to.
type TLSRouteBackendRef struct {
	Name string `json:"name"`
	Port int32  `json:"port"`
}

// DeepCopyObject implements runtime.Object for TLSRoute to satisfy kube.Object.
// +gotohelm:ignore=true
func (t *TLSRoute) DeepCopyObject() runtime.Object {
	cp := *t
	return &cp
}

// TLSRoutes returns Gateway API TLSRoute resources for external access.
func TLSRoutes(state *RenderState) []*TLSRoute {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	gw := state.Values.External.Gateway
	parentRefs := toTLSRouteParentRefs(gw.ParentRefs)
	labels := FullLabels(state)
	fullname := Fullname(state)

	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	var routes []*TLSRoute

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

func tlsRoutesForListener(fullname string, namespace string, labels map[string]string, parentRefs []TLSRouteParentRef, pods []string, host string, hostTemplate string, name string, listenerTag string, port int32) []*TLSRoute {
	var routes []*TLSRoute

	if host == "" {
		return nil
	}

	bootstrapSvcName := fmt.Sprintf("%s-gateway-bootstrap", fullname)

	bootstrap := &TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s-bootstrap", fullname, listenerTag, name),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: TLSRouteSpec{
			ParentRefs: parentRefs,
			Hostnames:  []string{host},
			Rules: []TLSRouteRule{
				{
					BackendRefs: []TLSRouteBackendRef{
						{
							Name: bootstrapSvcName,
							Port: port,
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
		brokerSvcName := fmt.Sprintf("gw-%s", podname)

		route := &TLSRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1alpha2",
				Kind:       "TLSRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%s-%d", fullname, listenerTag, name, i),
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: TLSRouteSpec{
				ParentRefs: parentRefs,
				Hostnames:  []string{brokerHost},
				Rules: []TLSRouteRule{
					{
						BackendRefs: []TLSRouteBackendRef{
							{
								Name: brokerSvcName,
								Port: port,
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

func toTLSRouteParentRefs(refs []GatewayParentRef) []TLSRouteParentRef {
	var parentRefs []TLSRouteParentRef
	for _, ref := range refs {
		pr := TLSRouteParentRef{
			Name:        ref.Name,
			Group:       ref.Group,
			Kind:        ref.Kind,
			Namespace:   ref.Namespace,
			SectionName: ref.SectionName,
		}
		parentRefs = append(parentRefs, pr)
	}
	return parentRefs
}

func renderBrokerHost(tmpl string, ordinal int, podName string) string {
	result := strings.ReplaceAll(tmpl, "$POD_ORDINAL", fmt.Sprintf("%d", ordinal))
	result = strings.ReplaceAll(result, "$POD_NAME", podName)
	return result
}
