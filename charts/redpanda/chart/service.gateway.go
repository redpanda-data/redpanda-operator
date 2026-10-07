// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.gateway.go.tpl
package chart

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
)

// gatewayServiceConfig returns the ClusterIP Services for Gateway API
// TLSRoute-based external access: one bootstrap Service (targeting all pods)
// and one per-broker Service (targeting a specific pod via pod-name selector).
// TLSRoute resources reference these Services as backends.
func gatewayServiceConfig(state *RenderState, listeners redpanda.Listeners) redpanda.ServiceConfig {
	var brokers []redpanda.BrokerService
	for _, podname := range gatewayPodNames(state) {
		brokers = append(brokers, redpanda.BrokerService{
			Name:     gatewayBrokerServiceName(podname),
			Selector: map[string]string{"statefulset.kubernetes.io/pod-name": podname},
		})
	}

	return redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindGateway,
		Listeners: listeners,
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-gateway-bootstrap", Fullname(state)),
				Namespace:   state.Release.Namespace,
				Labels:      FullLabels(state),
				Annotations: FullAnnotations(state),
			},
			Spec: corev1.ServiceSpec{
				PublishNotReadyAddresses: true,
				Selector:                 ClusterPodLabelsSelector(state),
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeClusterIP,
			},
		},
		Brokers: brokers,
	}
}
