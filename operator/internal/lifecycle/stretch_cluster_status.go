// Copyright 2026 Redpanda Data, Inc.
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

// StretchClusterStatusUpdater represents a status updater for v2 clusters.
type StretchClusterStatusUpdater struct{}

var _ ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterStatusUpdater)(nil)

// NewStretchClusterStatusUpdater returns a StretchClusterStatusUpdater.
func NewStretchClusterStatusUpdater() *StretchClusterStatusUpdater {
	return &StretchClusterStatusUpdater{}
}

// Update updates the given Redpanda v2 cluster with the given cluster status.
func (m *StretchClusterStatusUpdater) Update(cluster *StretchClusterWithPools, status *ClusterStatus) bool {
	dirty := status.StretchClusterStatus.UpdateConditions(cluster.StretchCluster)

	for _, pool := range status.Pools {
		if setAndDirtyCheckBrokerPools(&cluster.Status.BrokerPools, pool) {
			dirty = true
		}
	}

	if status.ConfigVersion != nil && cluster.Status.ConfigVersion != *status.ConfigVersion {
		cluster.Status.ConfigVersion = *status.ConfigVersion
		dirty = true
	}

	if status.LicenseStatus != nil {
		cluster.Status.LicenseStatus = status.LicenseStatus
		dirty = true
	}

	return dirty
}

func setAndDirtyCheckBrokerPools(pools *[]redpandav1alpha2.EmbeddedBrokerPoolStatus, updated PoolStatus) bool {
	dirty := false
	for i, existing := range *pools {
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

			(*pools)[i] = existing
			return dirty
		}
	}

	*pools = append(*pools, redpandav1alpha2.EmbeddedBrokerPoolStatus{
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
