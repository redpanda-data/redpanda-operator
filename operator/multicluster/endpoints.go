// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"strings"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// perPodEndpoints renders Endpoints and EndpointSlices for selector-less
// per-pod Services in flat network mode. Each per-pod Service gets an
// Endpoints object (for CoreDNS) and an EndpointSlice (for kube-proxy/mesh)
// pointing to the actual pod IP.
func perPodEndpoints(state *RenderState) ([]kube.Object, error) {
	if !state.Spec().Networking.IsFlatNetwork() {
		return nil, nil
	}
	if len(state.podEndpoints) == 0 {
		return nil, nil
	}

	spec := state.Spec()
	ports := perPodServicePorts(spec)

	var objects []kube.Object
	for _, pool := range state.pools {
		for i := int32(0); i < pool.GetReplicas(); i++ {
			svcName := PerPodServiceName(pool, i)

			// Match service name to pod by suffix.
			// TODO: should the per-pod service names match the pod names?
			var ep PodEndpoint
			found := false
			for _, e := range state.podEndpoints {
				if strings.HasSuffix(e.Name, svcName) {
					ep = e
					found = true
					break
				}
			}
			if !found {
				continue
			}

			objects = append(objects, endpointsForService(state, svcName, ep, ports)...)
		}
	}

	return objects, nil
}

func endpointsForService(state *RenderState, svcName string, ep PodEndpoint, svcPorts []corev1.ServicePort) []kube.Object {
	labels := state.commonLabels()

	// Endpoints — CoreDNS resolves headless service DNS from this API.
	var v1Ports []corev1.EndpointPort
	for _, p := range svcPorts {
		v1Ports = append(v1Ports, corev1.EndpointPort{
			Name:     p.Name,
			Port:     p.Port,
			Protocol: p.Protocol,
		})
	}
	epObj := &corev1.Endpoints{ //nolint:staticcheck // Endpoints required for CoreDNS headless service resolution
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Endpoints"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: state.namespace,
			Labels:    labels,
		},
		Subsets: []corev1.EndpointSubset{{ //nolint:staticcheck // see above
			Addresses: []corev1.EndpointAddress{{IP: ep.IP}},
			Ports:     v1Ports,
		}},
	}

	// EndpointSlice — for kube-proxy and service mesh consumers.
	var slicePorts []discoveryv1.EndpointPort
	for _, p := range svcPorts {
		p := p
		slicePorts = append(slicePorts, discoveryv1.EndpointPort{
			Name:     &p.Name,
			Port:     &p.Port,
			Protocol: &p.Protocol,
		})
	}
	sliceLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		sliceLabels[k] = v
	}
	sliceLabels[discoveryv1.LabelServiceName] = svcName
	epSlice := &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{APIVersion: "discovery.k8s.io/v1", Kind: "EndpointSlice"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName + "-cross-cluster",
			Namespace: state.namespace,
			Labels:    sliceLabels,
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{ep.IP},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(ep.Ready)},
		}},
		Ports: slicePorts,
	}

	return []kube.Object{epObj, epSlice}
}

