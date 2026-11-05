// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	redpandaclient "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/brokerset"
	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// TestReplaceFailedPodPassProperties runs failed-pod replacement for every
// node pool of a random V1 cluster in one pass, as its pools are reconciled
// from a cache that shows none of the pass's writes, and checks what the pass
// deleted against the rule.
func TestReplaceFailedPodPassProperties(t *testing.T) {
	ctx := t.Context()

	t.Run("AtMostOneDeletionPerPass", rapid.MakeCheck(func(t *rapid.T) {
		r := genV1Cluster(t).pass(ctx)
		require.LessOrEqual(t, len(r.deleted), 1, "deleted %v", r.deleted)
	}))

	// The pass replaces exactly the pod the rule picks: in pool order, each
	// pool's first marked pod that failedpods allows, but one that can't be
	// scheduled while one that can crash loops.
	t.Run("ReplacesThePodTheRulePicks", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Cluster(t)
		want := w.picked(ctx)
		require.Equal(t, want, w.pass(ctx).deleted, "%s", w.change)
	}))

	t.Run("NeverDeletesARunningContainer", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Cluster(t)
		for name := range w.pass(ctx).deletedBy {
			require.Nil(t, w.pods[name].Status.ContainerStatuses[0].State.Running, "deleted %s, which runs", name)
		}
	}))

	t.Run("DeletesOnlyAMarkedOutdatedPodItsPoolKeeps", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Cluster(t)
		for name, pool := range w.pass(ctx).deletedBy {
			pod := w.pods[name]
			require.Equal(t, pod.Labels[labels.NodePoolKey], pool, "pool %s deleted %s, another pool's", pool, name)
			require.True(t, ClusterUpdatePodCondition == clusterUpdate(pod), "deleted %s, which isn't marked for update", name)
			require.NotEqual(t, newV1Revision, pod.Labels[appsv1.StatefulSetRevisionLabel], "deleted %s, already on its template", name)
			require.False(t, w.removing(pod), "deleted %s, which a scale-down or decommission removes", name)
		}
	}))

	// Its container may have started since it was judged.
	t.Run("DeletesOnlyThePodAsItWasJudged", rapid.MakeCheck(func(t *rapid.T) {
		r := genV1Cluster(t).pass(ctx)
		for name := range r.deletedBy {
			require.True(t, r.unchanged[name], "deleted %s whatever it became", name)
		}
	}))

	// A restart-only config change leaves no diff, but still needs the pod
	// replaced.
	t.Run("ReplacesARestartOnlyPodWithNoDiff", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Decision(t, "no diff, marked for a restart")
		require.Equal(t, []string{w.podNames()[0]}, w.pass(ctx).deleted)
	}))

	// One that can't be scheduled waits for one its pool replaces that can
	// run, at whatever point of its crash loop, and for no other.
	t.Run("ACandidateThatCanRunGoesFirst", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Decision(t, rapid.SampledFrom([]string{"unscheduled beside one that runs", "unscheduled beside one between crashes"}).Draw(t, "change"))
		names := w.podNames()
		marked := clusterUpdate(w.pods[names[2]]) == ClusterUpdatePodCondition
		require.Equal(t, !marked, slices.Contains(w.pass(ctx).deleted, names[0]), "%s, %s marked %t: replaced %s", w.change, names[2], marked, names[0])
	}))

	// A later pool lists pods itself, after the peers were asked.
	t.Run("NeverDecidesWithoutABrokerThatStartedSinceItsPeersWereAsked", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Decision(t, "started mid-pass")
		r := w.pass(ctx)
		for name, pool := range r.deletedBy {
			if pool == w.cluster.Spec.NodePools[0].Name {
				continue
			}
			for _, started := range w.startsMidPass {
				require.Positive(t, r.asked[started], "%s deleted %s without asking %s, which started since its peers were asked", pool, name, started)
			}
		}
	}))

	t.Run("NothingWhileAPodIsMissingOrTerminatingOrAStatefulSetTrails", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Cluster(t)
		r := w.pass(ctx)
		if w.missing || w.behind || w.orphaned || slices.ContainsFunc(w.podNames(), func(name string) bool { return w.pods[name].DeletionTimestamp != nil && !w.stuck[name] }) {
			require.Empty(t, r.deleted)
		}
	}))

	t.Run("NeverDeletesAPodAPeerSeesAlive", rapid.MakeCheck(func(t *rapid.T) {
		w := genV1Cluster(t)
		for name := range w.pass(ctx).deletedBy {
			id := w.id[name]
			for _, other := range w.podNames() {
				a, answered := w.answers[other]
				answered = answered && !w.silent[other] && w.canAnswer(other)
				require.False(t, other != name && answered && !slices.Contains(a.Health.NodesDown, id),
					"deleted %s, which %s reports alive", name, other)
			}
		}
	}))

	// One hung broker costs the pass one timeout, not one a pool.
	t.Run("AsksEachBrokerAtMostOnce", rapid.MakeCheck(func(t *rapid.T) {
		for name, n := range genV1Cluster(t).pass(ctx).asked {
			require.LessOrEqual(t, n, 1, "asked %s's broker %d times", name, n)
		}
	}))

	t.Run("ErrorsHoldAndOnlyTheDeleteIsWritten", rapid.MakeCheck(func(t *rapid.T) {
		r := genV1Cluster(t).pass(ctx)
		for _, err := range r.errs {
			var requeue *RequeueAfterError
			require.ErrorAs(t, err, &requeue, "a plain error skips the pool's scaling")
		}
		for _, write := range r.writes {
			require.True(t, slices.Contains([]string{"delete", "status patch"}, write), "wrote %s", write)
		}
	}))
}

const (
	oldV1Image, newV1Image       = "redpanda:old", "redpanda:new"
	oldV1Revision, newV1Revision = "rev-old", "rev-new"
)

// v1Cluster is a V1 Cluster's StatefulSets and pods at the start of a pass,
// and what each pod's broker answers.
type v1Cluster struct {
	cluster *vectorizedv1alpha1.Cluster
	objs    []client.Object
	pods    map[string]*corev1.Pod
	id      map[string]int
	answers map[string]failedpods.Answer
	// state is each pod's redpanda state.
	state map[string]string
	// silent maps the brokers that time out on the pass's one ask; stuck the
	// pods terminating on a node that won't finish them.
	silent, stuck map[string]bool
	// missing, behind and orphaned: a pod is yet to be created, a
	// StatefulSet's status trails its spec, or one is being recreated around
	// its orphaned pods.
	missing, behind, orphaned bool
	// failList and failDelete fail the pass's pod list and deletes.
	failList, failDelete bool
	// startsMidPass are pods whose brokers start once the first pool has
	// run.
	startsMidPass []string
	// change is the one change a decision world made, "" in a random one.
	change string
}

func newV1Cluster(pools int) *v1Cluster {
	w := &v1Cluster{cluster: pandaCluster(), pods: map[string]*corev1.Pod{}, id: map[string]int{}, state: map[string]string{}, answers: map[string]failedpods.Answer{}, silent: map[string]bool{}, stuck: map[string]bool{}}
	if pools == 2 {
		second := w.cluster.Spec.NodePools[0]
		second.Name = "second"
		w.cluster.Spec.NodePools = append(w.cluster.Spec.NodePools, second)
	}
	w.cluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{}
	return w
}

// genV1Cluster draws one or two pools of up to four pods in any state the
// pass reads. Half the time it draws a decision instead, so each rule's case
// comes up.
func genV1Cluster(t *rapid.T) *v1Cluster {
	if rapid.Bool().Draw(t, "decision") {
		return genV1Decision(t, rapid.SampledFrom(v1DecisionChanges).Draw(t, "change"))
	}
	w := newV1Cluster(rapid.IntRange(1, 2).Draw(t, "pools"))
	nextID := 0
	for i := range w.cluster.Spec.NodePools {
		pool := &w.cluster.Spec.NodePools[i]
		keep := rapid.Int32Range(1, 3).Draw(t, pool.Name+" replicas")
		current := keep
		if rapid.IntRange(0, 3).Draw(t, pool.Name+" scaling down") == 0 {
			current += rapid.Int32Range(1, 2).Draw(t, pool.Name+" scaling down by")
		}
		sts := w.addPool(pool, keep, current, rapid.IntRange(0, 11).Draw(t, pool.Name+" behind") == 0)
		for ordinal := range current {
			name := fmt.Sprintf("%s-%d", sts.Name, ordinal)
			id := nextID
			nextID++
			if rapid.IntRange(0, 19).Draw(t, name+" missing") == 0 {
				w.missing = true
				continue
			}
			revision := rapid.SampledFrom([]string{oldV1Revision, newV1Revision}).Draw(t, name+" revision")
			image := newV1Image
			if revision == oldV1Revision && rapid.IntRange(0, 9).Draw(t, name+" already matches") > 0 {
				image = oldV1Image
			}
			reason := ""
			if rapid.IntRange(0, 4).Draw(t, name+" marked") > 0 {
				reason = rapid.SampledFrom([]string{ClusterUpdateReasonVersion, ClusterUpdateReasonConfig}).Draw(t, name+" marked for")
			}
			pod := w.addPod(sts, pool.Name, name, id, revision, image, reason,
				rapid.SampledFrom([]string{"running", "running", "starting", "between crashes", "crash looping", "crash looping", "creating", "unschedulable"}).Draw(t, name+" redpanda"))
			if rapid.IntRange(0, 19).Draw(t, name+" terminating") == 0 {
				pod.DeletionTimestamp, pod.Finalizers = ptr.To(metav1.Now()), []string{"test/hold"}
				if rapid.Bool().Draw(t, name+" stuck terminating") {
					pod.DeletionTimestamp, w.stuck[name] = ptr.To(metav1.NewTime(time.Now().Add(-10*time.Minute))), true
				}
			}
		}
	}
	if ids := w.podNames(); len(ids) > 0 && rapid.IntRange(0, 5).Draw(t, "decommission in flight") == 0 {
		w.cluster.SetDecommissionBrokerID(ptr.To(int32(w.id[rapid.SampledFrom(ids).Draw(t, "decommissioning")])))
	}
	if len(w.cluster.Spec.NodePools) == 2 && rapid.IntRange(0, 9).Draw(t, "a StatefulSet recreating") == 0 {
		w.orphan("second")
	}
	w.failList = rapid.IntRange(0, 19).Draw(t, "pod list fails") == 0
	w.failDelete = rapid.IntRange(0, 9).Draw(t, "deletes fail") == 0

	for _, name := range w.podNames() {
		if rapid.IntRange(0, 2).Draw(t, name+" answers") == 0 {
			continue
		}
		w.answer(name, func(other string) bool {
			// Mostly right, so the rule's cases are reached.
			down := w.pods[other].Status.ContainerStatuses[0].State.Running == nil
			return down != (rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("%s misreports %s", name, other)) == 0)
		})
		w.silent[name] = rapid.IntRange(0, 3).Draw(t, name+" silent") == 0
	}
	return w
}

// genV1Decision draws the case the pass decides on: two pools, nothing
// missing, terminating, trailing or removed, each pod marked if outdated, and
// each pool's first pod outdated and crash looping beside a current one that
// runs, every broker reporting each as it is; then one change a rule turns on,
// in the first pool. The second pool's candidate is what a pass that missed
// the first's delete would replace too.
func genV1Decision(t *rapid.T, change string) *v1Cluster {
	w := newV1Cluster(2)
	w.change = change
	nextID := 0
	for i := range w.cluster.Spec.NodePools {
		pool := &w.cluster.Spec.NodePools[i]
		least := int32(2)
		if i == 0 && (w.change == "unschedulable" || strings.HasPrefix(w.change, "unscheduled beside")) {
			least = 3
		}
		keep := rapid.Int32Range(least, 3).Draw(t, pool.Name+" replicas")
		current := keep
		// The first pool's scale-down removes two failing pods, left the only
		// ones to replace: one more than the next it removes.
		if w.change == "scale-down" && i == 0 {
			current += 2
		}
		sts := w.addPool(pool, keep, current, false)
		for ordinal := range current {
			name := fmt.Sprintf("%s-%d", sts.Name, ordinal)
			id := nextID
			nextID++
			revision, state := oldV1Revision, "crash looping"
			switch {
			case ordinal == 1 || i == 0 && ordinal == 0 && w.change == "scale-down":
				revision, state = newV1Revision, "running"
			case i == 0 && ordinal >= keep:
			case ordinal > 1:
				revision = rapid.SampledFrom([]string{oldV1Revision, newV1Revision}).Draw(t, name+" revision")
				state = rapid.SampledFrom([]string{"running", "crash looping"}).Draw(t, name+" redpanda")
			}
			image, reason := newV1Image, ""
			if revision == oldV1Revision {
				image, reason = oldV1Image, ClusterUpdateReasonVersion
			}
			w.addPod(sts, pool.Name, name, id, revision, image, reason, state)
		}
	}
	names := w.podNames()
	candidate, current := names[0], names[1]
	pick := func(label string) string { return rapid.SampledFrom(names).Draw(t, label) }
	reset := func(name, revision, image, reason, state string) {
		pod := w.pods[name]
		pool := pod.Labels[labels.NodePoolKey]
		w.objs = slices.DeleteFunc(w.objs, func(o client.Object) bool { return o == client.Object(pod) })
		w.addPod(w.stsOf(pool), pool, name, w.id[name], revision, image, reason, state)
	}
	switch w.change {
	case "terminating, being removed":
		// A broker being decommissioned, its pod deleted as it drains.
		name := pick("removed")
		w.cluster.SetDecommissionBrokerID(ptr.To(int32(w.id[name])))
		w.pods[name].DeletionTimestamp, w.pods[name].Finalizers = ptr.To(metav1.Now()), []string{"test/hold"}
	case "terminating", "stuck terminating":
		name := pick("terminating")
		pod := w.pods[name]
		pod.DeletionTimestamp, pod.Finalizers = ptr.To(metav1.Now()), []string{"test/hold"}
		if w.change == "stuck terminating" {
			pod.DeletionTimestamp, w.stuck[name] = ptr.To(metav1.NewTime(time.Now().Add(-10*time.Minute))), true
		}
	case "missing":
		name := pick("missing")
		w.objs = slices.DeleteFunc(w.objs, func(o client.Object) bool { return o.GetName() == name })
		delete(w.pods, name)
		w.missing = true
	case "behind":
		sts := w.stsOf(rapid.SampledFrom([]string{"first", "second"}).Draw(t, "behind"))
		sts.Generation, sts.Status.ObservedGeneration = 2, 1
		w.behind = true
	case "recreating":
		w.orphan("second")
	case "pool not yet created":
		// Nothing listed shows it.
		w.objs = slices.DeleteFunc(w.objs, func(o client.Object) bool { return o.GetLabels()[labels.NodePoolKey] == "second" })
		for name, pod := range w.pods {
			if pod.Labels[labels.NodePoolKey] == "second" {
				delete(w.pods, name)
			}
		}
		w.missing = true
	case "decommission in flight":
		decommissioning := candidate
		if rapid.Bool().Draw(t, "decommissioning another") {
			decommissioning = pick("decommissioning")
		}
		w.cluster.SetDecommissionBrokerID(ptr.To(int32(w.id[decommissioning])))
	case "unschedulable":
		reset(names[2], newV1Revision, newV1Image, "", "unschedulable")
	case "template down":
		for _, name := range names {
			if w.pods[name].Labels[labels.NodePoolKey] == "first" && w.pods[name].Labels[appsv1.StatefulSetRevisionLabel] == newV1Revision {
				reset(name, newV1Revision, newV1Image, "", "crash looping")
			}
		}
	case "between crashes", "starting":
		reset(current, newV1Revision, newV1Image, "", w.change)
	case "no diff":
		reset(candidate, oldV1Revision, newV1Image, ClusterUpdateReasonVersion, "crash looping")
	case "unscheduled beside one that runs", "unscheduled beside one between crashes":
		// Replacing the one that runs first shows its template does, at
		// whatever point of its crash loop. One its pool's roll doesn't mark
		// is never replaced, so isn't waited for.
		reset(candidate, oldV1Revision, oldV1Image, ClusterUpdateReasonVersion, "unschedulable")
		state := "crash looping"
		if w.change == "unscheduled beside one between crashes" {
			state = "between crashes"
		}
		reason := ClusterUpdateReasonVersion
		if rapid.Bool().Draw(t, "beside one unmarked") {
			reason = ""
		}
		reset(names[2], oldV1Revision, oldV1Image, reason, state)
	case "no diff, marked for a restart":
		// A restart-only config change: nothing to diff, but replaced.
		reset(candidate, oldV1Revision, newV1Image, ClusterUpdateReasonConfig, "crash looping")
	case "started mid-pass":
		// Nothing can answer when the first pool asks, then holds on its
		// down template; the second pool's current pod starts before it
		// decides.
		for _, name := range names[1:] {
			pod := w.pods[name]
			pool, revision := pod.Labels[labels.NodePoolKey], pod.Labels[appsv1.StatefulSetRevisionLabel]
			reason := ""
			if revision == oldV1Revision {
				reason = ClusterUpdateReasonVersion
			}
			reset(name, revision, pod.Spec.Containers[0].Image, reason, "crash looping")
			if pool == "second" && name[len(name)-2:] == "-1" {
				reset(name, revision, pod.Spec.Containers[0].Image, reason, "creating")
				w.startsMidPass = append(w.startsMidPass, name)
			}
		}
	case "current marked for a restart":
		reset(current, newV1Revision, newV1Image, ClusterUpdateReasonConfig, "crash looping")
	case "unmarked":
		// Its pool's roll found no diff for it, or isn't rolling.
		reset(candidate, oldV1Revision, oldV1Image, "", "crash looping")
	case "deletes fail":
		w.failDelete = true
	case "list fails":
		w.failList = true
	}

	names = w.podNames()
	for _, name := range names {
		w.answer(name, func(other string) bool {
			switch {
			case w.change == "seen alive" && other == candidate:
				return false
			case w.change == "reported down" && other == current:
				return true
			}
			return !w.canAnswer(other)
		})
		w.silent[name] = w.change == "silent"
	}
	return w
}

// v1DecisionChanges are the one change genV1Decision makes, each a case a
// rule turns on; "started mid-pass" is drawn only for its own property.
var v1DecisionChanges = []string{
	"none", "terminating", "stuck terminating", "missing", "behind", "recreating", "pool not yet created", "scale-down", "decommission in flight",
	"unschedulable", "template down", "between crashes", "starting", "seen alive", "reported down", "silent",
	"no diff", "no diff, marked for a restart", "current marked for a restart", "unmarked", "deletes fail", "list fails",
	"terminating, being removed", "unscheduled beside one that runs", "unscheduled beside one between crashes",
}

// orphan has pool's StatefulSet deleted, orphaning its pods, to be recreated.
func (w *v1Cluster) orphan(pool string) {
	sts := w.stsOf(pool)
	w.objs = slices.DeleteFunc(w.objs, func(o client.Object) bool { return o == client.Object(sts) })
	for _, pod := range w.pods {
		if pod.Labels[labels.NodePoolKey] == pool {
			pod.OwnerReferences = nil
		}
	}
	w.orphaned = true
}

// canAnswer reports whether name's broker can answer: it runs, has started,
// and its node is still there.
func (w *v1Cluster) canAnswer(name string) bool {
	return (w.state[name] == "running" || w.state[name] == "between crashes") && !w.stuck[name]
}

func (w *v1Cluster) addPool(pool *vectorizedv1alpha1.NodePoolSpec, keep, current int32, behind bool) *appsv1.StatefulSet {
	pool.Replicas = ptr.To(keep)
	w.cluster.Status.NodePools[pool.Name] = vectorizedv1alpha1.NodePoolStatus{CurrentReplicas: current}
	ssres := StatefulSetResource{pandaCluster: w.cluster, nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: *pool}}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: ssres.Key().Name, Namespace: w.cluster.Namespace, UID: types.UID("uid-" + pool.Name), Labels: labels.ForCluster(w.cluster).WithNodePool(pool.Name)},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(current)},
		Status:     appsv1.StatefulSetStatus{UpdateRevision: newV1Revision},
	}
	if behind {
		sts.Generation, sts.Status.ObservedGeneration = 2, 1
		w.behind = true
	}
	w.objs = append(w.objs, sts)
	return sts
}

func (w *v1Cluster) stsOf(pool string) *appsv1.StatefulSet {
	for _, o := range w.objs {
		if sts, ok := o.(*appsv1.StatefulSet); ok && sts.Labels[labels.NodePoolKey] == pool {
			return sts
		}
	}
	return nil
}

// addPod adds a pod on revision running image, marked for update for reason
// unless "", with redpanda in state.
func (w *v1Cluster) addPod(sts *appsv1.StatefulSet, pool, name string, id int, revision, image, reason, state string) *corev1.Pod {
	podLabels := labels.ForCluster(w.cluster).WithNodePool(pool)
	podLabels[appsv1.StatefulSetRevisionLabel] = revision
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   w.cluster.Namespace,
			UID:         types.UID("uid-" + name),
			Labels:      podLabels,
			Annotations: map[string]string{PodAnnotationNodeIDKey: strconv.Itoa(id)},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: redpandaContainerName, Image: image}},
			Volumes: []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-" + name},
			}}},
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodInitialized, Status: corev1.ConditionTrue}}},
	}
	if reason != "" {
		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{Type: ClusterUpdatePodCondition, Status: corev1.ConditionTrue, Reason: reason})
	}

	status := corev1.ContainerStatus{Name: redpandaContainerName}
	now := metav1.NewTime(time.Now())
	switch state {
	case "running":
		status.State.Running = &corev1.ContainerStateRunning{}
		status.Started = ptr.To(true)
	case "starting":
		// A PostStart hook still running.
		status.State.Running = &corev1.ContainerStateRunning{}
		status.Started = ptr.To(false)
	case "between crashes":
		status.State.Running = &corev1.ContainerStateRunning{StartedAt: now}
		status.Started = ptr.To(true)
		status.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
	case "crash looping":
		status.State.Waiting = &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}
		status.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
	case "unschedulable":
		pod.Status.Phase = corev1.PodPending
		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
			LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
		})
		status.State.Waiting = &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}
	default:
		status.State.Waiting = &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{status}
	w.pods[name], w.id[name], w.state[name] = pod, id, state
	w.objs = append(w.objs, pod)
	return pod
}

// answer has name's broker answer with every pod's broker, down as reports
// has it.
func (w *v1Cluster) answer(name string, reports func(other string) bool) {
	a := failedpods.Answer{Health: rpadmin.ClusterHealthOverview{NodesDown: []int{}}}
	for _, other := range w.podNames() {
		a.Health.AllNodes = append(a.Health.AllNodes, w.id[other])
		a.Brokers = append(a.Brokers, rpadmin.Broker{NodeID: w.id[other], InternalRPCAddress: other + ".cluster.default.svc:33145"})
		if other != name && reports(other) {
			a.Health.NodesDown = append(a.Health.NodesDown, w.id[other])
		}
	}
	w.answers[name] = a
}

// picked is the pod the rule picks for the pass to replace, if any, from
// what the generator knows: in pool order, each pool's first marked pod that
// failedpods allows, diff or no diff, but one that can't be scheduled while a
// marked, outdated one of its pool that can be crash loops.
func (w *v1Cluster) picked(ctx context.Context) []string {
	if w.failList || w.missing || w.behind || w.orphaned {
		return nil
	}
	var view []failedpods.Pod
	for _, name := range w.podNames() {
		pod := w.pods[name]
		view = append(view, failedpods.Pod{
			Pod:      pod,
			Pool:     w.stsOf(pod.Labels[labels.NodePoolKey]).Name,
			Current:  pod.Labels[appsv1.StatefulSetRevisionLabel] == newV1Revision,
			Removing: w.removing(pod),
		})
	}
	peers := failedpods.AskPeers(ctx, view, false, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		if a, ok := w.answers[pod.Name]; ok && !w.silent[pod.Name] {
			return a, nil
		}
		return failedpods.Answer{}, errors.New("no answer")
	})
	for _, np := range w.cluster.Spec.NodePools {
		pool := w.stsOf(np.Name).Name
		waits := func(p failedpods.Pod) bool {
			return w.state[p.Name] == "unschedulable" && slices.ContainsFunc(view, func(q failedpods.Pod) bool {
				return q.Pool == pool && !q.Current && !q.Removing && q.DeletionTimestamp == nil && clusterUpdate(q.Pod) == ClusterUpdatePodCondition &&
					(w.state[q.Name] == "crash looping" || w.state[q.Name] == "between crashes")
			})
		}
		i := slices.IndexFunc(view, func(p failedpods.Pod) bool {
			return p.Pool == pool && clusterUpdate(p.Pod) == ClusterUpdatePodCondition && failedpods.Candidate(p, peers) && !waits(p) &&
				failedpods.Hold(view, p, peers) == ""
		})
		if i >= 0 {
			return []string{view[i].Pod.Name}
		}
	}
	return nil
}

func (w *v1Cluster) podNames() []string {
	var names []string
	for name := range w.pods {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// removing reports whether a scale-down or the decommission in flight
// removes pod.
func (w *v1Cluster) removing(pod *corev1.Pod) bool {
	if id := w.cluster.GetDecommissionBrokerID(); id != nil && int(*id) == w.id[pod.Name] {
		return true
	}
	pool := pod.Labels[labels.NodePoolKey]
	ordinal, err := strconv.Atoi(pod.Name[len(pod.Name)-1:])
	return err == nil && int32(ordinal) >= ptr.Deref(w.cluster.Spec.NodePools[slices.IndexFunc(w.cluster.Spec.NodePools, func(np vectorizedv1alpha1.NodePoolSpec) bool { return np.Name == pool })].Replicas, 0)
}

func clusterUpdate(pod *corev1.Pod) corev1.PodConditionType {
	for _, c := range pod.Status.Conditions {
		if c.Type == ClusterUpdatePodCondition && c.Status == corev1.ConditionTrue {
			return c.Type
		}
	}
	return ""
}

type v1PassResult struct {
	// asked counts the asks of each pod's broker.
	asked   map[string]int
	deleted []string
	// deletedBy maps each deleted pod to the pool that deleted it, unchanged
	// whether the delete required it as that pool listed it, and listed each
	// pool's pods.
	deletedBy map[string]string
	unchanged map[string]bool
	listed    map[string]map[string]types.UID
	writes    []string
	errs      []error
}

// pass runs failed-pod replacement for every node pool, as the cluster's
// reconcile does, and records what it wrote. Nothing written is read back.
func (w *v1Cluster) pass(ctx context.Context) v1PassResult {
	r := v1PassResult{deletedBy: map[string]string{}, unchanged: map[string]bool{}, asked: map[string]int{}, listed: map[string]map[string]types.UID{}}
	var mu sync.Mutex
	var pool string
	// Each pool's list, by pod, at the version it read, which kubelet's next
	// status write moves on.
	listedAt := map[string]map[string]string{}
	c := fake.NewClientBuilder().WithObjects(w.objs...).WithStatusSubresource(&corev1.Pod{}).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			pods, ok := list.(*corev1.PodList)
			if ok && w.failList {
				return errors.New("list failed")
			}
			if err := c.List(ctx, list, opts...); err != nil || !ok {
				return err
			}
			listedAt[pool], r.listed[pool] = map[string]string{}, map[string]types.UID{}
			for _, pod := range pods.Items {
				listedAt[pool][pod.Name], r.listed[pool][pod.Name] = pod.ResourceVersion, pod.UID
				if err := c.Status().Update(ctx, pod.DeepCopy()); err != nil {
					return err
				}
			}
			return nil
		},
		Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			var o client.DeleteOptions
			o.ApplyOptions(opts)
			r.deleted = append(r.deleted, obj.GetName())
			r.deletedBy[obj.GetName()] = pool
			r.unchanged[obj.GetName()] = o.Preconditions != nil && o.Preconditions.ResourceVersion != nil && o.Preconditions.UID != nil &&
				*o.Preconditions.ResourceVersion == listedAt[pool][obj.GetName()] && *o.Preconditions.UID == r.listed[pool][obj.GetName()]
			r.writes = append(r.writes, "delete")
			if w.failDelete {
				return errors.New("delete failed")
			}
			return nil
		},
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			r.writes = append(r.writes, "patch")
			return nil
		},
		SubResourcePatch: func(_ context.Context, _ client.Client, sub string, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
			r.writes = append(r.writes, sub+" patch")
			return nil
		},
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			r.writes = append(r.writes, "update")
			return nil
		},
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			r.writes = append(r.writes, "create")
			return nil
		},
	}).Build()

	factory := func(_ context.Context, _ client.Reader, _ *vectorizedv1alpha1.Cluster, _ string, _ resourcetypes.AdminTLSConfigProvider, _ redpandaclient.DialContextFunc, _ time.Duration, pods ...string) (adminutils.AdminAPIClient, error) {
		if len(pods) != 1 {
			return nil, errors.New("only pod-targeted clients are expected")
		}
		mu.Lock()
		r.asked[pods[0]]++
		mu.Unlock()
		a, ok := w.answers[pods[0]]
		if !ok || w.silent[pods[0]] {
			return nil, errors.New("no answer")
		}
		return &clusterViewAdminAPI{
			MockAdminAPI: &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")},
			all:          a.Health.AllNodes,
			down:         a.Health.NodesDown,
			brokers:      a.Brokers,
		}, nil
	}
	template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: redpandaContainerName, Image: newV1Image}}}}
	arbitration := &brokerset.Arbitration{}
	for _, np := range w.cluster.Spec.NodePools {
		pool = np.Name
		ssres := StatefulSetResource{
			Client:                c,
			pandaCluster:          w.cluster,
			nodePool:              vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: np},
			logger:                ctrl.Log.WithName("TestReplaceFailedPodPassProperties"),
			adminAPIClientFactory: factory,
			arbitration:           arbitration,
		}
		if err := ssres.replaceFailedPod(ctx, &template); err != nil {
			r.errs = append(r.errs, err)
		}
		if np.Name != w.cluster.Spec.NodePools[0].Name {
			continue
		}
		for _, name := range w.startsMidPass {
			var pod corev1.Pod
			if err := c.Get(ctx, client.ObjectKey{Namespace: w.cluster.Namespace, Name: name}, &pod); err == nil {
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}
				pod.Status.ContainerStatuses[0].Started = ptr.To(true)
				_ = c.Status().Update(ctx, &pod)
			}
		}
	}
	return r
}
