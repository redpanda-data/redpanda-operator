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
	"github.com/redpanda-data/common-go/kube"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// rbac returns the RBAC objects across every local pool.
func rbac(state *RenderState) []kube.Object {
	var out []kube.Object
	for _, pool := range state.inClusterPools {
		roleSet := rbacForPool(state, pool)
		out = append(out, roleSet.Render()...)
	}
	return out
}

// rbacForPool resolves a single local pool into the shared RBAC set, which
// never sees the CRD types. It is the operator's one translation function for
// everything RBAC shaped.
//
// RBAC and RackAwareness toggles are per-pool, so each pool gets its own
// objects named <cluster>-<pool>-<purpose>. Cluster-scoped names additionally
// fold in <namespace> to disambiguate StretchClusters sharing a name across
// namespaces.
func rbacForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) redpanda.RoleSet {
	enabled := pool.Spec.RBAC.IsEnabled()

	return redpanda.RoleSet{
		Prefix:         state.poolFullname(pool),
		Namespace:      state.namespace,
		Labels:         state.commonLabels(),
		ServiceAccount: pool.Spec.GetServiceAccountName(state.poolFullname(pool)),
		Roles: redpanda.Roles{
			RPKDebugBundle: enabled && ptr.Deref(pool.Spec.RBAC.RPKDebugBundle, false),
			Sidecar:        enabled,
		},
		ClusterRoles: redpanda.ClusterRoles{
			MetricsReader:        enabled,
			StretchRackAwareness: enabled && pool.Spec.RackAwareness.IsEnabled(),
		},
	}
}
