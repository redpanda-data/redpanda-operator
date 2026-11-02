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

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// loadBalancerServices returns per-pod LoadBalancer Services for external
// access across every local BrokerPool.
func loadBalancerServices(state *RenderState) ([]*corev1.Service, error) {
	var out []*corev1.Service
	for _, pool := range state.inClusterPools {
		svcs, err := loadBalancerServicesForPool(state, pool)
		if err != nil {
			return nil, err
		}
		out = append(out, svcs...)
	}
	return out, nil
}

// loadBalancerServicesForPool returns the per-pod LoadBalancer Services for
// a single local BrokerPool. Caller iterates state.InClusterPools().
func loadBalancerServicesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ([]*corev1.Service, error) {
	ext := pool.Spec.External
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
	// Mirrors the chart's LoadBalancer Service labels; the typo'd key is
	// kept until the next major release.
	labels["redpanda.com/type"] = "loadbalancer"
	labels["repdanda.com/type"] = "loadbalancer"

	selector := state.clusterPodLabelsSelector()

	domain, addresses, err := externalAdvertiseInputs(state, pool)
	if err != nil {
		return nil, err
	}

	var services []*corev1.Service
	for ord := int32(0); ord < pool.GetReplicas(); ord++ {
		podname := fmt.Sprintf("%s-%d", state.poolFullname(pool), ord)
		annotations := map[string]string{}
		for k, v := range ext.Annotations {
			annotations[k] = v
		}

		if ext.ExternalDNS != nil && ext.ExternalDNS.IsEnabled() {
			hostname := podname
			if int(ord) < len(addresses) {
				hostname = addresses[ord]
			}
			if domain != "" {
				hostname += "." + domain
			}
			annotations["external-dns.alpha.kubernetes.io/hostname"] = hostname
		}

		podSelector := map[string]string{}
		for k, v := range selector {
			podSelector[k] = v
		}
		podSelector["statefulset.kubernetes.io/pod-name"] = podname

		ports := lbExternalPorts(pool)

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

func lbExternalPorts(pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.ServicePort {
	return externalServicePorts(pool.Spec.Listeners, false)
}
