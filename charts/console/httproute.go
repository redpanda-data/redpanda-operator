// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_httproute.go.tpl
package console

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Lightweight HTTPRoute types that produce the correct YAML for
// gateway.networking.k8s.io/v1 HTTPRoute resources.
// We define these locally because the upstream Gateway API Go types are
// pointer-heavy and use type aliases that the gotohelm transpiler cannot
// construct. The chart renders the same wire bytes; the operator's
// controller-runtime cache uses the upstream type registered in [Scheme] (see
// Types and the gatewayv1 registration in render.go).

// HTTPRoute mirrors the Gateway API HTTPRoute resource.
type HTTPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HTTPRouteSpec `json:"spec"`
}

// HTTPRouteSpec defines the desired state of an HTTPRoute.
type HTTPRouteSpec struct {
	ParentRefs []HTTPRouteParentRef `json:"parentRefs"`
	Hostnames  []string             `json:"hostnames,omitempty"`
	Rules      []HTTPRouteRule      `json:"rules"`
}

// HTTPRouteParentRef identifies a parent resource (typically a Gateway).
type HTTPRouteParentRef struct {
	Group       *string `json:"group,omitempty"`
	Kind        *string `json:"kind,omitempty"`
	Name        string  `json:"name"`
	Namespace   *string `json:"namespace,omitempty"`
	SectionName *string `json:"sectionName,omitempty"`
	Port        *int32  `json:"port,omitempty"`
}

// HTTPRouteRule configures a routing rule. An empty Matches slice is rendered
// as an omitted field, which Gateway API treats as a match-all (path prefix
// "/") rule.
type HTTPRouteRule struct {
	Matches     []HTTPRouteMatch      `json:"matches,omitempty"`
	BackendRefs []HTTPRouteBackendRef `json:"backendRefs,omitempty"`
}

// HTTPRouteMatch defines the predicate used to match requests to a rule.
type HTTPRouteMatch struct {
	Path *HTTPRoutePathMatch `json:"path,omitempty"`
}

// HTTPRoutePathMatch matches requests based on their URL path.
type HTTPRoutePathMatch struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// HTTPRouteBackendRef identifies the backend to route matching traffic to.
type HTTPRouteBackendRef struct {
	Name string `json:"name"`
	Port int32  `json:"port"`
}

// DeepCopyObject implements runtime.Object for HTTPRoute to satisfy kube.Object.
// +gotohelm:ignore=true
func (h *HTTPRoute) DeepCopyObject() runtime.Object {
	cp := *h
	return &cp
}

// HTTPRoutes returns the Gateway API HTTPRoute(s) exposing the Console Service
// as an alternative to Ingress. Rendering is gated on `httpRoute.enabled`; the
// single rule routes matching traffic to the Console Service. A slice is
// returned (mirroring the redpanda chart's TLSRoutes) so the caller can append
// without nil-handling a single pointer.
func HTTPRoutes(state *RenderState) []*HTTPRoute {
	if !state.Values.HTTPRoute.Enabled {
		return nil
	}

	var hostnames []string
	for _, host := range state.Values.HTTPRoute.Hostnames {
		hostnames = append(hostnames, state.Template(host))
	}

	route := &HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.FullName(),
			Labels:      state.Labels(state.Values.HTTPRoute.Labels),
			Namespace:   state.Namespace,
			Annotations: state.Values.HTTPRoute.Annotations,
		},
		Spec: HTTPRouteSpec{
			ParentRefs: state.Values.HTTPRoute.ParentRefs,
			Hostnames:  hostnames,
			Rules: []HTTPRouteRule{
				{
					Matches: state.Values.HTTPRoute.Matches,
					BackendRefs: []HTTPRouteBackendRef{
						{
							Name: state.FullName(),
							Port: state.Values.Service.Port,
						},
					},
				},
			},
		},
	}

	return []*HTTPRoute{route}
}
