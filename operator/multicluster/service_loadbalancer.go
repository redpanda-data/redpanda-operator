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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// loadBalancerServices returns per-pod LoadBalancer Services for external access.
func loadBalancerServices(state *RenderState) ([]*corev1.Service, error) {
	ext := state.Spec().External
	if ext == nil || !ext.IsEnabled() {
		return nil, nil
	}
	if ext.Service != nil && !ext.Service.IsEnabled() {
		return nil, nil
	}
	if ext.GetType() != string(corev1.ServiceTypeLoadBalancer) {
		return nil, nil
	}

	labels := state.commonLabels()
	// Preserved typo for backwards compat.
	labels["repdanda.com/type"] = "loadbalancer"

	selector := state.clusterPodLabelsSelector()

	var services []*corev1.Service
	podNames := state.allPodNames()

	for i, podname := range podNames {
		annotations := map[string]string{}
		for k, v := range ext.Annotations {
			annotations[k] = v
		}

		if ext.ExternalDNS != nil && ext.ExternalDNS.IsEnabled() {
			// Determine the DNS prefix: per-pod address if available,
			// single shared address, or fall back to the pod name.
			prefix := podname
			switch {
			case len(ext.Addresses) > 1 && i < len(ext.Addresses):
				prefix = ext.Addresses[i]
			case len(ext.Addresses) == 1:
				prefix = ext.Addresses[0]
			}

			expandedDomain, err := tplutil.Tpl(ext.GetDomain(), state.tplData())
			if err != nil {
				return nil, fmt.Errorf("expanding external domain template: %w", err)
			}
			annotations["external-dns.alpha.kubernetes.io/hostname"] = fmt.Sprintf("%s.%s", prefix, expandedDomain)
		}

		podSelector := map[string]string{}
		for k, v := range selector {
			podSelector[k] = v
		}
		podSelector["statefulset.kubernetes.io/pod-name"] = podname

		ports := lbExternalPorts(state)

		svc := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("lb-%s", podname),
				Namespace:   state.namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				LoadBalancerSourceRanges: ext.SourceRanges,
				Ports:                    ports,
				PublishNotReadyAddresses: true,
				Selector:                 podSelector,
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeLoadBalancer,
			},
		}

		services = append(services, svc)
	}

	return services, nil
}

func lbExternalPorts(state *RenderState) []corev1.ServicePort {
	return externalServicePorts(state.Spec().Listeners, false)
}
