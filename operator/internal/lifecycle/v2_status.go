// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// V2ClusterStatusUpdater represents a status updater for v2 clusters.
type V2ClusterStatusUpdater struct{}

var _ ClusterStatusUpdater[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda] = (*V2ClusterStatusUpdater)(nil)

// NewV2ClusterStatusUpdater returns a V2ClusterStatusUpdater.
func NewV2ClusterStatusUpdater() *V2ClusterStatusUpdater {
	return &V2ClusterStatusUpdater{}
}

// Update updates the given Redpanda v2 cluster with the given cluster status.
func (m *V2ClusterStatusUpdater) Update(cluster *redpandav1alpha2.Redpanda, status ClusterStatus) bool {
	condition := metav1.Condition{
		Type:               "Quiesced",
		Status:             metav1.ConditionFalse,
		Reason:             "Quiesced",
		ObservedGeneration: cluster.GetGeneration(),
	}
	if status.Quiesced {
		condition.Status = metav1.ConditionTrue
	}
	cluster.Status.ObservedGeneration = cluster.Generation

	return apimeta.SetStatusCondition(&cluster.Status.Conditions, condition)
}
