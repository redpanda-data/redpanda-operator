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
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// StretchClusterWithPools serves as an intermediate structure to merge a Cluster with its NodePools in v2
type StretchClusterWithPools struct {
	*redpandav1alpha2.StretchCluster
	NodePools []*redpandav1alpha2.NodePool
	clusters  []string
}

func NewStretchClusterWithPools(cluster *redpandav1alpha2.StretchCluster, clusters []string, pools ...*redpandav1alpha2.NodePool) *StretchClusterWithPools {
	return &StretchClusterWithPools{
		StretchCluster: cluster,
		NodePools:      pools,
		clusters:       clusters,
	}
}

func (s *StretchClusterWithPools) GetClusters() []string {
	return s.clusters
}

// V2ResourceManagers is a factory function for tying together all of our v2 interfaces.
func StretchClusterResourceManagers(redpandaImage, sidecarImage Image, cloudSecrets CloudSecretsFlags) func(mgr multicluster.Manager) (
	OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools],
	ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools],
	NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools],
	SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools],
) {
	return func(mgr multicluster.Manager) (
		OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools],
		ClusterStatusUpdater[StretchClusterWithPools, *StretchClusterWithPools],
		NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools],
		SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools],
	) {
		return NewStretchClusterOwnershipResolver(), NewStretchClusterStatusUpdater(), NewStretchNodePoolRenderer(mgr, redpandaImage, sidecarImage, cloudSecrets), NewStretchClusterSimpleResourceRenderer(mgr)
	}
}

var _ MulticlusterResourceManagerFactory[StretchClusterWithPools, *StretchClusterWithPools] = StretchClusterResourceManagers(Image{}, Image{}, CloudSecretsFlags{})
