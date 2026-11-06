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
	ctrl "sigs.k8s.io/controller-runtime"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
)

// ClusterWithPools serves as an intermediate structure to merge a Cluster with its pools in v2
type ClusterWithPools struct {
	*redpandav1alpha2.Redpanda
	// Pools are resolved by [conversion.ResolvePools].
	Pools []conversion.OwnedPool
}

// NewClusterWithPools returns cluster with pools owned by the given NodePools.
func NewClusterWithPools(cluster *redpandav1alpha2.Redpanda, pools ...*redpandav1alpha2.NodePool) *ClusterWithPools {
	return NewClusterWithResolvedPools(cluster, conversion.ResolvePools(pools, nil))
}

func NewClusterWithResolvedPools(cluster *redpandav1alpha2.Redpanda, pools []conversion.OwnedPool) *ClusterWithPools {
	return &ClusterWithPools{
		Redpanda: cluster,
		Pools:    pools,
	}
}

// BrokerPools returns the rendered spec of each pool.
func (c *ClusterWithPools) BrokerPools() []*redpandav1alpha2.RedpandaBrokerPool {
	return conversion.BrokerPools(c.Pools)
}

// V2ResourceManagers is a factory function for tying together all of our v2 interfaces.
func V2ResourceManagers(redpandaImage, sidecarImage Image, cloudSecrets CloudSecretsFlags) func(mgr ctrl.Manager) (
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
		return NewV2OwnershipResolver(), NewV2ClusterStatusUpdater(), NewV2NodePoolRenderer(mgr, redpandaImage, sidecarImage, cloudSecrets), NewV2SimpleResourceRenderer(mgr)
	}
}
