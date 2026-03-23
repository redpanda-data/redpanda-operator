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
	"k8s.io/apimachinery/pkg/util/intstr"
)

func serviceInternal(state *RenderState) *corev1.Service {
	// This service is only used to create the DNS entries for each pod in
	// the stateful set and allow the serviceMonitor to target the pods.
	// This service should not be used by any client application.
	var ports []corev1.ServicePort

	l := state.Spec().Listeners

	// Admin listener.
	if l == nil || l.Admin == nil || l.Admin.IsEnabled() {
		adminPort := state.Spec().AdminPort()
		adminServicePort := corev1.ServicePort{
			Name:       internalAdminAPIPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       adminPort,
			TargetPort: intstr.FromInt32(adminPort),
		}
		if l != nil && l.Admin != nil {
			adminServicePort.AppProtocol = l.Admin.AppProtocol
		}
		ports = append(ports, adminServicePort)
	}

	// HTTP proxy listener.
	if l != nil && l.HTTP != nil && l.HTTP.IsEnabled() {
		httpPort := state.Spec().HTTPPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalPandaProxyPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       httpPort,
			TargetPort: intstr.FromInt32(httpPort),
		})
	}

	// Kafka listener.
	if l == nil || l.Kafka == nil || l.Kafka.IsEnabled() {
		kafkaPort := state.Spec().KafkaPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalKafkaPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       kafkaPort,
			TargetPort: intstr.FromInt32(kafkaPort),
		})
	}

	// RPC listener (always required for inter-broker communication).
	rpcPort := state.Spec().RPCPort()
	ports = append(ports, corev1.ServicePort{
		Name:       internalRPCPortName,
		Protocol:   corev1.ProtocolTCP,
		Port:       rpcPort,
		TargetPort: intstr.FromInt32(rpcPort),
	})

	// Schema Registry listener.
	if l != nil && l.SchemaRegistry != nil && l.SchemaRegistry.IsEnabled() {
		srPort := state.Spec().SchemaRegistryPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalSchemaRegistryPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       srPort,
			TargetPort: intstr.FromInt32(srPort),
		})
	}

	annotations := map[string]string{}
	if svc := state.Spec().Service; svc != nil && svc.Internal != nil {
		annotations = svc.Internal.Annotations
	}

	labels := state.commonLabels()
	labels[labelMonitorKey] = fmt.Sprintf("%t", state.Spec().Monitoring.IsEnabled())

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.Spec().GetServiceName(state.fullname()),
			Namespace:   state.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 state.clusterPodLabelsSelector(),
			Ports:                    ports,
		},
	}
}
