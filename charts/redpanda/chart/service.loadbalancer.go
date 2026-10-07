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
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// The label marking the chart's LoadBalancer Services; NOTES.txt selects on
// it.
const (
	loadBalancerTypeLabelKey   = "redpanda.com/type"
	loadBalancerTypeLabelValue = "loadbalancer"

	// The typo'd spelling shipped since the original helm-charts repository
	// https://github.com/redpanda-data/helm-charts/blob/2baa77b99a71a993e639a7138deaf4543727c8a1/charts/redpanda/templates/service.loadbalancer.yaml#L33
	// and selectors in the wild depend on it; drop it in the next major
	// release.
	legacyLoadBalancerTypeLabelKey = "repdanda.com/type"
)

func loadBalancerServiceConfig(state *RenderState, listeners redpanda.Listeners) redpanda.ServiceConfig {
	externalDNS := ptr.Deref(state.Values.External.ExternalDNS, Enableable{})

	labels := FullLabels(state)

	labels[loadBalancerTypeLabelKey] = loadBalancerTypeLabelValue
	labels[legacyLoadBalancerTypeLabelKey] = loadBalancerTypeLabelValue

	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	var brokers []redpanda.BrokerService

	for i, podname := range pods {
		annotations := map[string]string{}

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

		brokers = append(brokers, redpanda.BrokerService{
			Name:        fmt.Sprintf("lb-%s", podname),
			Selector:    map[string]string{"statefulset.kubernetes.io/pod-name": podname},
			Annotations: annotations,
		})
	}

	// NB: An annotation in external.annotations replaces a common annotation
	// that has the same key.
	annotations := map[string]string{}
	maps.Copy(annotations, FullAnnotations(state))
	maps.Copy(annotations, state.Values.External.Annotations)

	return redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindLoadBalancer,
		Listeners: listeners,
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   state.Release.Namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				LoadBalancerSourceRanges: state.Values.External.SourceRanges,
				PublishNotReadyAddresses: true,
				Selector:                 ClusterPodLabelsSelector(state),
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeLoadBalancer,
			},
		},
		Brokers: brokers,
	}
}
