// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.loadbalancer.go.tpl
package chart

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func LoadBalancerServices(state *RenderState, listeners *redpanda.Listeners) []*corev1.Service {
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

	selector := ClusterPodLabelsSelector(state)

	// If every enabled external listener opted into gateway mode (or there are
	// no external listener ports at all), there is nothing to publish. Emitting
	// a LoadBalancer Service with an empty port list is rejected by the API
	// server (`spec.ports: Required value`), so mirror the NodePort path and
	// render no LoadBalancer Services in that case.
	ports := listeners.LoadBalancerServicePorts()
	if len(ports) == 0 {
		return nil
	}

	var services []*corev1.Service
	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	for i, podname := range pods {
		// NB: A range loop is used here as its the most terse way to handle
		// nil maps in gotohelm.
		annotations := map[string]string{}
		for k, v := range helmette.SortedMap(state.Values.External.Annotations) {
			annotations[k] = v
		}

		// TODO: this looks quite broken just based on the fact that if replicas > addresses
		// this panics
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

		svc := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("lb-%s", podname),
				Namespace:   state.Release.Namespace,
				Labels:      labels,
				Annotations: helmette.Merge(annotations, FullAnnotations(state)),
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
