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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func HorizontalPodAutoscaler(state *RenderState) *autoscalingv2.HorizontalPodAutoscaler {
	if !state.Values.Autoscaling.Enabled {
		return nil
	}

	metrics := []autoscalingv2.MetricSpec{}

	if state.Values.Autoscaling.TargetCPUUtilizationPercentage != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: "Resource",
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: state.Values.Autoscaling.TargetCPUUtilizationPercentage,
				},
			},
		})
	}

	if state.Values.Autoscaling.TargetMemoryUtilizationPercentage != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: "Resource",
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: state.Values.Autoscaling.TargetMemoryUtilizationPercentage,
				},
			},
		})
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    state.Labels(nil),
			Name:      state.FullName(),
			Namespace: state.Namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       state.FullName(),
			},
			MinReplicas: ptr.To(state.Values.Autoscaling.MinReplicas),
			MaxReplicas: state.Values.Autoscaling.MaxReplicas,
			Metrics:     metrics,
		},
	}
}
