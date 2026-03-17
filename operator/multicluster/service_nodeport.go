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
)

// nodePortService returns a NodePort Service for external access.
func nodePortService(state *RenderState) *corev1.Service {
	ext := state.Spec().External
	if ext == nil || !ext.IsEnabled() {
		return nil
	}
	if ext.Service != nil && !ext.Service.IsEnabled() {
		return nil
	}
	if ext.GetType() != string(corev1.ServiceTypeNodePort) {
		return nil
	}

	ports := externalServicePorts(state.Spec().Listeners, true)
	if len(ports) == 0 {
		return nil
	}

	annotations := ext.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-external", state.Spec().GetServiceName(state.fullname())),
			Namespace:   state.namespace,
			Labels:      state.commonLabels(),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
			Ports:                    ports,
			PublishNotReadyAddresses: true,
			Selector:                 state.clusterPodLabelsSelector(),
			SessionAffinity:          corev1.ServiceAffinityNone,
			Type:                     corev1.ServiceTypeNodePort,
		},
	}
}
