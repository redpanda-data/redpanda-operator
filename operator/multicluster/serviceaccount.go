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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// serviceAccount returns ServiceAccounts across every local pool that has
// ServiceAccount.create=true. Legacy wrapper kept for tests / non-bucketed
// callers; the live multicluster controller path goes through
// serviceAccountForPool directly via RenderInClusterPoolResources.
func serviceAccount(state *RenderState) []*corev1.ServiceAccount {
	var out []*corev1.ServiceAccount
	for _, pool := range state.inClusterPools {
		if sa := serviceAccountForPool(state, pool); sa != nil {
			out = append(out, sa)
		}
	}
	return out
}

// serviceAccountForPool returns a ServiceAccount for a single local pool,
// named <cluster>-<pool> (or whatever the pool's ServiceAccount.Name override
// resolves to via GetServiceAccountName).
func serviceAccountForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.ServiceAccount {
	sa := pool.Spec.ServiceAccount
	if !sa.ShouldCreate() {
		return nil
	}

	return redpanda.ServiceAccount(
		pool.Spec.GetServiceAccountName(state.poolFullname(pool)),
		state.namespace,
		state.commonLabels(),
		sa.Annotations,
	)
}
