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
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// V1ClusterStatusUpdater represents a status updater for v1 clusters.
type V1ClusterStatusUpdater struct{}

var _ ClusterStatusUpdater[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster] = (*V1ClusterStatusUpdater)(nil)

// NewV1ClusterStatusUpdater returns a V1ClusterStatusUpdater.
func NewV1ClusterStatusUpdater() *V1ClusterStatusUpdater {
	return &V1ClusterStatusUpdater{}
}

// Update updates the given Redpanda v1 cluster with the given cluster status.
func (m *V1ClusterStatusUpdater) Update(cluster *vectorizedv1alpha1.Cluster, status *ClusterStatus) bool {
	return false
}
