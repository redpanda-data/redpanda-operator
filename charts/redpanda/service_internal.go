// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.internal.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	InternalAdminAPIPortName       = "admin"
	InternalKafkaPortName          = "kafka"
	InternalSchemaRegistryPortName = "schemaregistry"
	InternalPandaProxyPortName     = "http"
)

func MonitoringEnabledLabel(state *RenderState) map[string]string {
	return map[string]string{
		// no gotohelm support for strconv.FormatBool
		"monitoring.redpanda.com/enabled": fmt.Sprintf("%t", state.Values.Monitoring.Enabled),
	}
}

func ServiceInternal(state *RenderState) *corev1.Service {
	// This service is only used to create the DNS enteries for each pod in
	// the stateful set and allow the serviceMonitor to target the pods.
	// This service should not be used by any client application.
	ports := []corev1.ServicePort{}

	ports = append(ports, corev1.ServicePort{
		Name:        InternalAdminAPIPortName,
		Protocol:    "TCP",
		AppProtocol: state.Values.Listeners.Admin.AppProtocol,
		Port:        state.Values.Listeners.Admin.Port,
		TargetPort:  intstr.FromInt32(state.Values.Listeners.Admin.Port),
	})

	if state.Values.Listeners.HTTP.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       InternalPandaProxyPortName,
			Protocol:   "TCP",
			Port:       state.Values.Listeners.HTTP.Port,
			TargetPort: intstr.FromInt32(state.Values.Listeners.HTTP.Port),
		})
	}
	ports = append(ports, corev1.ServicePort{
		Name:       InternalKafkaPortName,
		Protocol:   "TCP",
		Port:       state.Values.Listeners.Kafka.Port,
		TargetPort: intstr.FromInt32(state.Values.Listeners.Kafka.Port),
	})
	ports = append(ports, corev1.ServicePort{
		Name:       "rpc",
		Protocol:   "TCP",
		Port:       state.Values.Listeners.RPC.Port,
		TargetPort: intstr.FromInt32(state.Values.Listeners.RPC.Port),
	})
	if state.Values.Listeners.SchemaRegistry.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       InternalSchemaRegistryPortName,
			Protocol:   "TCP",
			Port:       state.Values.Listeners.SchemaRegistry.Port,
			TargetPort: intstr.FromInt32(state.Values.Listeners.SchemaRegistry.Port),
		})
	}

	annotations := map[string]string{}
	if state.Values.Service != nil {
		annotations = state.Values.Service.Internal.Annotations
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceName(state),
			Namespace:   state.Release.Namespace,
			Labels:      helmette.Merge(FullLabels(state), MonitoringEnabledLabel(state)),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 ClusterPodLabelsSelector(state),
			Ports:                    ports,
		},
	}
}
