// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.nodeport.go.tpl
package chart

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func nodePortServiceConfig(state *RenderState, listeners redpanda.Listeners) redpanda.ServiceConfig {
	annotations := state.Values.External.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	return redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindNodePort,
		Listeners: listeners,
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-external", ServiceName(state)),
				Namespace:   state.Release.Namespace,
				Labels:      FullLabels(state),
				Annotations: helmette.Merge(annotations, FullAnnotations(state)),
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				PublishNotReadyAddresses: true,
				Selector:                 ClusterPodLabelsSelector(state),
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeNodePort,
			},
		},
	}
}
