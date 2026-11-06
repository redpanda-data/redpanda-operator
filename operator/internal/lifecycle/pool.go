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
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/redpanda-data/common-go/otelutil/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
)

type MulticlusterStatefulSet struct {
	*appsv1.StatefulSet
	clusterName          string
	canonicalClusterName string
}

func (s *MulticlusterStatefulSet) GetCluster() string {
	return s.clusterName
}

func (s *MulticlusterStatefulSet) GetCanonicalClusterName() string {
	return s.canonicalClusterName
}

type MulticlusterPod struct {
	*corev1.Pod
	clusterName          string
	canonicalClusterName string
}

func (p *MulticlusterPod) GetCluster() string {
	return p.clusterName
}

func (p *MulticlusterPod) GetCanonicalClusterName() string {
	return p.canonicalClusterName
}

// newMulticlusterStatefulSet builds a MulticlusterStatefulSet, setting its
// cluster name and canonical cluster name together so callers can't
// accidentally set one without the other.
func newMulticlusterStatefulSet(set *appsv1.StatefulSet, clusterName, canonicalClusterName string) *MulticlusterStatefulSet {
	return &MulticlusterStatefulSet{
		StatefulSet:          set,
		clusterName:          clusterName,
		canonicalClusterName: canonicalClusterName,
	}
}

// newMulticlusterPod builds a MulticlusterPod, setting its cluster name and
// canonical cluster name together so callers can't accidentally set one
// without the other.
func newMulticlusterPod(pod *corev1.Pod, clusterName, canonicalClusterName string) *MulticlusterPod {
	return &MulticlusterPod{
		Pod:                  pod,
		clusterName:          clusterName,
		canonicalClusterName: canonicalClusterName,
	}
}

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
	set       *MulticlusterStatefulSet
	revisions []*appsv1.ControllerRevision
	// brokerBacked marks a pool synthesized from Broker CRs (see
	// fetchBrokerBackedPools) rather than a live StatefulSet. Excluded from
	// every StatefulSet mutation planner and revision-based roll helper.
	brokerBacked bool
}

// ScaleDownSet holds a reference to a pod in need of decommissioning
// (the last ordinal in a StatefulSet), as well as the StatefulSet
// it is associated with.
type ScaleDownSet struct {
	LastPod     *corev1.Pod
	StatefulSet *MulticlusterStatefulSet
}

func (s *ScaleDownSet) GetCluster() string {
	return s.StatefulSet.clusterName
}

func (s *ScaleDownSet) GetNamespace() string {
	return s.LastPod.Namespace
}

func (s *ScaleDownSet) GetName() string {
	return s.LastPod.Name
}

func (s *ScaleDownSet) GetCanonicalClusterName() string {
	return s.StatefulSet.canonicalClusterName
}

// ClusterNamespacedName is used as the map key correlating a pool's existing
// and desired state (see PoolTracker.existingPools/desiredPools). The existing
// and desired MulticlusterStatefulSet for the same real pool must therefore
// report the same CanonicalClusterName — if one side drops or mis-derives it,
// the two are treated as unrelated pools (spurious create + orphaned delete)
// instead of being correlated.
type ClusterNamespacedName struct {
	Cluster              string
	Namespace            string
	Name                 string
	CanonicalClusterName string
}

func (c ClusterNamespacedName) String() string {
	return fmt.Sprintf("%s/%s/%s", c.CanonicalClusterName, c.Namespace, c.Name)
}

func objectKeyFromObject(o clusterObject) ClusterNamespacedName {
	return ClusterNamespacedName{
		Cluster:              o.GetCluster(),
		Namespace:            o.GetNamespace(),
		Name:                 o.GetName(),
		CanonicalClusterName: o.GetCanonicalClusterName(),
	}
}

// PoolTracker tracks the existing and desired node pool state
// for a cluster.
type PoolTracker struct {
	// latestGeneration is the generation of the cluster used
	// to determine whether or not a StatefulSet's definition is
	// out-of-date
	latestGeneration int64
	existingPools    map[ClusterNamespacedName]*poolWithOrdinals
	desiredPools     map[ClusterNamespacedName]*poolWithOrdinals
	// observedClusters records which cluster contexts the operator had
	// full visibility into for this reconcile — i.e. it successfully
	// fetched both the NodePool list AND the StatefulSet list for that
	// cluster without falling back to the probe-unreachable skip.
	// ToScaleDown's "existing pool with no desired counterpart" branch
	// requires the owning cluster to be observed before treating absence
	// as intent to drain — otherwise a transient fetch failure (or a
	// probe-state flip mid-reconcile) on a partitioned peer would be
	// indistinguishable from a real NodePool deletion and would trigger
	// an unintended decommission.
	observedClusters map[string]bool
	// unseenClusters are clusters whose pods this reconcile couldn't list:
	// their brokers may run though no pod here shows it.
	unseenClusters         []string
	poolGenerationLabelKey string
}

// NewPoolTracker creates a new PoolTracker with the given cluster generation.
func NewPoolTracker(generation int64, useBrokerPool bool) *PoolTracker {
	poolGenerationLabelKey := nodePoolGenerationLabel
	if useBrokerPool {
		poolGenerationLabelKey = BrokerPoolGenerationLabel
	}
	return &PoolTracker{
		poolGenerationLabelKey: poolGenerationLabelKey,
		latestGeneration:       generation,
		existingPools:          make(map[ClusterNamespacedName]*poolWithOrdinals),
		desiredPools:           make(map[ClusterNamespacedName]*poolWithOrdinals),
		observedClusters:       make(map[string]bool),
	}
}

// MarkClusterObserved records that the operator had complete visibility into
// the named cluster's NodePool and StatefulSet state during this reconcile.
// "no desired counterpart" decisions in ToScaleDown / ToDelete are gated on
// this — see the comment on PoolTracker.observedClusters.
func (p *PoolTracker) MarkClusterObserved(clusterName string) {
	p.observedClusters[clusterName] = true
}

// MarkClusterUnseen records that the named cluster's pods couldn't be listed
// during this reconcile.
func (p *PoolTracker) MarkClusterUnseen(clusterName string) {
	if !slices.Contains(p.unseenClusters, clusterName) {
		p.unseenClusters = append(p.unseenClusters, clusterName)
	}
}

// IsClusterObserved returns whether the cluster's pool state was fully fetched
// during this reconcile.
func (p *PoolTracker) IsClusterObserved(clusterName string) bool {
	return p.observedClusters[clusterName]
}

// ExistingStatefulSets returns a list of the names of the existing StatefulSets tracked by the PoolTracker.
func (p *PoolTracker) ExistingStatefulSets() []string {
	sets := []string{}
	for nn := range p.existingPools {
		sets = append(sets, nn.String())
	}
	return sets
}

// AnyReady returns true if any of the existing pools have a ready replica.
func (p *PoolTracker) AnyReady() bool {
	for _, pool := range p.existingPools {
		if pool.set.Status.ReadyReplicas > 0 {
			return true
		}
	}
	return false
}

func (p *PoolTracker) AllZero() bool {
	for _, pool := range p.desiredPools {
		if ptr.Deref(pool.set.Spec.Replicas, 0) > 0 {
			return false
		}
	}
	return true
}

// ScaledUpButNoneReady reports that the cluster is expected to be running (at
// least one desired pool is scaled above zero) but no existing pool has a Ready
// replica yet. Reconcilers use it to poll again quickly while the cluster comes
// up.
//
// It must only be consulted to decide the reconcile's REQUEUE cadence, applied
// at the very end of the reconcile — never to abort the reconcile before the
// cluster-level recovery steps (maintenance-mode clear, stale-disk wipe,
// decommission). Those steps exist precisely to run while brokers are unready:
// during a full region outage every surviving broker's sidecar readiness probe
// fails (under-replicated partitions), so no pool has a Ready replica and this
// returns true for the entire outage. Gating the recovery steps on it would
// deadlock — the operator could never clear stuck maintenance, wipe a
// bad_rejoin disk, or decommission a lost broker while it mattered most.
//
// Note this only removes the READINESS gate on those steps. Other, narrower
// gates in the reconcile can still defer them for a pass — notably CheckScale
// (an active scale/roll) and initAdminClient (no reachable broker admin API);
// recovery in those windows relies on eventual consistency across reconciles.
func (p *PoolTracker) ScaledUpButNoneReady() bool {
	return !(p.AnyReady() || p.AllZero())
}

// PoolStatuses returns a list of the pool statuses of the existing StatefulSets tracked by the PoolTracker.
// It also includes desired pools that don't yet exist so the status reflects intent to create them.
func (p *PoolTracker) PoolStatuses() []PoolStatus {
	sets := []PoolStatus{}
	seen := map[ClusterNamespacedName]struct{}{}

	for nn, pool := range p.existingPools {
		seen[nn] = struct{}{}
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
			UpToDateReplicas:  pool.set.Status.UpdatedReplicas,
			OutOfDateReplicas: pool.set.Status.Replicas - pool.set.Status.UpdatedReplicas,
			CondemnedReplicas: condemnedReplicas,
		})
	}

	// Include desired pools that don't yet exist so the user can see
	// pending pool creation in the status.
	for nn, pool := range p.desiredPools {
		if _, ok := seen[nn]; ok {
			continue
		}
		sets = append(sets, PoolStatus{
			Name:            nn.Name,
			DesiredReplicas: ptr.Deref(pool.set.Spec.Replicas, 0),
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

// DesiredPools returns a deep copy of every desired pool's rendered
// StatefulSet.
func (p *PoolTracker) DesiredPools() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}
	for _, pool := range p.desiredPools {
		mcset := pool.set
		sets = append(sets, newMulticlusterStatefulSet(mcset.DeepCopy(), mcset.clusterName, mcset.canonicalClusterName))
	}
	return sortByNameAndCluster(sets)
}

// BrokerBackedPoolsWithoutDesired returns broker-backed pools with no
// desired counterpart (removed from the spec). Gated on cluster observation
// like ToScaleDown, so a fetch failure never reads as intent to drain.
func (p *PoolTracker) BrokerBackedPoolsWithoutDesired() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}
	for nn, existing := range p.existingPools {
		if !existing.brokerBacked {
			continue
		}
		if _, ok := p.desiredPools[nn]; ok {
			continue
		}
		if !p.observedClusters[nn.Cluster] {
			continue
		}
		mcset := existing.set
		sets = append(sets, newMulticlusterStatefulSet(mcset.DeepCopy(), mcset.clusterName, mcset.canonicalClusterName))
	}
	return sortByNameAndCluster(sets)
}

// CheckScale checks if scaling operations can proceed based on the current state of pools.
// It returns true if scaling is allowed (i.e. no scaling operation is currently in progress).
func (p *PoolTracker) CheckScale(ctx context.Context) bool {
	// if we have no existing pools
	if len(p.existingPools) == 0 {
		return true
	}

	for _, pool := range p.existingPools {
		replicas := ptr.Deref(pool.set.Spec.Replicas, 0)
		if replicas != pool.set.Status.Replicas || int(replicas) != len(pool.pods) {
			// we're potentially in the middle of a scaling operation
			log.FromContext(ctx).V(log.DebugLevel).Info(
				"Checking pool scale returns false",
				"replicas", replicas,
				"pool.set.Status.Replicas", pool.set.Status.Replicas,
				"len(pool.pods)", len(pool.pods),
				"poolName", pool.set.GetName(),
				"clusterName", pool.set.GetCanonicalClusterName(),
			)
			return false
		}
	}

	return true
}

// ToCreate returns a list of StatefulSets that need to be created.
func (p *PoolTracker) ToCreate() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.desiredPools {
		if _, ok := p.existingPools[nn]; !ok {
			mcset := p.desiredPools[nn].set
			set := mcset.DeepCopy()
			if set.Labels == nil {
				set.Labels = map[string]string{}
			}
			set.Labels[generationLabel] = generation
			sets = append(sets, newMulticlusterStatefulSet(set, mcset.clusterName, mcset.canonicalClusterName))
		}
	}

	return sortByNameAndCluster(sets)
}

// ToScaleUp returns a list of StatefulSets that need to be scaled up
// (i.e. existing replicas are less than desired replicas).
func (p *PoolTracker) ToScaleUp() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if existing.brokerBacked {
			continue
		}
		if desired, ok := p.desiredPools[nn]; ok {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			if existingReplicas < desiredReplicas {
				mcset := desired.set
				set := mcset.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				sets = append(sets, newMulticlusterStatefulSet(set, mcset.clusterName, mcset.canonicalClusterName))
			}
		}
	}

	return sortByNameAndCluster(sets)
}

// RequiresUpdate returns a list of StatefulSets that require an update
// because they have not yet been updated since the last time the owning cluster
// was updated.
func (p *PoolTracker) RequiresUpdate() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn, existing := range p.existingPools {
		if existing.brokerBacked {
			continue
		}
		desired, ok := p.desiredPools[nn]
		if !ok {
			continue
		}

		labels := existing.set.Labels
		if labels == nil {
			continue
		}

		outOfDateGeneration := labels[generationLabel] != generation
		outOfDateConfigVersion := labels[configVersionLabel] != desired.set.Labels[configVersionLabel]
		outOfDateNodePool := labels[p.poolGenerationLabelKey] != desired.set.Labels[p.poolGenerationLabelKey]
		if outOfDateGeneration || outOfDateConfigVersion || outOfDateNodePool {
			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			desiredReplicas := ptr.Deref(desired.set.Spec.Replicas, 0)

			// we only return sets in which we already have matched replicas
			// since the scale operations handle patching the other statefulsets
			if existingReplicas == desiredReplicas {
				mcset := desired.set
				set := mcset.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				sets = append(sets, newMulticlusterStatefulSet(set, mcset.clusterName, mcset.canonicalClusterName))
			}
		}
	}

	return sortByNameAndCluster(sets)
}

// ToScaleDown returns a list of ScaleDownSets for StatefulSets
// that need to be scaled down. Each also contains the pod with
// the highest ordinal in the set so it can be decommissioned.
func (p *PoolTracker) ToScaleDown() []*ScaleDownSet {
	sets := []*ScaleDownSet{}

	generation := strconv.FormatInt(p.latestGeneration, 10)

	for nn := range p.existingPools {
		if p.existingPools[nn].brokerBacked {
			continue
		}
		if _, ok := p.desiredPools[nn]; !ok {
			existing := p.existingPools[nn]

			// Refuse to interpret "no desired counterpart" as user intent to
			// drain unless we *actually saw* the owning cluster's NodePool
			// list in this reconcile. Without this guard, a probe flip /
			// transient fetch failure on a partitioned peer makes "we
			// couldn't see your NodePool" look identical to "the user
			// removed your NodePool" — and the operator happily decommissions
			// healthy brokers. The user-facing rule we honor: decommission
			// only on explicit scale-down (the else branch below) or when
			// we've confirmed the pool is genuinely gone from a cluster we
			// could observe.
			if !p.observedClusters[nn.Cluster] {
				continue
			}

			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)

			if existingReplicas != 0 && len(existing.pods) != 0 {
				mcset := existing.set
				set := mcset.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				lastPod := existing.pods[len(existing.pods)-1]

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, &ScaleDownSet{
					StatefulSet: newMulticlusterStatefulSet(set, mcset.clusterName, mcset.canonicalClusterName),
					LastPod:     lastPod.pod.DeepCopy(),
				})
			}
		} else {
			existing, desired := p.existingPools[nn], p.desiredPools[nn]
			existingReplicas, desiredReplicas := ptr.Deref(existing.set.Spec.Replicas, 0), ptr.Deref(desired.set.Spec.Replicas, 0)

			// With no pod there's none to decommission, as for a removed pool.
			if existingReplicas > desiredReplicas && len(existing.pods) != 0 {
				// we use the desired set spec here
				mcset := existing.set
				set := mcset.DeepCopy()
				if set.Labels == nil {
					set.Labels = map[string]string{}
				}
				set.Labels[generationLabel] = generation
				lastPod := existing.pods[len(existing.pods)-1]

				set.Spec.Replicas = ptr.To(existingReplicas - 1)
				sets = append(sets, &ScaleDownSet{
					StatefulSet: newMulticlusterStatefulSet(set, mcset.clusterName, mcset.canonicalClusterName),
					LastPod:     lastPod.pod.DeepCopy(),
				})
			}
		}
	}
	return sortByNameAndCluster(sets)
}

// ToDelete returns a list of StatefulSets that need to be deleted.
func (p *PoolTracker) ToDelete() []*MulticlusterStatefulSet {
	sets := []*MulticlusterStatefulSet{}

	for nn, existing := range p.existingPools {
		if existing.brokerBacked {
			continue
		}
		if _, ok := p.desiredPools[nn]; !ok {
			// Same observation guard as ToScaleDown: never delete a
			// StatefulSet for a cluster we didn't observe — "no desired
			// counterpart" might mean the NodePool was removed (delete
			// is correct) or it might mean we couldn't see the cluster
			// (delete is wrong and irrecoverable). Only act when sure.
			if !p.observedClusters[nn.Cluster] {
				continue
			}

			existingReplicas := ptr.Deref(existing.set.Spec.Replicas, 0)
			// extra guard to make sure we don't accidentally delete a
			// statefulset whose pods still need to be decommissioned
			if existingReplicas == 0 && existing.set.Status.Replicas == 0 {
				// 				sort.SliceStable(sets, func(i, j int) bool {
				// 	return client.ObjectKeyFromObject(sets[i].StatefulSet).String() < client.ObjectKeyFromObject(sets[j].StatefulSet).String()
				// })
				//nolint:laconiccomments

				mcset := p.existingPools[nn].set
				sets = append(sets, newMulticlusterStatefulSet(mcset.DeepCopy(), mcset.clusterName, mcset.canonicalClusterName))
			}
		}
	}

	return sortByNameAndCluster(sets)
}

// HasRecentlyReplacedPods returns true if any existing pod has the latest
// revision (i.e., was recently recreated during a rolling restart) but is not
// yet fully ready, or if any pod is still terminating. This is a narrower
// check than "all pods ready" — it specifically targets the window between a
// pod replacement and the new pod's readiness probe passing, during which the
// Redpanda health API has not yet caught up.
func (p *PoolTracker) HasRecentlyReplacedPods() bool {
	for _, pool := range p.existingPools {
		if pool.brokerBacked {
			continue
		}
		latestRevision := ""
		if len(pool.revisions) > 0 {
			latestRevision = pool.revisions[len(pool.revisions)-1].Name
		}

		for _, withOrdinals := range pool.pods {
			pod := withOrdinals.pod

			// A pod being deleted is always a reason to wait.
			if pod.DeletionTimestamp != nil {
				return true
			}

			// Only check pods that already have the latest revision — these
			// are the ones that were just replaced by a prior roll pass.
			if latestRevision == "" || pod.Labels[appsv1.StatefulSetRevisionLabel] != latestRevision {
				continue
			}

			if pod.Status.Phase != corev1.PodRunning {
				return true
			}

			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				return true
			}
		}
	}
	return false
}

// ExistingPods returns every existing pod across all pools as MulticlusterPods,
// regardless of StatefulSet revision. Unlike PodsToRoll it does not filter on
// revision — both the maintenance-mode cleaner and the stale-disk wipe step must
// inspect pods that are not in the roll set (e.g. a persistently-down broker whose
// pod is Pending/not-Ready, or a decommissioned broker stuck in a bad_rejoin
// crashloop).
func (p *PoolTracker) ExistingPods() []*MulticlusterPod {
	pods := []*MulticlusterPod{}
	for _, existing := range p.existingPools {
		for _, withOrdinals := range existing.pods {
			pods = append(pods, newMulticlusterPod(withOrdinals.pod.DeepCopy(), existing.set.clusterName, existing.set.canonicalClusterName))
		}
	}
	return pods
}

// PodsToRoll returns the pods on a revision older than their StatefulSet's
// latest, in PoolPods' order, so passes racing a lagging cache roll the same
// pod.
func (p *PoolTracker) PodsToRoll() []*MulticlusterPod {
	var pods []*MulticlusterPod
	for _, pod := range p.PoolPods() {
		if !pod.Current {
			pods = append(pods, pod.MulticlusterPod)
		}
	}
	return pods
}

// NextFailedPod returns the outdated pod whose broker cannot start that may be
// replaced now, the first in PoolPods' order that failedpods allows and that
// doesn't wait, or nil and why not ("" with no such pod). Nothing is replaced while a pod is being
// added or removed, or a StatefulSet change is still to be applied. Brokers in
// pools Broker CRs manage, and in clusters whose pods couldn't be listed, still
// count as peers.
func (p *PoolTracker) NextFailedPod(ctx context.Context, ask failedpods.Ask) (*MulticlusterPod, string) {
	poolPods := p.PoolPods()
	view := make([]failedpods.Pod, len(poolPods))
	for i, pod := range poolPods {
		view[i] = failedpods.Pod{Pod: pod.Pod, Pool: pod.Pool, Current: pod.Current, Removing: pod.Removing}
	}
	if !slices.ContainsFunc(view, failedpods.MayBeCandidate) {
		return nil, ""
	}
	if !p.CheckScale(ctx) {
		return nil, "a pod is being added or removed"
	}
	// reconcilePools patches these this pass, after the snapshot was taken.
	if len(p.ToCreate()) > 0 || len(p.ToScaleUp()) > 0 || len(p.RequiresUpdate()) > 0 {
		return nil, "StatefulSets are being updated"
	}

	// Broker CRs roll their own pods: here they only vouch, and hold while one
	// is replaced.
	all := append(slices.Clone(view), p.brokerBackedPods()...)
	peers := failedpods.AskPeers(ctx, all, len(p.unseenClusters) > 0, ask)
	reason := "a peer sees each candidate's broker alive, or no broker that may answer did"
	held := false
	for i, pod := range view {
		if !failedpods.Candidate(pod, peers) || failedpods.Waits(view, pod, func(failedpods.Pod) bool { return true }) {
			continue
		}
		hold := failedpods.Hold(all, pod, peers)
		if hold == "" {
			return poolPods[i].MulticlusterPod, ""
		}
		if !held {
			reason, held = fmt.Sprintf("%s: %s", pod.Name, hold), true
		}
	}
	return nil, reason
}

// brokerBackedPods returns the pods of pools synthesized from Broker CRs, as
// current, so never a candidate here.
func (p *PoolTracker) brokerBackedPods() []failedpods.Pod {
	var pods []failedpods.Pod
	for nn, existing := range p.existingPools {
		if !existing.brokerBacked {
			continue
		}
		for _, withOrdinals := range existing.pods {
			pods = append(pods, failedpods.Pod{Pod: withOrdinals.pod, Pool: nn.String(), Current: true})
		}
	}
	slices.SortFunc(pods, func(a, b failedpods.Pod) int { return cmp.Or(cmp.Compare(a.Pool, b.Pool), cmp.Compare(a.Name, b.Name)) })
	return pods
}

// PoolPod is an existing pod with the pool it belongs to, whether it runs
// that pool's latest revision, and whether a scale-down is removing it.
type PoolPod struct {
	*MulticlusterPod
	Pool     string
	Current  bool
	Removing bool
}

// PoolPods returns the pods of every StatefulSet-managed pool. The
// StatefulSet's CurrentRevision can't be used under OnDelete, so a pod is
// outdated when it runs a cached ControllerRevision other than the latest. One
// whose revision can't be shown outdated counts as current: with no revisions
// cached, an adopted pod without a revision label (kube labels pods only at
// creation, and a Broker-CR rollback hands them over), or one on a revision
// newer than the cache.
func (p *PoolTracker) PoolPods() []PoolPod {
	var pods []PoolPod

	for nn, existing := range p.existingPools {
		if existing.brokerBacked {
			continue
		}
		var latest string
		known := map[string]bool{}
		for _, revision := range existing.revisions {
			known[revision.Name] = true
			latest = revision.Name
		}
		// Ordinals from here up are removed by a scale-down, or all of them
		// when the pool is gone from a cluster we observed (as ToScaleDown).
		removeFrom := math.MaxInt
		if desired, ok := p.desiredPools[nn]; ok {
			removeFrom = int(ptr.Deref(desired.set.Spec.Replicas, 0))
		} else if p.observedClusters[nn.Cluster] {
			removeFrom = 0
		}
		for _, withOrdinals := range existing.pods {
			revision := withOrdinals.pod.Labels[appsv1.StatefulSetRevisionLabel]
			pods = append(pods, PoolPod{
				MulticlusterPod: newMulticlusterPod(withOrdinals.pod.DeepCopy(), existing.set.clusterName, existing.set.canonicalClusterName),
				Pool:            nn.String(),
				Current:         revision == latest || !known[revision],
				Removing:        withOrdinals.ordinal >= removeFrom,
			})
		}
	}

	// Stable across passes, so passes racing a lagging cache pick the same pod.
	return sortByNameAndCluster(pods)
}

// PodEndpoint holds the information needed to create an EndpointSlice
// for a pod in a multicluster stretch cluster.
type PodEndpoint struct {
	// Name is the pod name (matches the per-pod Service name, e.g. "pool-0-0").
	Name string
	// IP is the pod's IP address.
	IP string
	// Cluster is the Kubernetes cluster the pod runs on.
	Cluster string
	// Ready indicates whether the pod's readiness probe is passing.
	Ready bool
}

// PodEndpoints returns the IP and cluster information for all existing pods
// across all clusters. This is used by the controller to manage EndpointSlices
// for cross-cluster per-pod Services in flat network mode.
func (p *PoolTracker) PodEndpoints(ctx context.Context) []PodEndpoint {
	logger := log.FromContext(ctx).WithName("PoolTracker.PodEndpoints")
	var endpoints []PodEndpoint
	// Per-cluster tallies so we can tell at a glance which cluster is
	// contributing zero pods to the rendered state — the usual cause of
	// flat-mode cross-cluster Endpoints churn.
	clusterPodCount := map[string]int{}
	clusterReadyCount := map[string]int{}
	clusterNoIPCount := map[string]int{}
	for _, pool := range p.existingPools {
		clusterPodCount[pool.set.clusterName] += len(pool.pods)
		for _, pod := range pool.pods {
			if pod.pod.Status.PodIP == "" {
				clusterNoIPCount[pool.set.clusterName]++
				logger.V(log.TraceLevel).Info(
					"pod has no PodIP yet, skipping",
					"cluster", pool.set.clusterName,
					"pod", pod.pod.Name,
					"phase", pod.pod.Status.Phase,
				)
				continue
			}
			ready := false
			for _, cond := range pod.pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if ready {
				clusterReadyCount[pool.set.clusterName]++
			}
			endpoints = append(endpoints, PodEndpoint{
				Name:    pod.pod.Name,
				IP:      pod.pod.Status.PodIP,
				Cluster: pool.set.clusterName,
				Ready:   ready,
			})
		}
	}
	logger.V(log.TraceLevel).Info(
		"gathered pod endpoints",
		"total", len(endpoints),
		"existingPools", len(p.existingPools),
		"clusterPodCount", clusterPodCount,
		"clusterReadyCount", clusterReadyCount,
		"clusterNoIPCount", clusterNoIPCount,
	)
	return endpoints
}

// addExisting poolWithOrdinals to the tracker
func (p *PoolTracker) addExisting(pools ...*poolWithOrdinals) {
	for i := range pools {
		p.existingPools[objectKeyFromObject(pools[i].set)] = pools[i]
	}
}

// addDesired statefulsets to the tracker
func (p *PoolTracker) addDesired(sets ...*MulticlusterStatefulSet) {
	for _, set := range sets {
		p.desiredPools[objectKeyFromObject(set)] = &poolWithOrdinals{set: newMulticlusterStatefulSet(set.DeepCopy(), set.clusterName, set.canonicalClusterName)}
	}
}
