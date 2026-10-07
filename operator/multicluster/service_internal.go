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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
)

// serviceInternal returns the cluster-wide headless ClusterIP Service. This
// is the cluster's DNS root: <cluster>.<ns>.svc.<domain> resolves to every
// pod in every local pool, so it can't be per-pool. Listener ports and the
// Monitoring label are read from the first local pool — when pools disagree,
// the headless Service can only advertise one port set (representative-pool
// model; heterogeneous per-pool ports require client-side per-pod DNS).
// Annotations come from StretchCluster.spec.InternalServiceAnnotations.
func serviceInternal(state *RenderState) []*corev1.Service {
	if len(state.inClusterPools) == 0 {
		return nil
	}
	rep := state.inClusterPools[0]

	labels := state.commonLabels()
	labels[labelMonitorKey] = fmt.Sprintf("%t", rep.Spec.Monitoring.IsEnabled())

	// NB: Unlike the per-pod Services, the headless Service omits admin and
	// kafka if the spec disables them.
	listeners := poolListeners(state, rep)
	config := redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindHeadless,
		Listeners: withoutDisabled(listeners.Reserved(), rep, []redpanda.APIKind{redpanda.AdminAPI, redpanda.KafkaAPI, redpanda.HTTPAPI, redpanda.SchemaRegistryAPI}),
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        state.fullname(),
				Namespace:   state.namespace,
				Labels:      labels,
				Annotations: state.Spec().InternalServiceAnnotations,
			},
			Spec: corev1.ServiceSpec{
				Type:                     corev1.ServiceTypeClusterIP,
				PublishNotReadyAddresses: true,
				ClusterIP:                corev1.ClusterIPNone,
				Selector:                 state.clusterPodLabelsSelector(),
			},
		},
	}

	return config.Render()
}
