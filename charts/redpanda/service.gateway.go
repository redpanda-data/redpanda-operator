// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.gateway.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// GatewayServices returns ClusterIP Services for Gateway API TLSRoute-based
// external access: one bootstrap service (targeting all pods) and one
// per-broker service (targeting a specific pod via pod-name selector).
// TLSRoute resources reference these services as backends.
func GatewayServices(state *RenderState) []*corev1.Service {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	labels := FullLabels(state)
	selector := ClusterPodLabelsSelector(state)

	// Collect external listener ports across all listener types.
	ports := gatewayServicePorts(state)
	if len(ports) == 0 {
		return nil
	}

	var services []*corev1.Service

	// Bootstrap service: targets all pods for initial client connection.
	bootstrap := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-gateway-bootstrap", Fullname(state)),
			Namespace: state.Release.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports:                    ports,
			PublishNotReadyAddresses: true,
			Selector:                 selector,
			SessionAffinity:          corev1.ServiceAffinityNone,
			Type:                     corev1.ServiceTypeClusterIP,
		},
	}
	services = append(services, bootstrap)

	// Per-broker services: one service per pod, selected by pod name.
	pods := PodNames(state, Pool{Statefulset: state.Values.Statefulset})
	for _, set := range state.Pools {
		pods = append(pods, PodNames(state, set)...)
	}

	for _, podname := range pods {
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
				Name:      fmt.Sprintf("gw-%s", podname),
				Namespace: state.Release.Namespace,
				Labels:    labels,
			},
			Spec: corev1.ServiceSpec{
				Ports:                    ports,
				PublishNotReadyAddresses: true,
				Selector:                 podSelector,
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeClusterIP,
			},
		}
		services = append(services, svc)
	}

	return services
}

// gatewayServicePorts collects external listener ports for Gateway ClusterIP
// services. These match the container ports that TLSRoutes will route to.
func gatewayServicePorts(state *RenderState) []corev1.ServicePort {
	var ports []corev1.ServicePort

	for name, listener := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		ports = append(ports, corev1.ServicePort{
			Name:     fmt.Sprintf("admin-%s", name),
			Protocol: corev1.ProtocolTCP,
			Port:     listener.Port,
		})
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		ports = append(ports, corev1.ServicePort{
			Name:     fmt.Sprintf("kafka-%s", name),
			Protocol: corev1.ProtocolTCP,
			Port:     listener.Port,
		})
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		ports = append(ports, corev1.ServicePort{
			Name:     fmt.Sprintf("http-%s", name),
			Protocol: corev1.ProtocolTCP,
			Port:     listener.Port,
		})
	}

	for name, listener := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if !ptr.Deref(listener.Enabled, state.Values.External.Enabled) || !listener.IsGatewayListener() {
			continue
		}
		ports = append(ports, corev1.ServicePort{
			Name:     fmt.Sprintf("schema-%s", name),
			Protocol: corev1.ProtocolTCP,
			Port:     listener.Port,
		})
	}

	return ports
}
