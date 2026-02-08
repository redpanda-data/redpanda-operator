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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func HTTPRoute(state *RenderState) *gatewayv1.HTTPRoute {
	if !state.Values.HTTPRoute.Enabled {
		return nil
	}

	var parentRefs []gatewayv1.ParentReference
	for _, ref := range state.Values.HTTPRoute.ParentRefs {
		parentRef := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(ref.Name),
		}
		if ref.Namespace != nil {
			ns := gatewayv1.Namespace(*ref.Namespace)
			parentRef.Namespace = &ns
		}
		if ref.SectionName != nil {
			sn := gatewayv1.SectionName(*ref.SectionName)
			parentRef.SectionName = &sn
		}
		parentRefs = append(parentRefs, parentRef)
	}

	var hostnames []gatewayv1.Hostname
	for _, host := range state.Values.HTTPRoute.Hostnames {
		hostnames = append(hostnames, gatewayv1.Hostname(state.Template(host)))
	}

	pathType := gatewayv1.PathMatchPathPrefix
	port := gatewayv1.PortNumber(state.Values.Service.Port)

	rules := []gatewayv1.HTTPRouteRule{
		{
			Matches: []gatewayv1.HTTPRouteMatch{
				{
					Path: &gatewayv1.HTTPPathMatch{
						Type:  &pathType,
						Value: ptrTo("/"),
					},
				},
			},
			BackendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName(state.FullName()),
							Port: &port,
						},
					},
				},
			},
		},
	}

	return &gatewayv1.HTTPRoute{
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
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: hostnames,
			Rules:     rules,
		},
	}
}

func ptrTo[T any](v T) *T {
	return &v
}
