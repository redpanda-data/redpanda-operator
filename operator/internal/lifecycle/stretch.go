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
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// StretchClusterWithPools serves as an intermediate structure to merge a Cluster with its NodePools in v2
type StretchClusterWithPools struct {
	*redpandav1alpha2.StretchCluster
	NodePools []*redpandav1alpha2.NodePool
}

func NewStrechClusterWithPools(stretchCluster *redpandav1alpha2.StretchCluster, pools ...*redpandav1alpha2.NodePool) *StretchClusterWithPools {
	return &StretchClusterWithPools{
		StretchCluster: stretchCluster,
		NodePools:      pools,
	}
}

// StrechResourceManagers is a factory function for tying together all of our Stretch.
func StrechResourceManagers(redpandaImage, sidecarImage Image, cloudSecrets CloudSecretsFlags) func(mgr cluster.Cluster) (
	OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools],
	ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools],
	NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools],
	SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools],
) {
	return func(mgr cluster.Cluster) (
		OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools],
		ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools],
		NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools],
		SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools],
	) {
		return NewStretchOwnershipResolver(), NewStretchStatusUpdater(), NewStretchNodePoolRenderer(mgr, redpandaImage, sidecarImage, cloudSecrets), NewStretchSimpleResourceRenderer(mgr)
	}
}
