// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"cmp"
	"slices"

	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// OwnedPool is one pool of a Redpanda cluster and the CR that owns it.
type OwnedPool struct {
	// BrokerPool is what gets rendered. When NodePool is set, it is that
	// NodePool converted by [NodePoolToBrokerPool], never a stored object.
	BrokerPool *redpandav1alpha2.RedpandaBrokerPool
	// NodePool is the owner if set; otherwise BrokerPool owns the pool.
	NodePool *redpandav1alpha2.NodePool
}

// Ref is the clusterRef that the pool's Broker CRs carry.
func (p OwnedPool) Ref() redpandav1alpha2.ClusterRef {
	kind := redpandav1alpha2.BrokerPoolKind
	if p.NodePool != nil {
		kind = redpandav1alpha2.NodePoolKind
	}
	return redpandav1alpha2.ClusterRef{Kind: ptr.To(kind), Name: p.BrokerPool.Name}
}

// ResolvePools decides which CR owns each pool of one Redpanda cluster; both
// lists must already be filtered to pools that reference that cluster.
//
// A NodePool keeps its pool until it releases it (see
// [redpandav1alpha2.NodePool.ReleasesToBrokerPool]) and a RedpandaBrokerPool
// of the same name exists to take it. A release with no RedpandaBrokerPool
// changes nothing, so a pool never drops out of the desired set mid-handoff.
func ResolvePools(nodePools []*redpandav1alpha2.NodePool, brokerPools []*redpandav1alpha2.RedpandaBrokerPool) []OwnedPool {
	brokerPoolNames := map[string]bool{}
	for _, bp := range brokerPools {
		brokerPoolNames[bp.Name] = true
	}

	var pools []OwnedPool
	superseded := map[string]bool{}
	for _, np := range nodePools {
		if brokerPoolNames[np.Name] && np.ReleasesToBrokerPool() {
			continue
		}
		superseded[np.Name] = true
		pools = append(pools, OwnedPool{BrokerPool: NodePoolToBrokerPool(np), NodePool: np})
	}
	for _, bp := range brokerPools {
		if !superseded[bp.Name] {
			pools = append(pools, OwnedPool{BrokerPool: bp})
		}
	}

	slices.SortStableFunc(pools, func(a, b OwnedPool) int {
		return cmp.Or(
			cmp.Compare(a.BrokerPool.Name, b.BrokerPool.Name),
			cmp.Compare(a.BrokerPool.Namespace, b.BrokerPool.Namespace),
		)
	})
	return pools
}

// FindPool returns the resolved pool named name.
func FindPool(pools []OwnedPool, name string) (OwnedPool, bool) {
	i := slices.IndexFunc(pools, func(p OwnedPool) bool { return p.BrokerPool.Name == name })
	if i < 0 {
		return OwnedPool{}, false
	}
	return pools[i], true
}

// BrokerPools returns the pools' rendered specs.
func BrokerPools(pools []OwnedPool) []*redpandav1alpha2.RedpandaBrokerPool {
	specs := make([]*redpandav1alpha2.RedpandaBrokerPool, len(pools))
	for i, p := range pools {
		specs[i] = p.BrokerPool
	}
	return specs
}

// NodePoolToBrokerPool converts a NodePool to the RedpandaBrokerPool that
// renders the same pool. It keeps the NodePool's metadata, so the rendered
// StatefulSet carries the NodePool's generation, as it always has.
func NodePoolToBrokerPool(np *redpandav1alpha2.NodePool) *redpandav1alpha2.RedpandaBrokerPool {
	spec := np.Spec.DeepCopy()
	return &redpandav1alpha2.RedpandaBrokerPool{
		ObjectMeta: *np.ObjectMeta.DeepCopy(),
		Spec: redpandav1alpha2.BrokerPoolSpec{
			ClusterRef: spec.ClusterRef,
			EmbeddedBrokerPoolSpec: redpandav1alpha2.EmbeddedBrokerPoolSpec{
				AdditionalSelectorLabels:             spec.AdditionalSelectorLabels,
				Replicas:                             spec.Replicas,
				AdditionalRedpandaCmdFlags:           spec.AdditionalRedpandaCmdFlags,
				PodTemplate:                          spec.PodTemplate,
				Services:                             spec.Services,
				InitContainers:                       spec.InitContainers,
				Image:                                spec.Image,
				SidecarImage:                         spec.SidecarImage,
				InitContainerImage:                   spec.InitContainerImage,
				PersistentVolumeClaimRetentionPolicy: spec.PersistentVolumeClaimRetentionPolicy,
			},
		},
	}
}

// brokerPoolNodeSpec is the inverse of [NodePoolToBrokerPool] for the spec:
// the fields a RedpandaBrokerPool shares with a NodePool. CEL rejects the
// rest on a pool of a Redpanda cluster.
func brokerPoolNodeSpec(bp *redpandav1alpha2.RedpandaBrokerPool) redpandav1alpha2.EmbeddedNodePoolSpec {
	spec := bp.Spec.DeepCopy()
	return redpandav1alpha2.EmbeddedNodePoolSpec{
		AdditionalSelectorLabels:             spec.AdditionalSelectorLabels,
		Replicas:                             spec.Replicas,
		AdditionalRedpandaCmdFlags:           spec.AdditionalRedpandaCmdFlags,
		PodTemplate:                          spec.PodTemplate,
		Services:                             spec.Services,
		InitContainers:                       spec.InitContainers,
		Image:                                spec.Image,
		SidecarImage:                         spec.SidecarImage,
		InitContainerImage:                   spec.InitContainerImage,
		PersistentVolumeClaimRetentionPolicy: spec.PersistentVolumeClaimRetentionPolicy,
	}
}
