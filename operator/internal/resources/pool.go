// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func sortSetByName(sets []*appsv1.StatefulSet) []*appsv1.StatefulSet {
	slices.SortStableFunc(sets, func(a, b *appsv1.StatefulSet) int {
		return strings.Compare(client.ObjectKeyFromObject(a).String(), client.ObjectKeyFromObject(b).String())
	})

	return sets
}

type pool struct {
	pods []*corev1.Pod
	set  *appsv1.StatefulSet
}

func newPool(set *appsv1.StatefulSet, pods ...client.Object) *pool {
	pool := &pool{
		set: set,
	}
	for i := range pods {
		pool.pods = append(pool.pods, pods[i].(*corev1.Pod))
	}

	return pool
}

type PoolManager struct {
	existingPools map[types.NamespacedName]*pool
	desiredPools  map[types.NamespacedName]*pool
}

func NewPoolManager() *PoolManager {
	return &PoolManager{
		existingPools: make(map[types.NamespacedName]*pool),
		desiredPools:  make(map[types.NamespacedName]*pool),
	}
}

func (p *PoolManager) AddExisting(set *appsv1.StatefulSet, pods ...client.Object) {
	p.existingPools[client.ObjectKeyFromObject(set)] = newPool(set, pods...)
}

func (p *PoolManager) AddDesired(sets ...*appsv1.StatefulSet) {
	for _, set := range sets {
		p.desiredPools[client.ObjectKeyFromObject(set)] = newPool(set)
	}
}

type ScaleReadiness int

const (
	ScaleReady ScaleReadiness = iota
	ScaleNotReady
	ScaleNeedsClusterCheck
)

func (p *PoolManager) CheckScale() ScaleReadiness {
	// if we have no existing pools
	if len(p.existingPools) == 0 {
		return ScaleReady
	}

	for _, pool := range p.existingPools {
		if ptr.Deref(pool.set.Spec.Replicas, 0) != pool.set.Status.UpdatedReplicas {
			// we're in the middle of a scaling operation
			return ScaleNotReady
		}
	}

	// at this point all of our sets are stable, but we need to check the cluster
	// health to see whether or not we can do a scale operation
	return ScaleNeedsClusterCheck
}

func (p *PoolManager) ToCreate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.desiredPools {
		if _, ok := p.existingPools[nn]; !ok {
			sets = append(sets, p.desiredPools[nn].set)
		}
	}

	return sortSetByName(sets)
}

func (p *PoolManager) ToScaleUp() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; ok {
			existing, desired := p.existingPools[nn], p.desiredPools[nn]
			existingReplicas, desiredReplicas := existing.set.Status.Replicas, desired.set.Status.Replicas

			if existingReplicas < desiredReplicas {
				// we use the desired set spec here
				set := desired.set.DeepCopy()

				set.Spec.Replicas = ptr.To(existingReplicas + 1)
				sets = append(sets, set)
			}
		}
	}

	return sortSetByName(sets)
}

func (p *PoolManager) Desired() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for _, desired := range p.desiredPools {
		sets = append(sets, desired.set.DeepCopy())
	}

	return sortSetByName(sets)
}

func (p *PoolManager) ToScaleDown() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			existing := p.existingPools[nn]
			existingReplicas := existing.set.Status.Replicas

			if existing.set.Status.Replicas != 0 {
				set := existing.set.DeepCopy()

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, set)
			}
		} else {
			existing, desired := p.existingPools[nn], p.desiredPools[nn]
			existingReplicas, desiredReplicas := ptr.Deref(existing.set.Spec.Replicas, 0), ptr.Deref(desired.set.Spec.Replicas, 0)

			if existingReplicas > desiredReplicas {
				// we use the desired set spec here
				set := desired.set.DeepCopy()

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, set)
			}
		}
	}

	return sortSetByName(sets)
}

func (p *PoolManager) ToDelete() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			sets = append(sets, p.existingPools[nn].set)
		}
	}

	return sortSetByName(sets)
}
