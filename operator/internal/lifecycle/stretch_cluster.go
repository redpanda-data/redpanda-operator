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

// StretchClusterWithPools serves as an intermediate structure to merge a Cluster with its BrokerPools in v2
type StretchClusterWithPools struct {
	*redpandav1alpha2.StretchCluster
	BrokerPools  []*BrokerPoolInCluster
	PodEndpoints []PodEndpoint
	clusters     []string
}

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

// GetBrokerPoolsForCluster returns the BrokerPools associated with the named
// k8s cluster. The returned pools are deep-copied and have MergeDefaults
// applied to their spec — callers may read any field (including ones
// populated only by in-memory defaulting such as ServiceAccount, RBAC, TLS,
// External, Listeners) without first re-defaulting.
//
// Defaulting is done in-memory rather than at API-server / etcd level (see
// BrokerPoolSpec.MergeDefaults) because the defaults are composite and map-
// entry-based, which CRD schema defaults can't express. Returning defaulted
// copies from the getter eliminates the footgun of consumers reading
// default-populated fields on the raw etcd object and silently getting nil.
//
// The deep copy also means callers can mutate the returned pools (e.g. to
// apply operator-level image overrides) without affecting other consumers
// or the underlying cache.
func (s *StretchClusterWithPools) GetBrokerPoolsForCluster(clusterName string) []*redpandav1alpha2.RedpandaBrokerPool {
	var result []*redpandav1alpha2.RedpandaBrokerPool
	for _, brokerPool := range s.BrokerPools {
		if brokerPool.cluster == clusterName {
			result = append(result, s.defaultedPoolCopy(brokerPool.brokerPool))
		}
	}
	return result
}

// GetAllBrokerPools returns every BrokerPool across all k8s clusters. Like
// GetBrokerPoolsForCluster, the returned pools are deep-copied and have
// MergeDefaults applied — see that method for rationale.
func (s *StretchClusterWithPools) GetAllBrokerPools() []*redpandav1alpha2.RedpandaBrokerPool {
	var result []*redpandav1alpha2.RedpandaBrokerPool
	for _, brokerPool := range s.BrokerPools {
		result = append(result, s.defaultedPoolCopy(brokerPool.brokerPool))
	}
	return result
}

// defaultedPoolCopy returns a deep copy of pool with cluster-level
// Storage/Resources/ImagePullSecrets inherited and MergeDefaults applied.
// Centralized so the two getters can't drift. The pipeline is:
//
//  1. DeepCopy of the pool.
//  2. Deep-copy of the cluster spec with cluster-level MergeDefaults applied
//     — so any defaults the cluster would set (e.g. Storage.PV.Enabled) are
//     visible during inheritance, even if the caller's StretchClusterWithPools
//     was constructed from a raw etcd object.
//  3. MergeFromCluster: pool's non-nil subfields win, cluster fills the rest.
//  4. Pool-level MergeDefaults: any still-nil fields get pool defaults.
func (s *StretchClusterWithPools) defaultedPoolCopy(pool *redpandav1alpha2.RedpandaBrokerPool) *redpandav1alpha2.RedpandaBrokerPool {
	out := pool.DeepCopy()
	if s != nil && s.StretchCluster != nil {
		clusterSpec := s.StretchCluster.Spec.DeepCopy()
		clusterSpec.MergeDefaults()
		out.Spec.MergeFromCluster(clusterSpec)
	}
	out.Spec.MergeDefaults()
	return out
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
		return NewStretchClusterOwnershipResolver(), NewStretchClusterStatusUpdater(), NewStretchBrokerPoolRenderer(mgr, redpandaImage, sidecarImage, cloudSecrets), NewStretchClusterSimpleResourceRenderer(mgr, redpandaImage, sidecarImage)
	}
}

var _ MulticlusterResourceManagerFactory[StretchClusterWithPools, *StretchClusterWithPools] = StretchClusterResourceManagers(Image{}, Image{}, CloudSecretsFlags{})
