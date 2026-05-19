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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// serviceAccounts returns one ServiceAccount per local NodePool. SA config
// (Create flag, name, annotations) is per-pool because workload-identity
// annotations are cloud-IAM-bindings that vary per pool.
func serviceAccounts(state *RenderState) []*corev1.ServiceAccount {
	var out []*corev1.ServiceAccount
	for _, pool := range state.inClusterPools {
		if sa := serviceAccountForPool(state, pool); sa != nil {
			out = append(out, sa)
		}
	}
	return out
}

func serviceAccountForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.ServiceAccount {
	sa := state.PoolSpec(pool).ServiceAccount
	if !sa.ShouldCreate() {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.PoolSpec(pool).GetServiceAccountName(state.poolFullname(pool)),
			Namespace:   state.namespace,
			Labels:      state.commonLabels(),
			Annotations: sa.Annotations,
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}
