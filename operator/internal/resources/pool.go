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
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const generationLabel = "cluster.redpanda.com/generation"

func sortByName[T client.Object](objs []T) []T {
	slices.SortStableFunc(objs, func(a, b T) int {
		return strings.Compare(client.ObjectKeyFromObject(a).String(), client.ObjectKeyFromObject(b).String())
	})

	return objs
}

func extractOrdinal(name string) (int, error) {
	resourceTokens := strings.Split(name, "-")
	if len(resourceTokens) < 2 {
		return 0, fmt.Errorf("invalid resource name for ordinal fetching: %s", name)
	}

	// grab the last item after the "-"" which should be the ordinal and parse it
	ordinal, err := strconv.Atoi(resourceTokens[len(resourceTokens)-1])
	if err != nil {
		return 0, fmt.Errorf("parsing resource name %q: %w", name, err)
	}

	return ordinal, nil
}

type podsWithOrdinals struct {
	ordinal int
	pod     *corev1.Pod
}

func sortRevisions(controllerRevisions []*appsv1.ControllerRevision) []*appsv1.ControllerRevision {
	// from https://github.com/kubernetes/kubernetes/blob/dd25c6a6cb4ea0be1e304de35de45adeef78b264/pkg/controller/history/controller_history.go#L158
	sort.SliceStable(controllerRevisions, func(i, j int) bool {
		if controllerRevisions[i].Revision == controllerRevisions[j].Revision {
			if controllerRevisions[j].CreationTimestamp.Equal(&controllerRevisions[i].CreationTimestamp) {
				return controllerRevisions[i].Name < controllerRevisions[j].Name
			}
			return controllerRevisions[j].CreationTimestamp.After(controllerRevisions[i].CreationTimestamp.Time)
		}
		return controllerRevisions[i].Revision < controllerRevisions[j].Revision
	})

	return controllerRevisions
}

func sortPodsByOrdinal(pods ...client.Object) ([]*podsWithOrdinals, error) {
	withOrdinals := []*podsWithOrdinals{}
	for _, pod := range pods {
		ordinal, err := extractOrdinal(pod.GetName())
		if err != nil {
			return nil, err
		}
		withOrdinals = append(withOrdinals, &podsWithOrdinals{
			ordinal: ordinal,
			pod:     pod.(*corev1.Pod).DeepCopy(),
		})
	}

	sort.SliceStable(withOrdinals, func(i, j int) bool {
		return withOrdinals[i].ordinal < withOrdinals[j].ordinal
	})

	return withOrdinals, nil
}

type pool struct {
	pods      []*podsWithOrdinals
	set       *appsv1.StatefulSet
	revisions []*appsv1.ControllerRevision
}

func newPool(set *appsv1.StatefulSet, revisions []*appsv1.ControllerRevision, pods ...*podsWithOrdinals) *pool {
	return &pool{
		set:       set,
		pods:      pods,
		revisions: revisions,
	}
}

type PoolManager struct {
	latestGeneration int64
	existingPools    map[types.NamespacedName]*pool
	desiredPools     map[types.NamespacedName]*pool
}

func NewPoolManager(generation int64) *PoolManager {
	return &PoolManager{
		latestGeneration: generation,
		existingPools:    make(map[types.NamespacedName]*pool),
		desiredPools:     make(map[types.NamespacedName]*pool),
	}
}

func (p *PoolManager) ExistingStatefulSets() []string {
	sets := []string{}
	for nn := range p.existingPools {
		sets = append(sets, nn.String())
	}
	return sets
}

func (p *PoolManager) DesiredStatefulSets() []string {
	sets := []string{}
	for nn := range p.desiredPools {
		sets = append(sets, nn.String())
	}
	return sets
}

func (p *PoolManager) AddExisting(set *appsv1.StatefulSet, revisions []*appsv1.ControllerRevision, pods ...client.Object) error {
	withOrdinals, err := sortPodsByOrdinal(pods...)
	if err != nil {
		return err
	}

	p.existingPools[client.ObjectKeyFromObject(set)] = newPool(set, sortRevisions(revisions), withOrdinals...)
	return nil
}

func (p *PoolManager) AddDesired(sets ...*appsv1.StatefulSet) {
	for _, set := range sets {
		p.desiredPools[client.ObjectKeyFromObject(set)] = newPool(set, []*appsv1.ControllerRevision{})
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
		replicas := ptr.Deref(pool.set.Spec.Replicas, 0)
		if replicas != pool.set.Status.Replicas || int(replicas) != len(pool.pods) {
			// we're potentially in the middle of a scaling operation
			return ScaleNotReady
		}
	}

	// at this point all of our sets are stable, but we need to check the cluster
	// health to see whether or not we can do a scale operation
	return ScaleNeedsClusterCheck
}

func (p *PoolManager) ToCreate() []*appsv1.StatefulSet {
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

func (p *PoolManager) ToScaleUp() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; ok {
			existing, desired := p.existingPools[nn], p.desiredPools[nn]
			existingReplicas, desiredReplicas := existing.set.Status.Replicas, desired.set.Status.Replicas

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

func (p *PoolManager) RequiresUpdate() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if desired, ok := p.desiredPools[nn]; ok && existing.set.Labels[generationLabel] != generation {
			set := desired.set.DeepCopy()
			set.Labels[generationLabel] = generation
			sets = append(sets, set)
		}
	}

	return sortByName(sets)
}

type ScaleDownSet struct {
	LastPod     *corev1.Pod
	StatefulSet *appsv1.StatefulSet
}

func (p *PoolManager) ToScaleDown() []*ScaleDownSet {
	sets := []*ScaleDownSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			existing := p.existingPools[nn]
			existingReplicas := existing.set.Status.Replicas

			if existing.set.Status.Replicas != 0 {
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

func (p *PoolManager) ToDelete() []*appsv1.StatefulSet {
	sets := []*appsv1.StatefulSet{}

	for nn := range p.existingPools {
		if _, ok := p.desiredPools[nn]; !ok {
			sets = append(sets, p.existingPools[nn].set.DeepCopy())
		}
	}

	return sortByName(sets)
}

func (p *PoolManager) PodsToRoll() []*corev1.Pod {
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
