// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func HTTPRoute(state *RenderState) *gatewayv1.HTTPRoute {
	if !state.Values.Gateway.Enabled {
		return nil
	}

	var parentRefs []gatewayv1.ParentReference
	for _, parentRef := range state.Values.Gateway.ParentRefs {
		ref := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(state.Template(parentRef.Name)),
		}
		if parentRef.Namespace != nil {
			namespace := state.Template(*parentRef.Namespace)
			ref.Namespace = ptr.To(gatewayv1.Namespace(namespace))
		}
		if parentRef.SectionName != nil {
			sectionName := gatewayv1.SectionName(state.Template(string(*parentRef.SectionName)))
			ref.SectionName = ptr.To(sectionName)
		}
		parentRefs = append(parentRefs, ref)
	}

	var hostnames []gatewayv1.Hostname
	for _, hostname := range state.Values.Gateway.Hostnames {
		hostnames = append(hostnames, gatewayv1.Hostname(state.Template(hostname)))
	}

	pathType := gatewayv1.PathMatchPathPrefix
	if state.Values.Gateway.PathType != nil {
		pathType = *state.Values.Gateway.PathType
	}

	path := state.Template(state.Values.Gateway.Path)
	port := gatewayv1.PortNumber(state.Values.Service.Port)

	return &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.FullName(),
			Labels:      state.Labels(nil),
			Namespace:   state.Namespace,
			Annotations: state.Values.Gateway.Annotations,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: hostnames,
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  ptr.To(pathType),
								Value: ptr.To(path),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(state.FullName()),
									Port: ptr.To(port),
								},
							},
						},
					},
				},
			},
		},
	}
}
