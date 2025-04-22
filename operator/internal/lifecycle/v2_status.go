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
func (m *V2ClusterStatusUpdater) Update(cluster *redpandav1alpha2.Redpanda, status *ClusterStatus) bool {
	dirty := status.Status.UpdateConditions(cluster)

	for _, pool := range status.Pools {
		if setAndDirtyCheckPools(&cluster.Status.NodePools, pool) {
			dirty = true
		}
	}

	return dirty
}

func setAndDirtyCheckPools(pools *[]redpandav1alpha2.NodePoolStatus, updated PoolStatus) bool {
	dirty := false
	for _, existing := range *pools {
		if existing.Name == updated.Name {
			if setAndDirtyCheck(&existing.Replicas, updated.Replicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.DesiredReplicas, updated.DesiredReplicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.OutOfDateReplicas, updated.OutOfDateReplicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.UpToDateReplicas, updated.UpToDateReplicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.CondemnedReplicas, updated.CondemnedReplicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.ReadyReplicas, updated.ReadyReplicas) {
				dirty = true
			}
			if setAndDirtyCheck(&existing.RunningReplicas, updated.RunningReplicas) {
				dirty = true
			}

			return dirty
		}
	}

	*pools = append(*pools, redpandav1alpha2.NodePoolStatus{
		Name:              updated.Name,
		Replicas:          updated.Replicas,
		DesiredReplicas:   updated.DesiredReplicas,
		OutOfDateReplicas: updated.OutOfDateReplicas,
		UpToDateReplicas:  updated.UpToDateReplicas,
		CondemnedReplicas: updated.CondemnedReplicas,
		ReadyReplicas:     updated.ReadyReplicas,
		RunningReplicas:   updated.RunningReplicas,
	})
	return true
}

func setAndDirtyCheck[T comparable](source *T, value T) bool {
	if *source != value {
		*source = value
		return true
	}
	return false
}
