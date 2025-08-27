// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_poddisruptionbudget.go.tpl
package redpanda

import (
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func PodDisruptionBudget(state *RenderState) *policyv1.PodDisruptionBudget {
	budget := state.Values.Statefulset.Budget.MaxUnavailable

	// to maintain quorum, raft cannot lose more than half its members
	minReplicas := state.Values.Statefulset.Replicas / 2

	// the lowest we can go is 1 so allow that always
	if budget > 1 && budget > minReplicas {
		panic(fmt.Sprintf("statefulset.budget.maxUnavailable is set too high to maintain quorum: %d > %d", budget, minReplicas))
	}

	maxUnavailable := intstr.FromInt32(int32(budget))
	matchLabels := StatefulSetPodLabelsSelector(state)
	matchLabels["redpanda.com/poddisruptionbudget"] = Fullname(state)

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      Fullname(state),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			MaxUnavailable: &maxUnavailable,
		},
	}
}
