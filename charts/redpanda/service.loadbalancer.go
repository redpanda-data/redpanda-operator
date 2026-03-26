// Copyright 2026 Redpanda Data, Inc.
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

// dedicatedListenerNames returns the set of external listener names that have
// per-listener annotations configured on any protocol. These listeners will get
// their own dedicated LoadBalancer Service per broker instead of sharing the
// default one.
func dedicatedListenerNames(listeners *Listeners) map[string]bool {
	dedicated := map[string]bool{}
	for name, l := range helmette.SortedMap(listeners.Admin.External) {
		if len(l.Annotations) > 0 {
			dedicated[name] = true
		}
	}
	for name, l := range helmette.SortedMap(listeners.Kafka.External) {
		if len(l.Annotations) > 0 {
			dedicated[name] = true
		}
	}
	for name, l := range helmette.SortedMap(listeners.HTTP.External) {
		if len(l.Annotations) > 0 {
			dedicated[name] = true
		}
	}
	for name, l := range helmette.SortedMap(listeners.SchemaRegistry.External) {
		if len(l.Annotations) > 0 {
			dedicated[name] = true
		}
	}
	return dedicated
}

// dedicatedListenerAnnotations returns the merged annotations for a named
// listener across all protocols. If multiple protocols define annotations for
// the same listener name, they are merged (last write wins for duplicate keys).
func dedicatedListenerAnnotations(listeners *Listeners, listenerName string) map[string]string {
	merged := map[string]string{}
	for name, l := range helmette.SortedMap(listeners.Admin.External) {
		if name == listenerName {
			for k, v := range helmette.SortedMap(l.Annotations) {
				merged[k] = v
			}
		}
	}
	for name, l := range helmette.SortedMap(listeners.Kafka.External) {
		if name == listenerName {
			for k, v := range helmette.SortedMap(l.Annotations) {
				merged[k] = v
			}
		}
	}
	for name, l := range helmette.SortedMap(listeners.HTTP.External) {
		if name == listenerName {
			for k, v := range helmette.SortedMap(l.Annotations) {
				merged[k] = v
			}
		}
	}
	for name, l := range helmette.SortedMap(listeners.SchemaRegistry.External) {
		if name == listenerName {
			for k, v := range helmette.SortedMap(l.Annotations) {
				merged[k] = v
			}
		}
	}
	return merged
}

// dedicatedListenerSourceRanges returns the LoadBalancerSourceRanges for a named
// listener. Uses the first non-empty value found across protocols.
func dedicatedListenerSourceRanges(listeners *Listeners, listenerName string) []string {
	for name, l := range helmette.SortedMap(listeners.Kafka.External) {
		if name == listenerName && len(l.LoadBalancerSourceRanges) > 0 {
			return l.LoadBalancerSourceRanges
		}
	}
	for name, l := range helmette.SortedMap(listeners.Admin.External) {
		if name == listenerName && len(l.LoadBalancerSourceRanges) > 0 {
			return l.LoadBalancerSourceRanges
		}
	}
	for name, l := range helmette.SortedMap(listeners.HTTP.External) {
		if name == listenerName && len(l.LoadBalancerSourceRanges) > 0 {
			return l.LoadBalancerSourceRanges
		}
	}
	for name, l := range helmette.SortedMap(listeners.SchemaRegistry.External) {
		if name == listenerName && len(l.LoadBalancerSourceRanges) > 0 {
			return l.LoadBalancerSourceRanges
		}
	}
	return nil
}

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

	selector := ClusterPodLabelsSelector(state)

	var services []*corev1.Service
	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	// Identify listeners that should get their own dedicated LB Service.
	dedicated := dedicatedListenerNames(&state.Values.Listeners)

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

		// Default shared LB: includes all listeners that do NOT have dedicated annotations.
		var ports []corev1.ServicePort
		ports = append(ports, state.Values.Listeners.Admin.ServicePortsExcludingListeners("admin", &state.Values.External, dedicated)...)
		ports = append(ports, state.Values.Listeners.Kafka.ServicePortsExcludingListeners("kafka", &state.Values.External, dedicated)...)
		ports = append(ports, state.Values.Listeners.HTTP.ServicePortsExcludingListeners("http", &state.Values.External, dedicated)...)
		ports = append(ports, state.Values.Listeners.SchemaRegistry.ServicePortsExcludingListeners("schema", &state.Values.External, dedicated)...)

		// Only create the shared LB if it has ports remaining.
		if len(ports) > 0 {
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

		// Dedicated LBs: one per listener name that has annotations.
		for listenerName := range helmette.SortedMap(dedicated) {
			var dedicatedPorts []corev1.ServicePort
			dedicatedPorts = append(dedicatedPorts, state.Values.Listeners.Admin.ServicePortsForListener("admin", listenerName, &state.Values.External)...)
			dedicatedPorts = append(dedicatedPorts, state.Values.Listeners.Kafka.ServicePortsForListener("kafka", listenerName, &state.Values.External)...)
			dedicatedPorts = append(dedicatedPorts, state.Values.Listeners.HTTP.ServicePortsForListener("http", listenerName, &state.Values.External)...)
			dedicatedPorts = append(dedicatedPorts, state.Values.Listeners.SchemaRegistry.ServicePortsForListener("schema", listenerName, &state.Values.External)...)

			if len(dedicatedPorts) == 0 {
				continue
			}

			dedicatedAnnotations := map[string]string{}
			for k, v := range helmette.SortedMap(dedicatedListenerAnnotations(&state.Values.Listeners, listenerName)) {
				dedicatedAnnotations[k] = v
			}

			sourceRanges := dedicatedListenerSourceRanges(&state.Values.Listeners, listenerName)

			svc := &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("lb-%s-%s", listenerName, podname),
					Namespace:   state.Release.Namespace,
					Labels:      labels,
					Annotations: dedicatedAnnotations,
				},
				Spec: corev1.ServiceSpec{
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
					LoadBalancerSourceRanges: sourceRanges,
					Ports:                    dedicatedPorts,
					PublishNotReadyAddresses: true,
					Selector:                 podSelector,
					SessionAffinity:          corev1.ServiceAffinityNone,
					Type:                     corev1.ServiceTypeLoadBalancer,
				},
			}

			services = append(services, svc)
		}
	}

	return services
}
