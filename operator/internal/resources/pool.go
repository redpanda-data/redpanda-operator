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
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type podsWithOrdinals struct {
	ordinal int
	pod     *corev1.Pod
}

type poolWithOrdinals struct {
	pods      []*podsWithOrdinals
	set       *appsv1.StatefulSet
	revisions []*appsv1.ControllerRevision
}

type ScaleDownSet struct {
	LastPod     *corev1.Pod
	StatefulSet *appsv1.StatefulSet
}

type PoolTracker struct {
	latestGeneration int64
	existingPools    map[types.NamespacedName]*poolWithOrdinals
	desiredPools     map[types.NamespacedName]*poolWithOrdinals
}

func NewPoolTracker(generation int64) *PoolTracker {
	return &PoolTracker{
		latestGeneration: generation,
		existingPools:    make(map[types.NamespacedName]*poolWithOrdinals),
		desiredPools:     make(map[types.NamespacedName]*poolWithOrdinals),
	}
}

func (p *PoolTracker) ExistingStatefulSets() []string {
	sets := []string{}
	for nn := range p.existingPools {
		sets = append(sets, nn.String())
	}
	return sets
}

func (p *PoolTracker) DesiredStatefulSets() []string {
	sets := []string{}
	for nn := range p.desiredPools {
		sets = append(sets, nn.String())
	}
	return sets
}

func (p *PoolTracker) AddExisting(pools ...*poolWithOrdinals) {
	for i := range pools {
		p.existingPools[client.ObjectKeyFromObject(pools[i].set)] = pools[i]
	}
}

func (p *PoolTracker) AddDesired(sets ...*appsv1.StatefulSet) {
	for _, set := range sets {
		p.desiredPools[client.ObjectKeyFromObject(set)] = &poolWithOrdinals{set: set.DeepCopy()}
	}
}

func (p *PoolTracker) CheckScale() bool {
	// if we have no existing pools
	if len(p.existingPools) == 0 {
		return true
	}

	for _, pool := range p.existingPools {
		replicas := ptr.Deref(pool.set.Spec.Replicas, 0)
		if replicas != pool.set.Status.Replicas || int(replicas) != len(pool.pods) {
			// we're potentially in the middle of a scaling operation
			return false
		}
	}

	return true
}

func (p *PoolTracker) ToCreate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.desiredPools {
		if _, ok := p.existingPools[nn]; !ok {
			set := p.desiredPools[nn].set.DeepCopy()
			set.Labels[generationLabel] = generation

			sets = append(sets, set)
		}
	}

	return sortByName(sets)
}

func (p *PoolTracker) ToScaleUp() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if desired, ok := p.desiredPools[nn]; ok {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			if existingReplicas < desiredReplicas {
				// we use the desired set spec here
				set := desired.set.DeepCopy()
				set.Labels[generationLabel] = generation
				set.Spec.Replicas = ptr.To(existingReplicas + 1)
				sets = append(sets, set)
			}
		}
	}

	return sortByName(sets)
}

func (p *PoolTracker) RequiresUpdate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if desired, ok := p.desiredPools[nn]; ok && existing.set.Labels[generationLabel] != generation {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			// we only return sets in which we already have matched replicas
			// since the scale operations handle patching the other statefulsets
			if existingReplicas == desiredReplicas {
				set := desired.set.DeepCopy()
				set.Labels[generationLabel] = generation
				sets = append(sets, set)
			}
		}
	}

	return sortByName(sets)
}

func (p *PoolTracker) ToScaleDown() []*ScaleDownSet {
	sets := []*ScaleDownSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			existing := p.existingPools[nn]
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)

			if existingReplicas != 0 && len(existing.pods) != 0 {
				set := existing.set.DeepCopy()
				set.Labels[generationLabel] = generation
				lastPod := existing.pods[len(existing.pods)-1]

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, &ScaleDownSet{
					StatefulSet: set,
					LastPod:     lastPod.pod.DeepCopy(),
				})
			}
		} else {
			existing, desired := p.existingPools[nn], p.desiredPools[nn]
			existingReplicas, desiredReplicas := ptr.Deref(existing.set.Spec.Replicas, 0), ptr.Deref(desired.set.Spec.Replicas, 0)

			if existingReplicas > desiredReplicas {
				// we use the desired set spec here
				set := desired.set.DeepCopy()
				set.Labels[generationLabel] = generation
				lastPod := existing.pods[len(existing.pods)-1]

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, &ScaleDownSet{
					StatefulSet: set,
					LastPod:     lastPod.pod.DeepCopy(),
				})
			}
		}
	}

	sort.SliceStable(sets, func(i, j int) bool {
		return client.ObjectKeyFromObject(sets[i].StatefulSet).String() < client.ObjectKeyFromObject(sets[j].StatefulSet).String()
	})

	return sets
}

func (p *PoolTracker) ToDelete() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			sets = append(sets, p.existingPools[nn].set.DeepCopy())
		}
	}

	return sortByName(sets)
}

func (p *PoolTracker) PodsToRoll() []*corev1.Pod {
	pods := []*corev1.Pod{}

	for _, existing := range p.existingPools {
		for _, withOrdinals := range existing.pods {
			// the CurrentRevision on the StatefulSet can't be used here due to leveraging onDelete
			if len(existing.revisions) == 0 {
				// we have no revisions, just assume this needs to be rolled
				pods = append(pods, withOrdinals.pod.DeepCopy())
			} else {
				lastRevision := existing.revisions[len(existing.revisions)-1]
				if withOrdinals.pod.Labels[appsv1.StatefulSetRevisionLabel] != lastRevision.Name {
					pods = append(pods, withOrdinals.pod.DeepCopy())
				}
			}
		}
	}

	return pods
}
