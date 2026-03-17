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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TODO: This PDB only covers pods in the local Kubernetes cluster, but stretch
// cluster node pools span multiple independent clusters. A single PDB cannot
// enforce quorum across clusters — it only prevents the local scheduler from
// evicting too many pods at once. Since the stretch cluster reconciler renders
// resources per-cluster, controller-initiated rollouts can coordinate across
// clusters (e.g., gating restarts on cross-cluster health checks).
//
// However, the PDB is the only protection against evictions that happen outside our
// controller's control — node drains, spot instance reclamation, cluster
// autoscaler scale-downs, etc. In those cases, the local PDB limits damage to
// maxUnavailable=1 per cluster, but cannot prevent simultaneous evictions in
// *different* clusters from collectively breaking Raft quorum (e.g., two
// clusters each evicting one pod at the same time in a 3-replica stretch
// cluster). True cross-cluster disruption safety would require an admission
// webhook or external coordinator that checks global cluster health before
// allowing local evictions to proceed.
//
// For now this is a best-effort local safeguard with maxUnavailable=1.
func podDisruptionBudget(state *RenderState) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.FromInt32(1)
	matchLabels := state.clusterPodLabelsSelector()
	matchLabels[labelPDBKey] = state.fullname()

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.fullname(),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			MaxUnavailable: &maxUnavailable,
		},
	}
}
