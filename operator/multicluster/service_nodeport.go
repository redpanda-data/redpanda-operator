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

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// nodePortService returns NodePort Services across every local pool with
// External enabled and Type=NodePort. Wrapper used by RenderResources.
func nodePortService(state *RenderState) []*corev1.Service {
	var out []*corev1.Service
	for _, pool := range state.inClusterPools {
		if svc := nodePortServiceForPool(state, pool); svc != nil {
			out = append(out, svc)
		}
	}
	return out
}

// nodePortServiceForPool returns a NodePort Service for a single local pool,
// named <cluster>-<pool>-external. External configuration (enabled, type,
// annotations) and listener port set come from the pool's spec.
func nodePortServiceForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.Service {
	ext := pool.Spec.External
	if ext == nil || !ext.IsEnabled() {
		return nil
	}
	if ext.Service != nil && !ext.Service.IsEnabled() {
		return nil
	}
	if ext.GetType() != string(corev1.ServiceTypeNodePort) {
		return nil
	}

	ports := externalServicePorts(pool.Spec.Listeners, true)
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
			Name:        fmt.Sprintf("%s-external", state.poolFullname(pool)),
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
