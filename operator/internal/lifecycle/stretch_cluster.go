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

// StretchClusterWithPools serves as an intermediate structure to merge a
// StretchCluster with its broker pools across member K8s clusters.
type StretchClusterWithPools struct {
	*redpandav1alpha2.StretchCluster
	BrokerPools  []*BrokerPoolInCluster
	PodEndpoints []PodEndpoint
	clusters     []string
}

// BrokerPoolInCluster pairs a RedpandaBrokerPool with the K8s cluster it
// lives in.
type BrokerPoolInCluster struct {
	cluster    string
	brokerPool *redpandav1alpha2.RedpandaBrokerPool
}

func NewStretchClusterWithPools(cluster *redpandav1alpha2.StretchCluster, clusters []string, pools ...*BrokerPoolInCluster) *StretchClusterWithPools {
	return &StretchClusterWithPools{
		StretchCluster: cluster,
		BrokerPools:    pools,
		clusters:       clusters,
	}
}

func (s *StretchClusterWithPools) GetClusters() []string {
	return s.clusters
}

func (s *StretchClusterWithPools) GetBrokerPoolsForCluster(clusterName string) []*redpandav1alpha2.RedpandaBrokerPool {
	var result []*redpandav1alpha2.RedpandaBrokerPool
	for _, pool := range s.BrokerPools {
		if pool.cluster == clusterName {
			result = append(result, pool.brokerPool)
		}
	}
	return result
}

func (s *StretchClusterWithPools) GetAllBrokerPools() []*redpandav1alpha2.RedpandaBrokerPool {
	var result []*redpandav1alpha2.RedpandaBrokerPool
	for _, pool := range s.BrokerPools {
		result = append(result, pool.brokerPool)
	}
	return result
}

// StretchClusterResourceManagers is a factory function for tying together all
// of the StretchCluster-side resource manager interfaces.
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
		return NewStretchClusterOwnershipResolver(), NewStretchClusterStatusUpdater(), NewStretchNodePoolRenderer(mgr, redpandaImage, sidecarImage, cloudSecrets), NewStretchClusterSimpleResourceRenderer(mgr, redpandaImage, sidecarImage)
	}
}

var _ MulticlusterResourceManagerFactory[StretchClusterWithPools, *StretchClusterWithPools] = StretchClusterResourceManagers(Image{}, Image{}, CloudSecretsFlags{})
