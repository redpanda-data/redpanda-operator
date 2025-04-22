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
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// podWithOrdinals is a container for sorting pods
// by their ordinals
type podsWithOrdinals struct {
	ordinal int
	pod     *corev1.Pod
}

// poolWithOrdinals is a container for all of the information
// we need to figure out how to manipulate a given node pool
type poolWithOrdinals struct {
	pods      []*podsWithOrdinals
	set       *appsv1.StatefulSet
	revisions []*appsv1.ControllerRevision
}

// ScaleDownSet holds a reference to a pod in need of decommissioning
// (the last ordinal in a StatefulSet), as well as the StatefulSet
// it is associated with.
type ScaleDownSet struct {
	LastPod     *corev1.Pod
	StatefulSet *appsv1.StatefulSet
}

// PoolTracker tracks the existing and desired node pool state
// for a cluster.
type PoolTracker struct {
	// latestGeneration is the generation of the cluster used
	// to determine whether or not a StatefulSet's definition is
	// out-of-date
	latestGeneration int64
	existingPools    map[types.NamespacedName]*poolWithOrdinals
	desiredPools     map[types.NamespacedName]*poolWithOrdinals
}

// NewPoolTracker creates a new PoolTracker with the given cluster generation.
func NewPoolTracker(generation int64) *PoolTracker {
	return &PoolTracker{
		latestGeneration: generation,
		existingPools:    make(map[types.NamespacedName]*poolWithOrdinals),
		desiredPools:     make(map[types.NamespacedName]*poolWithOrdinals),
	}
}

// ExistingStatefulSets returns a list of the names of the existing StatefulSets tracked by the PoolTracker.
func (p *PoolTracker) ExistingStatefulSets() []string {
	sets := []string{}
	for nn := range p.existingPools {
		sets = append(sets, nn.String())
	}
	return sets
}

// PoolStatuses returns a list of the pool statuses of the existing StatefulSets tracked by the PoolTracker.
func (p *PoolTracker) PoolStatuses() []PoolStatus {
	sets := []PoolStatus{}
	for nn, pool := range p.existingPools {
		desiredReplicas := ptr.Deref(pool.set.Spec.Replicas, 0)
		condemnedReplicas := pool.set.Status.Replicas - desiredReplicas
		if condemnedReplicas < 0 {
			condemnedReplicas = 0
		}
		sets = append(sets, PoolStatus{
			Name:              nn.Name,
			Replicas:          pool.set.Status.Replicas,
			DesiredReplicas:   desiredReplicas,
			ReadyReplicas:     pool.set.Status.ReadyReplicas,
			RunningReplicas:   pool.set.Status.AvailableReplicas,
			UpToDateReplicas:  pool.set.Status.CurrentReplicas,
			OutOfDateReplicas: pool.set.Status.Replicas - pool.set.Status.CurrentReplicas,
			CondemnedReplicas: condemnedReplicas,
		})
	}
	return sets
}

// DesiredStatefulSets returns a list of the names of the desired StatefulSets tracked by the PoolTracker.
func (p *PoolTracker) DesiredStatefulSets() []string {
	sets := []string{}
	for nn := range p.desiredPools {
		sets = append(sets, nn.String())
	}
	return sets
}

// CheckScale checks if scaling operations can proceed based on the current state of pools.
// It returns true if scaling is allowed (i.e. no scaling operation is currently in progress).
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

// ToCreate returns a list of StatefulSets that need to be created.
func (p *PoolTracker) ToCreate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.desiredPools {
		if _, ok := p.existingPools[nn]; !ok {
			set := p.desiredPools[nn].set.DeepCopy()
			if set.Labels == nil {
				set.Labels = map[string]string{}
			}
			set.Labels[generationLabel] = generation
			sets = append(sets, set)
		}
	}

	return sortByName(sets)
}

// ToScaleUp returns a list of StatefulSets that need to be scaled up
// (i.e. existing replicas are less than desired replicas).
func (p *PoolTracker) ToScaleUp() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if desired, ok := p.desiredPools[nn]; ok {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			if existingReplicas < desiredReplicas {
				set := desired.set.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				sets = append(sets, set)
			}
		}
	}

	return sortByName(sets)
}

// RequiresUpdate returns a list of StatefulSets that require an update
// because they have not yet been updated since the last time the owning cluster
// was updated.
func (p *PoolTracker) RequiresUpdate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		labels := existing.set.Labels
		if labels == nil {
			continue
		}
		if desired, ok := p.desiredPools[nn]; ok && labels[generationLabel] != generation {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			// we only return sets in which we already have matched replicas
			// since the scale operations handle patching the other statefulsets
			if existingReplicas == desiredReplicas {
				set := desired.set.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				sets = append(sets, set)
			}
		}
	}

	return sortByName(sets)
}

// ToScaleDown returns a list of ScaleDownSets for StatefulSets
// that need to be scaled down. Each also contains the pod with
// the highest ordinal in the set so it can be decommissioned.
func (p *PoolTracker) ToScaleDown() []*ScaleDownSet {
	sets := []*ScaleDownSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			existing := p.existingPools[nn]
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)

			if existingReplicas != 0 && len(existing.pods) != 0 {
				set := existing.set.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
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
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
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

// ToDelete returns a list of StatefulSets that need to be deleted.
func (p *PoolTracker) ToDelete() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn, existing := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			// extra guard to make sure we don't accidentally delete a
			// statefulset whose pods still need to be decommissioned
			if existingReplicas == 0 && existing.set.Status.Replicas == 0 {
				sets = append(sets, p.existingPools[nn].set.DeepCopy())
			}
		}
	}

	return sortByName(sets)
}

// PodsToRoll returns a list of pods that need to be rolled
// because their association ControllerRevision does not match
// the latest applied to the StatefulSet.
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

// addExisting poolWithOrdinals to the tracker
func (p *PoolTracker) addExisting(pools ...*poolWithOrdinals) {
	for i := range pools {
		p.existingPools[client.ObjectKeyFromObject(pools[i].set)] = pools[i]
	}
}

// addDesired statefulsets to the tracker
func (p *PoolTracker) addDesired(sets ...*appsv1.StatefulSet) {
	for _, set := range sets {
		p.desiredPools[client.ObjectKeyFromObject(set)] = &poolWithOrdinals{set: set.DeepCopy()}
	}
}
