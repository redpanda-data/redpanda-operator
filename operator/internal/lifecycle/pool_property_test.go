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
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// TestPoolTrackerProperties checks the StatefulSet planners against random
// pool states. Each property is its own check: rapid stops a check at its
// first failure, which would hide the others.
func TestPoolTrackerProperties(t *testing.T) {
	ctx := t.Context()

	t.Run("ScaleDownRemovesOneBrokerAtATime", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		removing := map[string]bool{}
		for _, pod := range p.PoolPods() {
			removing[pod.GetCluster()+"/"+pod.Name] = pod.Removing
		}

		for _, s := range p.ToScaleDown() {
			nn := objectKeyFromObject(s.StatefulSet)
			existing := p.existingPools[nn]
			to := ptr.Deref(s.StatefulSet.Spec.Replicas, 0)

			require.False(t, existing.brokerBacked)
			require.Equal(t, ptr.Deref(existing.set.Spec.Replicas, 0)-1, to)
			if desired, ok := p.desiredPools[nn]; ok {
				require.GreaterOrEqual(t, to, ptr.Deref(desired.set.Spec.Replicas, 0))
			} else {
				require.True(t, p.IsClusterObserved(nn.Cluster), "drained %s, absent from a cluster that wasn't observed", nn)
			}

			// Only a settled pool pins which pod the StatefulSet removes; the
			// controller re-checks CheckScale before acting on a plan.
			if p.CheckScale(ctx) {
				require.Equal(t, fmt.Sprintf("%s-%d", nn.Name, to), s.LastPod.Name, "decommissions a pod the StatefulSet keeps")
				require.True(t, removing[nn.Cluster+"/"+s.LastPod.Name], "failed-pod replacement may replace %s mid-decommission", s.LastPod.Name)
			}
		}
	}))

	t.Run("DeleteOnlyEmptyPoolsGoneFromAnObservedCluster", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		for _, s := range p.ToDelete() {
			nn := objectKeyFromObject(s)
			existing := p.existingPools[nn]
			_, desired := p.desiredPools[nn]

			require.False(t, existing.brokerBacked)
			require.False(t, desired)
			require.True(t, p.IsClusterObserved(nn.Cluster))
			require.Zero(t, ptr.Deref(existing.set.Spec.Replicas, 0))
			require.Zero(t, existing.set.Status.Replicas)
		}
	}))

	t.Run("AtMostOneMutationPerPool", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		planned := map[ClusterNamespacedName]bool{}
		for _, s := range slices.Concat(p.ToCreate(), p.ToScaleUp(), p.RequiresUpdate(), statefulSets(p.ToScaleDown()), p.ToDelete()) {
			nn := objectKeyFromObject(s)
			require.False(t, planned[nn], "%s planned for two mutations in one pass", nn)
			planned[nn] = true
		}
	}))

	t.Run("UnobservingAClusterNeverAddsDestructivePlans", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		for cluster := range p.observedClusters {
			blind := *p
			blind.observedClusters = maps.Clone(p.observedClusters)
			delete(blind.observedClusters, cluster)
			require.Subset(t, destructive(p), destructive(&blind), "unobserving %s", cluster)
		}
	}))

	t.Run("EveryScaleDownDecommissionsOneOfItsPods", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		for _, s := range p.ToScaleDown() {
			existing := p.existingPools[objectKeyFromObject(s.StatefulSet)]
			require.NotNil(t, s.LastPod, "%s scales down with no pod to decommission", objectKeyFromObject(s.StatefulSet))
			require.True(t, slices.ContainsFunc(existing.pods, func(pod *podsWithOrdinals) bool { return pod.pod.Name == s.LastPod.Name }),
				"%s decommissions %s, not one of its pods", objectKeyFromObject(s.StatefulSet), s.LastPod.Name)
		}
	}))

	// One definition of outdated serves the roll and failed-pod replacement:
	// a pod on a cached ControllerRevision older than the latest.
	t.Run("RollsExactlyThePodsOnAnOlderCachedRevision", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		var want []string
		for _, pool := range p.existingPools {
			if pool.brokerBacked || len(pool.revisions) == 0 {
				continue
			}
			latest := pool.revisions[len(pool.revisions)-1].Name
			for _, pod := range pool.pods {
				revision := pod.pod.Labels[appsv1.StatefulSetRevisionLabel]
				if revision != latest && slices.ContainsFunc(pool.revisions, func(r *appsv1.ControllerRevision) bool { return r.Name == revision }) {
					want = append(want, pool.set.GetCluster()+"/"+pod.pod.Name)
				}
			}
		}

		var rolled, outdated []string
		for _, pod := range p.PodsToRoll() {
			rolled = append(rolled, pod.GetCluster()+"/"+pod.Name)
		}
		for _, pod := range p.PoolPods() {
			if !pod.Current {
				outdated = append(outdated, pod.GetCluster()+"/"+pod.Name)
			}
		}
		require.ElementsMatch(t, want, rolled)
		require.ElementsMatch(t, want, outdated)
	}))

	// Passes racing a lagging cache must pick the same pod, whatever order the
	// pools were tracked in.
	t.Run("PodsComeInADeterministicOrder", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		var rolled, pods []string
		for _, pod := range p.PodsToRoll() {
			rolled = append(rolled, clusterObjectKey(pod))
		}
		for _, pod := range p.PoolPods() {
			pods = append(pods, clusterObjectKey(pod))
		}
		require.True(t, slices.IsSorted(rolled), "rolls in %v", rolled)
		require.True(t, slices.IsSorted(pods), "lists pods in %v", pods)
	}))

	t.Run("RemovingIsWhatAScaleDownRemoves", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		seen := map[string]int{}
		for _, pod := range p.PoolPods() {
			key := pod.GetCluster() + "/" + pod.Name
			seen[key]++
			nn := ClusterNamespacedName{Cluster: pod.GetCluster(), CanonicalClusterName: pod.GetCanonicalClusterName(), Namespace: pod.Namespace, Name: pod.Pool[strings.LastIndex(pod.Pool, "/")+1:]}
			ordinal, err := extractOrdinal(pod.Name)
			require.NoError(t, err)
			want := p.IsClusterObserved(nn.Cluster)
			if desired, ok := p.desiredPools[nn]; ok {
				want = ordinal >= int(ptr.Deref(desired.set.Spec.Replicas, 0))
			}
			require.Equal(t, want, pod.Removing, "%s", key)
		}
		for _, pool := range p.existingPools {
			for _, pod := range pool.pods {
				require.Equal(t, !pool.brokerBacked, seen[pool.set.GetCluster()+"/"+pod.pod.Name] == 1, "%s/%s listed %d times", pool.set.GetCluster(), pod.pod.Name, seen[pool.set.GetCluster()+"/"+pod.pod.Name])
			}
		}
	}))

	t.Run("NextFailedPodIsAnOutdatedPodThatCannotStart", rapid.MakeCheck(func(t *rapid.T) {
		p := genSettledTracker(t)
		pod, _ := p.NextFailedPod(ctx, genAnswers(t, p))
		if pod == nil {
			return
		}
		i := slices.IndexFunc(p.PoolPods(), func(o PoolPod) bool { return o.GetCluster() == pod.GetCluster() && o.Name == pod.Name })
		require.GreaterOrEqual(t, i, 0, "replaces %s, not a pool's pod", pod.Name)
		picked := p.PoolPods()[i]
		require.False(t, picked.Current, "%s is already on its template", pod.Name)
		require.False(t, picked.Removing, "a scale-down removes %s", pod.Name)
		require.Nil(t, pod.DeletionTimestamp, "%s is terminating", pod.Name)
		require.True(t, utils.ContainerFailingToStart(pod.Pod, redpandachart.RedpandaContainerName) || utils.ContainerHung(pod.Pod, redpandachart.RedpandaContainerName),
			"%s's broker can start", pod.Name)
	}))

	// Replacing one that runs shows its template does; replacing one that
	// can't be scheduled can't, and holds the rest: it waits through every
	// phase of the other's crash loop, not just those it's a candidate in.
	t.Run("ACandidateThatCanRunGoesFirst", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 3)
		pods := p.existingPools[ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "pa"}].pods
		pods[0].pod.Status = newPod(pods[0].pod.Name, "unschedulable").Status
		pods[2].pod.Labels[appsv1.StatefulSetRevisionLabel] = "pa-rev-1"
		phase := rapid.SampledFrom([]string{"crash looping", "between crashes", "image pull failing"}).Draw(t, "pa-2 phase")
		pods[2].pod.Status = newPod(pods[2].pod.Name, phase).Status
		if phase == "between crashes" {
			// Running, it answers as pa-1 does.
			pools := ask
			ask = func(ctx context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
				if pod.UID == pods[2].pod.UID {
					return pools(ctx, pods[1].pod)
				}
				return pools(ctx, pod)
			}
		}
		pod, reason := p.NextFailedPod(ctx, ask)
		if phase == "between crashes" {
			// Another pod the rule allows may go; pa-0 mustn't.
			require.False(t, pod != nil && pod.Name == "pa-0", "replaced pa-0, which can't be scheduled, while pa-2 is between crashes")
			return
		}
		require.NotNil(t, pod, "held: %s", reason)
		require.Equal(t, "pa-2", pod.Name)
	}))

	// A failing template in one pool mustn't stop a replacement in another.
	t.Run("AHeldCandidateDoesntStopOneTheRuleAllows", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 2)
		addDecidedPool(t, p, "p0", "crash looping", 2)
		// Or one with no candidate at all, listed first.
		if rapid.Bool().Draw(t, "p0-0 starting") {
			pod := p.existingPools[ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "p0"}].pods[0].pod
			pod.Status.ContainerStatuses = newPod(pod.Name, "starting").Status.ContainerStatuses
		}
		pod, reason := p.NextFailedPod(ctx, ask)
		require.NotNil(t, pod, "held: %s", reason)
		require.Equal(t, "pa-0", pod.Name)
	}))

	// Each of these starts from a pool whose pod the rule replaces, so a hold
	// is the change's doing.
	t.Run("NothingIsReplacedWhileThePoolsAreChanging", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 2)
		requireReplaces(t, p, ask)
		pool := ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "pa"}
		switch change := rapid.SampledFrom([]string{"create", "scale up", "update"}).Draw(t, "change"); change {
		case "create":
			cluster := rapid.SampledFrom(propertyClusters).Draw(t, "new pool's cluster")
			p.addDesired(genStatefulSet(t, cluster+"/pc", cluster, "pc"))
		case "scale up":
			desired := p.desiredPools[pool].set
			desired.Spec.Replicas = ptr.To(ptr.Deref(desired.Spec.Replicas, 0) + 1)
		case "update":
			p.existingPools[pool].set.Labels[generationLabel] = "0"
		}
		require.True(t, p.CheckScale(ctx))
		require.NotEmpty(t, slices.Concat(p.ToCreate(), p.ToScaleUp(), p.RequiresUpdate()))
		pod, _ := p.NextFailedPod(ctx, ask)
		require.Nil(t, pod)
	}))

	t.Run("NothingIsReplacedWhileAPoolScales", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 2)
		requireReplaces(t, p, ask)
		pool := p.existingPools[ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "pa"}]
		if rapid.Bool().Draw(t, "pod missing") {
			pool.pods = pool.pods[:len(pool.pods)-1]
		} else {
			pool.set.Status.Replicas++
		}
		require.False(t, p.CheckScale(ctx))
		pod, _ := p.NextFailedPod(ctx, ask)
		require.Nil(t, pod)
	}))

	// A held candidate, in its pool or by a peer that sees it alive, must not
	// stop one the rule allows.
	t.Run("NextFailedPodIsTheFirstTheRuleAllows", rapid.MakeCheck(func(t *rapid.T) {
		p := genSettledTracker(t)
		require.True(t, p.CheckScale(ctx))
		addBrokerBackedPool(t, p)
		unseen := rapid.IntRange(0, 4).Draw(t, "a cluster unseen") == 0
		if unseen {
			p.MarkClusterUnseen("north")
		}
		ask := genAnswers(t, p)
		var view []failedpods.Pod
		for _, pod := range p.PoolPods() {
			view = append(view, failedpods.Pod{Pod: pod.Pod, Pool: pod.Pool, Current: pod.Current, Removing: pod.Removing})
		}
		all := append(slices.Clone(view), p.brokerBackedPods()...)
		peers := failedpods.AskPeers(ctx, all, unseen, ask)
		want := slices.IndexFunc(view, func(pod failedpods.Pod) bool {
			return failedpods.Candidate(pod, peers) && !failedpods.Waits(view, pod, func(failedpods.Pod) bool { return true }) && failedpods.Hold(all, pod, peers) == ""
		})

		pod, _ := p.NextFailedPod(ctx, ask)
		if want < 0 {
			require.Nil(t, pod)
			return
		}
		require.NotNil(t, pod, "%s may be replaced", view[want].Name)
		require.Equal(t, clusterObjectKey(p.PoolPods()[want]), clusterObjectKey(pod))
	}))

	// Brokers the pools leave out, in a cluster whose pods couldn't be listed
	// or a pool Broker CRs manage, may still run, and say what the pools'
	// one answering broker can't: a pod's own container never decides while
	// they might answer, and one being deleted holds.
	t.Run("BrokersOutsideThePoolsAreNeverIgnored", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 2)
		requireReplaces(t, p, ask)
		outside := rapid.SampledFrom([]string{"unseen", "silent", "sees the candidate alive", "sees the current broker down", "terminating"}).Draw(t, "outside")
		if outside == "unseen" {
			p.MarkClusterUnseen("north")
		} else {
			backed := addRunningBrokerBackedPod(t, p, outside == "terminating")
			pools := ask
			ask = func(ctx context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
				if pod.UID != backed.UID {
					return pools(ctx, pod)
				}
				a, err := pools(ctx, p.existingPools[ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "pa"}].pods[1].pod)
				switch outside {
				case "silent":
					return failedpods.Answer{}, errors.New("no answer")
				case "sees the candidate alive":
					a.Health.NodesDown = nil
				case "sees the current broker down":
					a.Health.NodesDown = append(a.Health.NodesDown, 1)
				}
				return a, err
			}
		}
		pod, _ := p.NextFailedPod(ctx, ask)
		require.Nil(t, pod, "%s: replaced a pod the brokers outside the pools hold", outside)
	}))

	// But one that agrees with the pools' broker, asked as they are, doesn't.
	t.Run("ABrokerOutsideThePoolsThatAgreesDoesntHold", rapid.MakeCheck(func(t *rapid.T) {
		p, ask := genDecidedTracker(t, 2)
		backed := addRunningBrokerBackedPod(t, p, false)
		pools := ask
		ask = func(ctx context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
			if pod.UID != backed.UID {
				return pools(ctx, pod)
			}
			return pools(ctx, p.existingPools[ClusterNamespacedName{Cluster: "east", CanonicalClusterName: "east", Namespace: "redpanda", Name: "pa"}].pods[1].pod)
		}
		requireReplaces(t, p, ask)
	}))

	t.Run("BrokerBackedPoolsNeverReachStatefulSetPlanners", rapid.MakeCheck(func(t *rapid.T) {
		p := genTracker(t)
		for _, s := range slices.Concat(p.ToCreate(), p.ToScaleUp(), p.RequiresUpdate(), statefulSets(p.ToScaleDown()), p.ToDelete()) {
			existing, ok := p.existingPools[objectKeyFromObject(s)]
			require.False(t, ok && existing.brokerBacked, "%s is broker-backed", objectKeyFromObject(s))
		}

		backed := brokerBackedPods(p)
		for _, pod := range p.PodsToRoll() {
			require.False(t, backed[pod.GetCluster()+"/"+pod.Name], "rolls broker-backed pod %s", pod.Name)
		}
		for _, pod := range p.PoolPods() {
			require.False(t, backed[pod.GetCluster()+"/"+pod.Name], "%s is broker-backed", pod.Name)
		}
	}))
}

// TestPoolTrackerConvergedIsQuiescent: pools that already match their desired
// state must plan nothing, or the controller writes on every pass.
func TestPoolTrackerConvergedIsQuiescent(t *testing.T) {
	ctx := t.Context()

	rapid.Check(t, func(t *rapid.T) {
		p := genConvergedTracker(t)

		require.True(t, p.CheckScale(ctx))
		require.False(t, p.ScaledUpButNoneReady())
		require.False(t, p.HasRecentlyReplacedPods())
		require.Empty(t, p.ToCreate())
		require.Empty(t, p.ToScaleUp())
		require.Empty(t, p.RequiresUpdate())
		require.Empty(t, p.ToScaleDown())
		require.Empty(t, p.ToDelete())
		require.Empty(t, p.BrokerBackedPoolsWithoutDesired())
		require.Empty(t, p.PodsToRoll())
		for _, pod := range p.PoolPods() {
			require.True(t, pod.Current, "%s reads as outdated", pod.Name)
			require.False(t, pod.Removing, "%s reads as being removed", pod.Name)
		}
		// Not even an outbound ask: only a failing outdated pod warrants one.
		var asked atomic.Int32
		pod, _ := p.NextFailedPod(ctx, func(context.Context, *corev1.Pod) (failedpods.Answer, error) {
			asked.Add(1)
			return failedpods.Answer{}, nil
		})
		require.Nil(t, pod)
		require.Zero(t, asked.Load(), "asked the brokers")
	})
}

var (
	propertyClusters = []string{"east", "west"}
	propertyPools    = []string{"pa", "pb"}
)

// genTracker draws two clusters of two pools with up to four replicas each:
// enough to combine every planner branch, small enough for rapid to shrink a
// failure to a readable case.
func genTracker(t *rapid.T) *PoolTracker {
	p := NewPoolTracker(rapid.Int64Range(1, 3).Draw(t, "generation"), false)
	for _, cluster := range propertyClusters {
		if rapid.Bool().Draw(t, cluster+" observed") {
			p.MarkClusterObserved(cluster)
		}
		for _, pool := range propertyPools {
			key := cluster + "/" + pool
			if rapid.Bool().Draw(t, key+" exists") {
				p.addExisting(genExistingPool(t, key, cluster, pool))
			}
			if rapid.Bool().Draw(t, key+" desired") {
				p.addDesired(genStatefulSet(t, key+" desired", cluster, pool))
			}
		}
	}
	return p
}

func genExistingPool(t *rapid.T, key, cluster, pool string) *poolWithOrdinals {
	mcset := genStatefulSet(t, key+" existing", cluster, pool)
	mcset.Status.Replicas = rapid.Int32Range(0, 4).Draw(t, key+" status replicas")
	mcset.Status.ReadyReplicas = rapid.Int32Range(0, mcset.Status.Replicas).Draw(t, key+" ready replicas")

	var revisions []*appsv1.ControllerRevision
	for i := range rapid.IntRange(0, 2).Draw(t, key+" revisions") {
		revisions = append(revisions, &appsv1.ControllerRevision{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-rev-%d", pool, i+1)},
		})
	}

	// Pods lag their StatefulSet both ways, so any ordinal below the larger
	// of spec and status may be missing.
	var pods []*corev1.Pod
	for i := range max(ptr.Deref(mcset.Spec.Replicas, 0), mcset.Status.Replicas) {
		label := fmt.Sprintf("%s pod %d", key, i)
		if rapid.Bool().Draw(t, label+" exists") {
			pods = append(pods, genPod(t, label, fmt.Sprintf("%s-%d", pool, i), revisions))
		}
	}
	withOrdinals, err := sortPodsByOrdinal(pods...)
	require.NoError(t, err)

	return &poolWithOrdinals{
		set:          mcset,
		pods:         withOrdinals,
		revisions:    revisions,
		brokerBacked: rapid.IntRange(0, 7).Draw(t, key+" broker-backed") == 0,
	}
}

func genStatefulSet(t *rapid.T, label, cluster, pool string) *MulticlusterStatefulSet {
	labels := map[string]string{
		configVersionLabel:      rapid.SampledFrom([]string{"c1", "c2"}).Draw(t, label+" config version"),
		nodePoolGenerationLabel: rapid.SampledFrom([]string{"1", "2"}).Draw(t, label+" pool generation"),
	}
	if generation := rapid.SampledFrom([]string{"", "1", "2", "3"}).Draw(t, label+" generation label"); generation != "" {
		labels[generationLabel] = generation
	}
	return newMulticlusterStatefulSet(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: pool, Namespace: "redpanda", Labels: labels},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(rapid.Int32Range(0, 4).Draw(t, label+" replicas"))},
	}, cluster, cluster)
}

// genPod draws a pod on one of revisions, on none (adopted by a rollback from
// Broker CRs), or on one newer than the cached revisions.
func genPod(t *rapid.T, label, name string, revisions []*appsv1.ControllerRevision) *corev1.Pod {
	revisionNames := []string{"", "newer-than-cache"}
	for _, r := range revisions {
		revisionNames = append(revisionNames, r.Name)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "redpanda"},
		Status: corev1.PodStatus{
			Phase: rapid.SampledFrom([]corev1.PodPhase{corev1.PodRunning, corev1.PodPending}).Draw(t, label+" phase"),
		},
	}
	if revision := rapid.SampledFrom(revisionNames).Draw(t, label+" revision"); revision != "" {
		pod.Labels = map[string]string{appsv1.StatefulSetRevisionLabel: revision}
	}
	if rapid.Bool().Draw(t, label+" ready") {
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	}
	if rapid.IntRange(0, 7).Draw(t, label+" terminating") == 0 {
		pod.DeletionTimestamp = ptr.To(metav1.Now())
	}
	state := rapid.SampledFrom([]string{"running", "running", "starting", "hung", "crash looping", "crash looping", "creating", "unschedulable"}).Draw(t, label+" redpanda")
	fresh := newPod(name, state)
	pod.Spec, pod.Status.ContainerStatuses = fresh.Spec, fresh.Status.ContainerStatuses
	if state == "unschedulable" {
		pod.Status.Phase, pod.Status.Conditions = fresh.Status.Phase, fresh.Status.Conditions
	}
	return pod
}

// newPod is a broker pod with its data on a PVC and redpanda in state.
func newPod(name, state string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "redpanda"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: redpanda.DatadirVolumeName, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: redpanda.DatadirVolumeName + "-" + name},
		}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	now := metav1.Now()
	redpandaStatus := corev1.ContainerStatus{Name: redpandachart.RedpandaContainerName}
	switch state {
	case "running":
		redpandaStatus.State.Running = &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-time.Hour))}
		redpandaStatus.Started = ptr.To(true)
	case "starting", "hung":
		started := now.Add(-time.Minute)
		if state == "hung" {
			started = now.Add(-15 * time.Minute)
		}
		redpandaStatus.State.Running = &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(started)}
		redpandaStatus.Started = ptr.To(false)
	case "crash looping", "between crashes":
		redpandaStatus.State.Waiting = &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}
		redpandaStatus.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
		if state == "between crashes" {
			redpandaStatus.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}
			redpandaStatus.Started = ptr.To(true)
		}
	case "unschedulable":
		pod.Status = corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
			LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute)),
		}}}
		return pod
	case "image pull failing":
		// It never ran.
		pod.Status.Phase = corev1.PodPending
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodInitialized, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute))}}
		redpandaStatus.State.Waiting = &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}
	default:
		redpandaStatus.State.Waiting = &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{redpandaStatus}
	return pod
}

// genAnswers draws what each pod's broker answers: nothing, or a down list
// and a member list naming every pod by the broker ID of its order.
func genAnswers(t *rapid.T, p *PoolTracker) failedpods.Ask {
	pods := allPods(p)
	var members []rpadmin.Broker
	for id, pod := range pods {
		members = append(members, rpadmin.Broker{NodeID: id, InternalRPCAddress: pod.Name + "." + pod.GetCluster() + ".svc:33145"})
	}
	answers := map[string]*failedpods.Answer{}
	for _, pod := range pods {
		key := clusterObjectKey(pod)
		if !rapid.Bool().Draw(t, key+" answers") {
			continue
		}
		a := &failedpods.Answer{Brokers: members}
		for id := range pods {
			a.Health.AllNodes = append(a.Health.AllNodes, id)
			if rapid.Bool().Draw(t, fmt.Sprintf("%s reports %d down", key, id)) {
				a.Health.NodesDown = append(a.Health.NodesDown, id)
			}
		}
		answers[pod.GetCluster()+"/"+pod.Name] = a
	}
	return func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		for _, o := range pods {
			if o.Pod.Name == pod.Name && o.Pod.UID == pod.UID {
				if a := answers[o.GetCluster()+"/"+o.Name]; a != nil {
					return *a, nil
				}
			}
		}
		return failedpods.Answer{}, errors.New("no answer")
	}
}

// genDecidedTracker draws the case failed-pod replacement acts on, in a
// settled pool pa of cluster east: its outdated crash-looping pod pa-0 beside
// pa-1, on the latest revision and running, the one broker that may answer,
// which reports every other down; then up to two more pods that can't answer.
// The rule replaces pa-0.
func genDecidedTracker(t *rapid.T, least int32) (*PoolTracker, failedpods.Ask) {
	p := NewPoolTracker(1, false)
	p.MarkClusterObserved("east")
	pods := addDecidedPool(t, p, "pa", "running", least)

	var members []rpadmin.Broker
	var all, down []int
	for id, pod := range pods {
		members = append(members, rpadmin.Broker{NodeID: id, InternalRPCAddress: pod.Name + ".east.svc:33145"})
		all = append(all, id)
		if id != 1 {
			down = append(down, id)
		}
	}
	return p, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		if pod.UID != pods[1].UID {
			return failedpods.Answer{}, errors.New("no answer")
		}
		return failedpods.Answer{Health: rpadmin.ClusterHealthOverview{AllNodes: all, NodesDown: slices.Clone(down)}, Brokers: members}, nil
	}
}

// addDecidedPool adds a settled pool to cluster east of at least least pods:
// its outdated crash-looping pod <pool>-0 beside <pool>-1 on the latest
// revision, in current, then more that can't answer. It returns them.
func addDecidedPool(t *rapid.T, p *PoolTracker, pool, current string, least int32) []*corev1.Pod {
	n := rapid.Int32Range(least, 4).Draw(t, pool+" pods")
	desired := genStatefulSet(t, "east/"+pool, "east", pool)
	desired.Spec.Replicas = ptr.To(n)
	p.addDesired(desired)

	set := desired.DeepCopy()
	set.Labels[generationLabel] = "1"
	set.Status.Replicas, set.Status.ReadyReplicas, set.Status.UpdatedReplicas = n, n, n
	revisions := []*appsv1.ControllerRevision{{ObjectMeta: metav1.ObjectMeta{Name: pool + "-rev-1"}}, {ObjectMeta: metav1.ObjectMeta{Name: pool + "-rev-2"}}}
	var pods []*corev1.Pod
	for i := range n {
		name := fmt.Sprintf("%s-%d", pool, i)
		state, revision := "crash looping", revisions[0].Name
		switch {
		case i == 1:
			state, revision = current, revisions[1].Name
		case i > 1:
			state = rapid.SampledFrom([]string{"crash looping", "creating", "starting", "hung"}).Draw(t, name+" redpanda")
			revision = rapid.SampledFrom([]string{revisions[0].Name, revisions[1].Name}).Draw(t, name+" revision")
		}
		pod := newPod(name, state)
		pod.Labels = map[string]string{appsv1.StatefulSetRevisionLabel: revision}
		pod.UID = types.UID("east/" + name)
		pods = append(pods, pod)
	}
	withOrdinals, err := sortPodsByOrdinal(pods...)
	require.NoError(t, err)
	p.addExisting(&poolWithOrdinals{set: newMulticlusterStatefulSet(set, "east", "east"), pods: withOrdinals, revisions: revisions})
	return pods
}

func requireReplaces(t *rapid.T, p *PoolTracker, ask failedpods.Ask) {
	pod, reason := p.NextFailedPod(context.Background(), ask)
	require.NotNil(t, pod, "the decided pool holds: %s", reason)
}

// allPods lists the pools' pods, then the Broker-managed ones.
func allPods(p *PoolTracker) []*MulticlusterPod {
	var pods []*MulticlusterPod
	for _, pod := range p.PoolPods() {
		pods = append(pods, pod.MulticlusterPod)
	}
	for _, pod := range p.brokerBackedPods() {
		pods = append(pods, &MulticlusterPod{Pod: pod.Pod})
	}
	return pods
}

// addBrokerBackedPool sometimes adds a pool Broker CRs manage, in cluster
// east, its pods in any redpanda state.
func addBrokerBackedPool(t *rapid.T, p *PoolTracker) {
	if !rapid.Bool().Draw(t, "broker-backed pool") {
		return
	}
	var pods []*corev1.Pod
	for i := range rapid.IntRange(1, 2).Draw(t, "broker-backed pods") {
		label := fmt.Sprintf("east/pc pod %d", i)
		pod := genPod(t, label, fmt.Sprintf("pc-%d", i), nil)
		pod.UID = types.UID(label)
		pods = append(pods, pod)
	}
	addBrokerBacked(t, p, pods...)
}

// addRunningBrokerBackedPod adds a pool Broker CRs manage with one pod, whose
// broker runs, and returns it.
func addRunningBrokerBackedPod(t *rapid.T, p *PoolTracker, terminating bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-0", Namespace: "redpanda", UID: "east/pc-0"},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:    redpandachart.RedpandaContainerName,
			State:   corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			Started: ptr.To(true),
		}}},
	}
	if terminating {
		pod.DeletionTimestamp = ptr.To(metav1.Now())
	}
	addBrokerBacked(t, p, pod)
	return pod
}

func addBrokerBacked(t *rapid.T, p *PoolTracker, pods ...*corev1.Pod) {
	withOrdinals, err := sortPodsByOrdinal(pods...)
	require.NoError(t, err)
	replicas := int32(len(pods))
	set := newMulticlusterStatefulSet(&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "pc", Namespace: "redpanda"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{Replicas: replicas}}, "east", "east")
	p.addExisting(&poolWithOrdinals{set: set, pods: withOrdinals, brokerBacked: true})
}

func statefulSets(down []*ScaleDownSet) []*MulticlusterStatefulSet {
	var sets []*MulticlusterStatefulSet
	for _, s := range down {
		sets = append(sets, s.StatefulSet)
	}
	return sets
}

// destructive lists the plans that remove brokers.
func destructive(p *PoolTracker) []string {
	var plans []string
	for _, s := range p.ToScaleDown() {
		plans = append(plans, fmt.Sprintf("scale %s to %d, decommissioning %s", objectKeyFromObject(s.StatefulSet), ptr.Deref(s.StatefulSet.Spec.Replicas, 0), s.LastPod.Name))
	}
	for _, s := range p.ToDelete() {
		plans = append(plans, "delete "+objectKeyFromObject(s).String())
	}
	for _, s := range p.BrokerBackedPoolsWithoutDesired() {
		plans = append(plans, "drain "+objectKeyFromObject(s).String())
	}
	return plans
}

func brokerBackedPods(p *PoolTracker) map[string]bool {
	pods := map[string]bool{}
	for _, pool := range p.existingPools {
		if !pool.brokerBacked {
			continue
		}
		for _, pod := range pool.pods {
			pods[pool.set.GetCluster()+"/"+pod.pod.Name] = true
		}
	}
	return pods
}

// genSettledTracker draws pools whose StatefulSets match their desired state,
// or that a scale-down settled on, their pods on either of two cached
// revisions and in any redpanda state: where failed-pod replacement may act.
func genSettledTracker(t *rapid.T) *PoolTracker {
	p := genConvergedTracker(t)
	for _, nn := range sortedKeys(p.existingPools) {
		pool := p.existingPools[nn]
		if replicas := ptr.Deref(pool.set.Spec.Replicas, 0); replicas > 0 && rapid.IntRange(0, 2).Draw(t, clusterObjectKey(pool.set)+" scaling down") == 0 {
			p.desiredPools[nn].set.Spec.Replicas = ptr.To(rapid.Int32Range(0, replicas-1).Draw(t, clusterObjectKey(pool.set)+" scaling down to"))
		}
		name := pool.set.Name
		pool.revisions = append(pool.revisions, &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: name + "-rev-2"}})
		for _, pod := range pool.pods {
			label := pool.set.GetCluster() + "/" + pod.pod.Name
			revision := rapid.SampledFrom([]string{name + "-rev-1", name + "-rev-2"}).Draw(t, label+" revision")
			pod.pod.Labels = map[string]string{appsv1.StatefulSetRevisionLabel: revision}
			pod.pod.UID = types.UID(label)
			fresh := genPod(t, label, pod.pod.Name, nil)
			pod.pod.Spec, pod.pod.Status, pod.pod.DeletionTimestamp = fresh.Spec, fresh.Status, fresh.DeletionTimestamp
		}
	}
	return p
}

// genConvergedTracker draws pools that already match their desired state,
// some pods adopted without a revision label.
func genConvergedTracker(t *rapid.T) *PoolTracker {
	generation := rapid.Int64Range(1, 3).Draw(t, "generation")
	p := NewPoolTracker(generation, false)
	for _, cluster := range propertyClusters {
		if rapid.Bool().Draw(t, cluster+" observed") {
			p.MarkClusterObserved(cluster)
		}
		for _, pool := range propertyPools {
			key := cluster + "/" + pool
			if !rapid.Bool().Draw(t, key+" exists") {
				continue
			}
			desired := genStatefulSet(t, key, cluster, pool)
			p.addDesired(desired)

			set := desired.DeepCopy()
			set.Labels[generationLabel] = strconv.FormatInt(generation, 10)
			replicas := ptr.Deref(set.Spec.Replicas, 0)
			set.Status.Replicas = replicas
			set.Status.ReadyReplicas = replicas
			set.Status.UpdatedReplicas = replicas

			revision := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: pool + "-rev-1"}}
			var pods []*corev1.Pod
			for i := range replicas {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", pool, i), Namespace: "redpanda"},
					Status: corev1.PodStatus{
						Phase:      corev1.PodRunning,
						Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						ContainerStatuses: []corev1.ContainerStatus{{
							Name:    redpandachart.RedpandaContainerName,
							State:   corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
							Started: ptr.To(true),
						}},
					},
				}
				if !rapid.Bool().Draw(t, fmt.Sprintf("%s pod %d adopted", key, i)) {
					pod.Labels = map[string]string{appsv1.StatefulSetRevisionLabel: revision.Name}
				}
				pods = append(pods, pod)
			}
			withOrdinals, err := sortPodsByOrdinal(pods...)
			require.NoError(t, err)

			p.addExisting(&poolWithOrdinals{
				set:       newMulticlusterStatefulSet(set, cluster, cluster),
				pods:      withOrdinals,
				revisions: []*appsv1.ControllerRevision{revision},
			})
		}
	}
	return p
}

func sortedKeys[V any](m map[ClusterNamespacedName]V) []ClusterNamespacedName {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b ClusterNamespacedName) int {
		return strings.Compare(a.CanonicalClusterName+"/"+a.Namespace+"/"+a.Name, b.CanonicalClusterName+"/"+b.Namespace+"/"+b.Name)
	})
	return keys
}
