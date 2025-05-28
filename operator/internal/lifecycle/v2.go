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
	ctrl "sigs.k8s.io/controller-runtime"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
)

// ClusterWithPools serves as an intermediate structure to merge a Cluster with its NodePools in v2
type ClusterWithPools struct {
	*redpandav1alpha2.Redpanda
	NodePools []*redpandav1alpha3.NodePool
}

func NewClusterWithPools(cluster *redpandav1alpha2.Redpanda, pools ...*redpandav1alpha3.NodePool) *ClusterWithPools {
	return &ClusterWithPools{
		Redpanda:  cluster,
		NodePools: pools,
	}
}

// V2ResourceManagers is a factory function for tying together all of our v2 interfaces.
func V2ResourceManagers(image Image, cloudSecrets CloudSecretsFlags) func(mgr ctrl.Manager) (
	OwnershipResolver[ClusterWithPools, *ClusterWithPools],
	ClusterStatusUpdater[ClusterWithPools, *ClusterWithPools],
	NodePoolRenderer[ClusterWithPools, *ClusterWithPools],
	SimpleResourceRenderer[ClusterWithPools, *ClusterWithPools],
) {
	return func(mgr ctrl.Manager) (
		OwnershipResolver[ClusterWithPools, *ClusterWithPools],
		ClusterStatusUpdater[ClusterWithPools, *ClusterWithPools],
		NodePoolRenderer[ClusterWithPools, *ClusterWithPools],
		SimpleResourceRenderer[ClusterWithPools, *ClusterWithPools],
	) {
		return NewV2OwnershipResolver(), NewV2ClusterStatusUpdater(), NewV2NodePoolRenderer(mgr, image, cloudSecrets), NewV2SimpleResourceRenderer(mgr)
	}
}
