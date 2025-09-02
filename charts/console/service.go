// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Service(state *RenderState) *corev1.Service {
	port := corev1.ServicePort{
		Name:     "http",
		Port:     int32(state.Values.Service.Port),
		Protocol: corev1.ProtocolTCP,
	}

	if state.Values.Service.TargetPort != nil {
		port.TargetPort = intstr.FromInt32(*state.Values.Service.TargetPort)
	}

	if helmette.Contains("NodePort", string(state.Values.Service.Type)) && state.Values.Service.NodePort != nil {
		port.NodePort = *state.Values.Service.NodePort
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.FullName(),
			Namespace:   state.Namespace,
			Labels:      state.Labels(nil),
			Annotations: state.Values.Service.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     state.Values.Service.Type,
			Selector: state.SelectorLabels(),
			Ports:    []corev1.ServicePort{port},
		},
	}
}
