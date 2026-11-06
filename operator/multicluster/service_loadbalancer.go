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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
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

	domain, addresses, err := externalAdvertiseInputs(state, pool)
	if err != nil {
		return nil, err
	}

	var brokers []redpanda.BrokerService
	for ord := int32(0); ord < pool.GetReplicas(); ord++ {
		podname := fmt.Sprintf("%s-%d", state.poolFullname(pool), ord)

		var annotations map[string]string
		if ext.ExternalDNS != nil && ext.ExternalDNS.IsEnabled() {
			hostname := podname
			if int(ord) < len(addresses) {
				hostname = addresses[ord]
			}
			if domain != "" {
				hostname += "." + domain
			}
			annotations = map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": hostname,
			}
		}

		brokers = append(brokers, redpanda.BrokerService{
			Name:        fmt.Sprintf("lb-%s", podname),
			Selector:    map[string]string{"statefulset.kubernetes.io/pod-name": podname},
			Annotations: annotations,
		})
	}

	listeners := poolListeners(state, pool)
	config := redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindLoadBalancer,
		Listeners: listeners.Additional(),
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   state.namespace,
				Labels:      labels,
				Annotations: ext.Annotations,
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				LoadBalancerSourceRanges: ext.SourceRanges,
				PublishNotReadyAddresses: true,
				Selector:                 state.clusterPodLabelsSelector(),
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeLoadBalancer,
			},
		},
		Brokers: brokers,
	}

	return config.Render(), nil
}
