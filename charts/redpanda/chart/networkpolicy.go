// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_networkpolicy.go.tpl
package chart

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// NetworkPolicy restricts ingress to the broker Pods. RPC is limited to the
// brokers and the Admin API to this release's Pods, the operator and
// adminPeers. Client ports stay open unless clientPeers is set.
func NetworkPolicy(state *RenderState) *networkingv1.NetworkPolicy {
	np := state.Values.NetworkPolicy
	if !np.Enabled {
		return nil
	}

	brokers := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{MatchLabels: ClusterPodLabelsSelector(state)},
	}
	// Brokers, Console and the chart's Jobs share the release's instance label.
	internal := []networkingv1.NetworkPolicyPeer{{
		PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
			"app.kubernetes.io/instance": state.Release.Name,
		}},
	}}
	if np.OperatorPeer != nil {
		internal = append(internal, *np.OperatorPeer)
	}

	clients := networkingv1.NetworkPolicyIngressRule{
		Ports: networkPolicyPorts(networkPolicyClientPorts(state)),
	}
	// No `from` admits every source, including external clients.
	if len(np.ClientPeers) > 0 {
		clients.From = concatPeers(internal, np.ClientPeers)
	}

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        Fullname(state),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: ClusterPodLabelsSelector(state)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: networkPolicyPorts([]int32{state.Values.Listeners.RPC.Port}),
					From:  []networkingv1.NetworkPolicyPeer{brokers},
				},
				{
					Ports: networkPolicyPorts([]int32{state.Values.Listeners.Admin.Port}),
					From:  concatPeers(internal, np.AdminPeers),
				},
				clients,
			},
		},
	}
}

// networkPolicyClientPorts returns the Kafka, HTTP Proxy and Schema Registry
// ports plus every external listener port the broker binds (see the
// container ports in statefulset.go).
func networkPolicyClientPorts(state *RenderState) []int32 {
	l := state.Values.Listeners
	ports := []int32{l.Kafka.Port, l.HTTP.Port, l.SchemaRegistry.Port}
	for _, ext := range helmette.SortedMap(l.Admin.External) {
		if ext.IsEnabled() {
			ports = append(ports, ext.Port)
		}
	}
	for _, ext := range helmette.SortedMap(l.HTTP.External) {
		if ext.IsEnabled() {
			ports = append(ports, ext.Port)
		}
	}
	for _, ext := range helmette.SortedMap(l.Kafka.External) {
		if ext.IsEnabled() {
			ports = append(ports, ext.Port)
		}
	}
	for _, ext := range helmette.SortedMap(l.SchemaRegistry.External) {
		if ext.IsEnabled() {
			ports = append(ports, ext.Port)
		}
	}
	return ports
}

func networkPolicyPorts(ports []int32) []networkingv1.NetworkPolicyPort {
	out := []networkingv1.NetworkPolicyPort{}
	for _, p := range ports {
		port := intstr.FromInt32(p)
		out = append(out, networkingv1.NetworkPolicyPort{
			Protocol: ptr.To(corev1.ProtocolTCP),
			Port:     &port,
		})
	}
	return out
}

// concatPeers copies into a fresh slice so rules never share a backing array.
func concatPeers(a, b []networkingv1.NetworkPolicyPeer) []networkingv1.NetworkPolicyPeer {
	out := []networkingv1.NetworkPolicyPeer{}
	out = append(out, a...)
	out = append(out, b...)
	return out
}
