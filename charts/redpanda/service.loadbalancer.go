// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.loadbalancer.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func LoadBalancerServices(state *RenderState) []*corev1.Service {
	// This is technically a divergence from previous behavior but this matches
	// the NodePort's check and is more reasonable.
	if !state.Values.External.Enabled || !state.Values.External.Service.Enabled {
		return nil
	}

	if state.Values.External.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}

	externalDNS := ptr.Deref(state.Values.External.ExternalDNS, Enableable{})

	labels := FullLabels(state)

	// This typo is intentionally being preserved for backwards compat
	// https://github.com/redpanda-data/helm-charts/blob/2baa77b99a71a993e639a7138deaf4543727c8a1/charts/redpanda/templates/service.loadbalancer.yaml#L33
	labels["repdanda.com/type"] = "loadbalancer"

	selector := StatefulSetPodLabelsSelector(state)

	var services []*corev1.Service
	replicas := state.Values.Statefulset.Replicas // TODO fix me once the transpiler is fixed.
	for i := int32(0); i < replicas; i++ {
		podname := fmt.Sprintf("%s-%d", Fullname(state), i)

		// NB: A range loop is used here as its the most terse way to handle
		// nil maps in gotohelm.
		annotations := map[string]string{}
		for k, v := range helmette.SortedMap(state.Values.External.Annotations) {
			annotations[k] = v
		}

		if externalDNS.Enabled {
			prefix := podname
			if len(state.Values.External.Addresses) > 0 {
				if len(state.Values.External.Addresses) == 1 {
					prefix = state.Values.External.Addresses[0]
				} else {
					prefix = state.Values.External.Addresses[i]
				}
			}

			address := fmt.Sprintf("%s.%s", prefix, helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot))

			annotations["external-dns.alpha.kubernetes.io/hostname"] = address
		}

		// NB: A range loop is used here as its the most terse way to handle
		// nil maps in gotohelm.
		podSelector := map[string]string{}
		for k, v := range selector {
			podSelector[k] = v
		}

		podSelector["statefulset.kubernetes.io/pod-name"] = podname

		// Divergences pop up here due to iterating over a map. This isn't okay
		// in helm. TODO setup a linter that barks about this? Also a helper
		// for getting the sorted keys of a map?
		var ports []corev1.ServicePort
		ports = append(ports, state.Values.Listeners.Admin.ServicePorts("admin", &state.Values.External)...)
		ports = append(ports, state.Values.Listeners.Kafka.ServicePorts("kafka", &state.Values.External)...)
		ports = append(ports, state.Values.Listeners.HTTP.ServicePorts("http", &state.Values.External)...)
		ports = append(ports, state.Values.Listeners.SchemaRegistry.ServicePorts("schema", &state.Values.External)...)

		svc := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("lb-%s", podname),
				Namespace:   state.Release.Namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				LoadBalancerSourceRanges: state.Values.External.SourceRanges,
				Ports:                    ports,
				PublishNotReadyAddresses: true,
				Selector:                 podSelector,
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeLoadBalancer,
			},
		}

		services = append(services, svc)
	}

	return services
}
