// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

// StretchClusterStatusUpdater represents a status updater for v2 clusters.
type StretchClusterStatusUpdater struct{}

var _ ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterStatusUpdater)(nil)

// NewStretchClusterStatusUpdater returns a StretchClusterStatusUpdater.
func NewStretchClusterStatusUpdater() *StretchClusterStatusUpdater {
	return &StretchClusterStatusUpdater{}
}

// Update updates the given Redpanda v2 cluster with the given cluster status.
func (m *StretchClusterStatusUpdater) Update(cluster *StretchClusterWithPools, status *ClusterStatus) bool {
	dirty := status.Status.UpdateConditions(cluster.StretchCluster)

	for _, pool := range status.Pools {
		if setAndDirtyCheckPools(&cluster.Status.NodePools, pool) {
			dirty = true
		}
	}

	if status.ConfigVersion != nil && cluster.Status.ConfigVersion != *status.ConfigVersion {
		cluster.Status.ConfigVersion = *status.ConfigVersion
		dirty = true
	}

	return dirty
}
