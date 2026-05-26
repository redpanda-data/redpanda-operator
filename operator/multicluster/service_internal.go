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

// serviceInternal returns the cluster-wide headless ClusterIP Service. This
// is the cluster's DNS root: <cluster>.<ns>.svc.<domain> resolves to every
// pod in every local pool, so it can't be per-pool. Listener ports and the
// Monitoring label are read from the first local pool — when pools disagree,
// the headless Service can only advertise one port set (representative-pool
// model; heterogeneous per-pool ports require client-side per-pod DNS).
// Annotations come from StretchCluster.spec.InternalServiceAnnotations.
func serviceInternal(state *RenderState) *corev1.Service {
	if len(state.inClusterPools) == 0 {
		return nil
	}
	rep := state.inClusterPools[0]

	var ports []corev1.ServicePort

	l := rep.Spec.Listeners

	// Admin listener.
	if l == nil || l.Admin == nil || l.Admin.IsEnabled() {
		adminPort := rep.Spec.AdminPort()
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
		httpPort := rep.Spec.HTTPPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalPandaProxyPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       httpPort,
			TargetPort: intstr.FromInt32(httpPort),
		})
	}

	// Kafka listener.
	if l == nil || l.Kafka == nil || l.Kafka.IsEnabled() {
		kafkaPort := rep.Spec.KafkaPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalKafkaPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       kafkaPort,
			TargetPort: intstr.FromInt32(kafkaPort),
		})
	}

	// RPC listener (always required for inter-broker communication).
	rpcPort := rep.Spec.RPCPort()
	ports = append(ports, corev1.ServicePort{
		Name:       internalRPCPortName,
		Protocol:   corev1.ProtocolTCP,
		Port:       rpcPort,
		TargetPort: intstr.FromInt32(rpcPort),
	})

	// Schema Registry listener.
	if l != nil && l.SchemaRegistry != nil && l.SchemaRegistry.IsEnabled() {
		srPort := rep.Spec.SchemaRegistryPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalSchemaRegistryPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       srPort,
			TargetPort: intstr.FromInt32(srPort),
		})
	}

	annotations := state.Spec().InternalServiceAnnotations

	labels := state.commonLabels()
	labels[labelMonitorKey] = fmt.Sprintf("%t", rep.Spec.Monitoring.IsEnabled())

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.fullname(),
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
