// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// TestBrokerSetPassProperties runs one reconcile pass over random cluster
// states, Ensure for every pool with a shared Arbitration as
// reconcileBrokerPools does, and checks the one-disruption-at-a-time rules on
// the result. Reads come from the pass-start state or the live one at random:
// the informer cache may or may not have caught up with the pass's own
// writes. Each property is its own check, since rapid stops a check at its
// first failure.
func TestBrokerSetPassProperties(t *testing.T) {
	ctx := t.Context()

	t.Run("AtMostOneUnexpiredGrant", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorldFor(t, "grant held into a decommission").pass(ctx, t)
		require.LessOrEqual(t, countBrokers(r.post, holdsUnexpiredGrant), 1)
	}))

	t.Run("AtMostOneDecommissionInFlight", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		require.LessOrEqual(t, countBrokers(r.post, isDecommissioning), 1)
	}))

	t.Run("NoDecommissionStartsWhileAGrantIsHeld", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		if len(r.newlyDecommissioned()) > 0 {
			require.Zero(t, countBrokers(r.post, holdsUnexpiredGrant))
		}
	}))

	t.Run("NoNormalGrantDuringADecommission", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		if countBrokers(r.post, isDecommissioning) == 0 {
			return
		}
		for _, b := range r.post {
			if r.grantIssued(b) {
				require.True(t, b.HasNoDrainRollGrant(), "normal grant to %s while a decommission is in flight", b.Name)
			}
		}
	}))

	t.Run("DecommissionIntentIsNeverCleared", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		for name, pre := range r.pre {
			if post, ok := r.post[name]; ok && pre.Spec.Decommission {
				require.True(t, post.Spec.Decommission, "cleared %s's decommission", name)
			}
		}
	}))

	// Only a Broker's own pool knows whether a scale-down removes it, which
	// decides whether it may skip the drain.
	t.Run("OnlyItsOwnPoolGrantsANoDrainRoll", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorldFor(t, "candidate in the other pool", "grant held in pb, beside its scale-down's excess").pass(ctx, t)
		for name, pool := range r.noDrainBy {
			require.Equal(t, r.pre[name].Labels[redpandav1alpha2.NodePoolLabel], pool, "pool %s gave %s a no-drain roll", pool, name)
		}
	}))

	t.Run("OnlyItsOwnPoolRekeysAGrant", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		for name, pool := range r.rekeys {
			// An expired grant is released, so a grant replacing it is new.
			if pre := r.pre[name]; pre.HasUnexpiredRollGrant() {
				require.Equal(t, pre.Labels[redpandav1alpha2.NodePoolLabel], pool, "pool %s re-keyed %s's grant", pool, name)
			}
		}
	}))

	t.Run("MidRollGrantStaysWithItsHolder", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		for name, pre := range r.pre {
			post := r.post[name]
			pod := w.pod(pre)
			// The pass may have read the template from before or after its
			// own update, so only a roll incomplete against both counts.
			if pre.IsDiskLost() || pre.Spec.Decommission || !pre.HasUnexpiredRollGrant() || pre.HasNoDrainRollGrant() ||
				post == nil || rollComplete(pre, pod) || rollComplete(post, pod) {
				continue
			}
			require.True(t, post.HasRollGrant(), "revoked %s's grant mid-roll", name)
			for other, b := range r.post {
				require.False(t, other != name && b.HasRollGrant(), "granted %s while %s is mid-roll", other, name)
			}
		}
	}))

	t.Run("ScaleDownDecommissionsTheHighestExcessBroker", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		for _, b := range r.newlyDecommissioned() {
			if b.IsDiskLost() {
				continue
			}
			pool, idx := b.Labels[redpandav1alpha2.NodePoolLabel], ptr.Deref(b.Spec.NetworkIndex, 0)
			require.GreaterOrEqual(t, idx, w.desiredReplicas(pool), "decommissioned %s at a desired index", b.Name)
			for _, o := range r.pre {
				if o.Labels[redpandav1alpha2.NodePoolLabel] == pool && !o.IsDiskLost() && ptr.Deref(o.Spec.NetworkIndex, 0) > idx {
					require.Equal(t, redpandav1alpha2.BrokerPhaseDecommissioned, o.Status.Phase, "decommissioned %s before %s", b.Name, o.Name)
				}
			}
		}
	}))

	t.Run("DiskLostTombstoneWaitsForItsReplacement", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		for _, b := range r.newlyDecommissioned() {
			pool, idx := b.Labels[redpandav1alpha2.NodePoolLabel], ptr.Deref(b.Spec.NetworkIndex, 0)
			if !b.IsDiskLost() || idx >= w.desiredReplicas(pool) {
				continue
			}
			replaced := slices.ContainsFunc(slices.Collect(maps.Values(r.pre)), func(o *redpandav1alpha2.Broker) bool {
				return !o.IsDiskLost() && o.Labels[redpandav1alpha2.NodePoolLabel] == pool &&
					ptr.Deref(o.Spec.NetworkIndex, 0) == idx && o.Status.BrokerID != nil
			})
			require.True(t, replaced, "decommissioned %s's dead id before its replacement registered", b.Name)
		}
	}))

	t.Run("NeverWritesPods", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		require.Empty(t, r.podWrites)
	}))

	t.Run("NoDrainGrantsGoOnlyToOutdatedPodsThatCannotStart", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "candidate not yet adopted")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			pod := w.pod(b)
			require.NotNil(t, pod, "%s has no pod to replace", b.Name)
			require.True(t, metav1.IsControlledBy(pod, b), "%s doesn't control its pod", b.Name)
			require.True(t, utils.ContainerFailingToStart(pod, redpandachart.RedpandaContainerName) || utils.ContainerHung(pod, redpandachart.RedpandaContainerName),
				"%s's broker can start", b.Name)
			require.True(t, b.PodOutdated(pod) || r.pre[b.Name].PodOutdated(pod), "%s's pod is already on its template", b.Name)
		}
	}))

	// Its status may say otherwise, as one whose kubelet restarted looks hung.
	// One that never joined serves nothing, up or not.
	t.Run("NoDrainGrantsNeverGoToALiveBroker", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "kubelet restarted", "kubelet restarted, alone")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			_, joined := w.nodeID(r.pre[b.Name])
			require.False(t, joined && w.alive(w.pod(b)), "granted %s, whose broker is up, a no-drain roll", b.Name)
		}
	}))

	// A later pool's pods may show one its peers' answers didn't hear from.
	t.Run("NoDrainGrantsAskEveryBrokerThatMayAnswer", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "started mid-pass")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			if r.noDrainBy[b.Name] == r.firstPool {
				continue
			}
			for _, name := range w.startsMidPass {
				require.Positive(t, r.asked[name], "granted %s a no-drain roll without asking %s, which started since its peers were asked", b.Name, name)
			}
		}
	}))

	// Each pool counts only its own scale-down's Brokers as removed, so only
	// the holder's pool knows whether the rule still allows its grant.
	t.Run("ANoDrainGrantIsDecidedByItsOwnPool", rapid.MakeCheck(func(t *rapid.T) {
		w := genDecisionWith(t, "no-drain held in pb, beside its scale-down's excess")
		w.staleReads = false
		r := w.pass(ctx, t)
		holder := w.brokers[slices.IndexFunc(w.brokers, func(b *redpandav1alpha2.Broker) bool { return b.Name == w.holder })]
		require.Equal(t, holder.Annotations[redpandav1alpha2.RollGrantAnnotation], r.post[holder.Name].Annotations[redpandav1alpha2.RollGrantAnnotation],
			"another pool revoked %s's grant", holder.Name)
	}))

	// Mid-roll, the rule may let it skip a drain it can't finish, whether its
	// template moved on since or before it went down.
	t.Run("AGrantOnAPodThatCannotStartBecomesNoDrain", rapid.MakeCheck(func(t *rapid.T) {
		w := genDecisionWith(t, rapid.SampledFrom([]string{"stale grant on the candidate", "grant held by the candidate"}).Draw(t, "change"))
		w.staleReads = false
		r := w.pass(ctx, t)
		require.True(t, r.post[w.brokers[0].Name].HasNoDrainRollGrant(), "%s: %s's grant still drains", w.change, w.brokers[0].Name)
	}))

	// The converse: the candidate the rule allows is replaced, one whose
	// replacement can run before one that can't be scheduled, unless a grant
	// it holds is re-keyed.
	t.Run("NoDrainGrantsGoToTheCandidateTheRuleAllows", rapid.MakeCheck(func(t *rapid.T) {
		w := genDecisionWith(t, rapid.SampledFrom([]string{"none", "candidate hung", "candidate unschedulable", "candidate unschedulable beside one that runs"}).Draw(t, "change"))
		r := w.pass(ctx, t)
		// One that can't be scheduled waits for a crash-looping one.
		want := w.brokers[0]
		if strings.HasPrefix(w.change, "candidate unschedulable") {
			for _, b := range w.brokers[1:] {
				if b.Labels[redpandav1alpha2.NodePoolLabel] == "pa" && b.PodOutdated(w.pod(b)) && w.states[b.PodName()] == "crash-looping" {
					want = b
					break
				}
			}
		}
		if holder := w.brokers[0]; holder.HasRollGrant() && want != holder {
			// Its grant stays in place, so nothing else goes.
			require.False(t, r.post[holder.Name].HasNoDrainRollGrant(), "%s: re-keyed %s's grant to skip its drain while it waits", w.change, holder.Name)
			return
		}
		require.True(t, r.post[want.Name].HasNoDrainRollGrant(), "%s: no no-drain grant for %s", w.change, want.Name)
	}))

	// A Broker past its pool's desired replicas is a scale-down's: replacing
	// its pod would only restart it on the template the scale-down froze.
	t.Run("NoDrainGrantsNeverGoToABrokerAScaleDownRemoves", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "scale-down removes a failing pod")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			require.Less(t, ptr.Deref(b.Spec.NetworkIndex, 0), w.desiredReplicas(b.Labels[redpandav1alpha2.NodePoolLabel]),
				"granted %s, which a scale-down removes, a no-drain roll", b.Name)
		}
	}))

	t.Run("NoDrainGrantsWaitWhileAPodIsTerminatingOrMissing", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		if len(r.newNoDrainGrants()) == 0 {
			return
		}
		for _, b := range r.pre {
			if b.IsDiskLost() {
				continue
			}
			pod := w.pod(b)
			require.False(t, pod == nil && !b.Spec.Decommission, "replaced a pod while %s's is missing", b.Name)
			// But one stuck on a node that's gone, which won't finish.
			require.False(t, pod != nil && pod.DeletionTimestamp != nil && w.states[pod.Name] != "node gone", "replaced a pod while %s's is terminating", b.Name)
		}
	}))

	// Moving a pod onto a template whose pods all fail to run only adds one to
	// them.
	t.Run("NoDrainGrantsNeverMoveOntoATemplateNothingRuns", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "template down", "reported down", "hung seen alive")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			// The pass may have read either side of its own spec updates.
			require.False(t, w.nothingRuns(r.pre, b) && w.nothingRuns(r.post, b),
				"replaced %s onto a template none of its pool's pods runs", b.Name)
		}
	}))

	// A template that can't be scheduled on some node may not fit the
	// candidate's.
	t.Run("NoDrainGrantsNeverMoveOntoATemplateAPodCantBeScheduledOn", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			require.False(t, w.unschedulableOn(r.pre, b) && w.unschedulableOn(r.post, b),
				"replaced %s onto a template one of its pool's pods can't be scheduled on", b.Name)
		}
	}))

	// Only a no-drain grant skips the health check, and an expired grant
	// is released, so a normal grant renewed without one is an in-place
	// re-key of a grant still running.
	t.Run("NormalGrantsPassTheHealthCheckUnlessRekeyed", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		for _, b := range r.post {
			if !r.grantIssued(b) || b.HasNoDrainRollGrant() {
				continue
			}
			if pre, ok := r.pre[b.Name]; ok && pre.HasUnexpiredRollGrant() {
				continue
			}
			require.True(t, w.hooks().healthy, "granted %s a roll without the health check", b.Name)
		}
	}))

	// Or, before its pod was deleted, once its template moved on: the rule
	// decides again.
	t.Run("NoDrainGrantIsReleasedOnlyOnceItsBrokerIsBack", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "no-drain held")
		r := w.pass(ctx, t)
		for name, pre := range r.pre {
			post := r.post[name]
			// One that won't roll loses its grant.
			if !pre.HasNoDrainRollGrant() || pre.IsDiskLost() || pre.Spec.Decommission || post == nil || post.Spec.Decommission || post.HasRollGrant() {
				continue
			}
			pod := w.pod(pre)
			outdated := pod != nil && (pre.PodOutdated(pod) || post.PodOutdated(pod))
			// Its deletion may be in flight. Before that, the rule decides
			// again, which AHeldNoDrainGrantIsReleasedOnceItsBrokerIsBack spells.
			require.False(t, outdated && pod.DeletionTimestamp != nil, "released %s's no-drain grant while its pod is being deleted", name)
			if outdated {
				continue
			}
			require.True(t, pod != nil && w.up(pre), "released %s's no-drain grant while its broker isn't back", name)
			// Back on its template, it rejoined, and Ready waits on every
			// other broker.
			require.True(t, apimeta.IsStatusConditionTrue(pre.Status.Conditions, "BrokerRegistered"), "released %s's no-drain grant before it registered", name)
			if !utils.IsPodReady(pod) {
				require.True(t, w.anotherDown(pre), "released %s's no-drain grant before Ready with every other broker up", name)
			}
		}
	}))

	// And the converse, where a decision drew a no-drain grant held: it is
	// released once its broker is back on its template, registered, and Ready
	// or with another down; or, before its pod was deleted, once its template
	// moved on or the rule no longer allows it, as once its broker is back or
	// its template is down, and then decided again. One whose pod is being
	// deleted holds, re-keyed if its template moved on.
	t.Run("AHeldNoDrainGrantIsReleasedOnceItsBrokerIsBack", rapid.MakeCheck(func(t *rapid.T) {
		w := genDecisionWith(t, "no-drain held")
		// A revocation off a stale read conflicts, to retry next pass.
		w.staleReads = false
		r := w.pass(ctx, t)
		holder, post := w.brokers[0], r.post[w.brokers[0].Name]
		pod := w.pod(holder)
		outdated := holder.PodOutdated(pod)
		registered := apimeta.IsStatusConditionTrue(holder.Status.Conditions, "BrokerRegistered")
		state := w.states[pod.Name]
		others := slices.DeleteFunc(slices.Clone(w.brokers), func(o *redpandav1alpha2.Broker) bool { return o == holder })
		pool := holder.Labels[redpandav1alpha2.NodePoolLabel]
		// One that can't be scheduled waits for another of its pool,
		// outdated, that can be and can't start.
		waits := state == "unschedulable" && slices.ContainsFunc(others, func(o *redpandav1alpha2.Broker) bool {
			p := w.pod(o)
			return p != nil && o.Labels[redpandav1alpha2.NodePoolLabel] == pool && o.PodOutdated(p) && !o.Spec.Decommission &&
				ptr.Deref(o.Spec.NetworkIndex, 0) < w.desiredReplicas(pool) && (w.states[p.Name] == "crash-looping" || w.states[p.Name] == "between crashes")
		})
		// Replacements go one at a time, once every Broker has its pod.
		settling := slices.ContainsFunc(others, func(o *redpandav1alpha2.Broker) bool {
			p := w.pod(o)
			return !o.IsDiskLost() && !o.Spec.Decommission && p == nil || p != nil && w.states[p.Name] == "terminating"
		})
		allowed := (state == "crash-looping" || state == "unschedulable" && (!w.othersMayAnswer(holder) || len(w.ids(holder)) > 0) && !waits ||
			(state == "hung" || state == "kubelet restarted") && w.othersMayAnswer(holder)) &&
			!w.seenAlive(holder) && !w.unheard(holder) && !w.nothingRuns(r.pre, holder) && !w.unschedulableOn(r.pre, holder) && !settling
		done := outdated && pod.DeletionTimestamp == nil && (!allowed || grantStale(holder, holder)) ||
			!outdated && registered && w.up(holder) && (utils.IsPodReady(pod) || w.anotherDown(holder))
		kept := post.HasNoDrainRollGrant() &&
			(post.Annotations[redpandav1alpha2.RollGrantAnnotation] == holder.Annotations[redpandav1alpha2.RollGrantAnnotation] || r.rekeys[holder.Name] != "")
		require.Equal(t, !done, kept, "%s: kept %s's no-drain grant", w.change, holder.Name)
		if done && outdated {
			require.False(t, r.quiesced, "%s: dropped %s from the roll", w.change, holder.Name)
			// Its pod's deletion may be in flight: the rule decides next pass.
			for _, b := range r.post {
				require.False(t, pod.DeletionTimestamp == nil && r.grantIssued(b), "%s: granted %s as %s's grant was revoked", w.change, b.Name, holder.Name)
			}
		}
	}))

	// One that never joined and can't be scheduled keeps no gate failing: the
	// gated roll replaces it.
	t.Run("NoDrainGrantsToPodsThatCantBeScheduledGoOnlyToMembers", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "candidate never joined")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			if pre := r.pre[b.Name]; w.othersMayAnswer(pre) && w.states[pre.PodName()] == "unschedulable" {
				require.NotEmpty(t, w.ids(pre), "granted %s, whose broker never joined, a no-drain roll", b.Name)
			}
		}
	}))

	// Pod status lags; only the peers' word says a broker is down.
	t.Run("NoDrainGrantsTakeThePeersWord", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "unmigrated pool")
		r := w.pass(ctx, t)
		for _, b := range r.newNoDrainGrants() {
			pre := r.pre[b.Name]
			require.False(t, w.seenAlive(pre), "granted %s a no-drain roll, which a peer sees alive", b.Name)
			require.False(t, w.unheard(pre), "granted %s a no-drain roll, which no running broker answered for", b.Name)
		}
	}))

	// A no-drain grant may finish before Ready, so the next normal roll waits
	// for Ready itself.
	t.Run("NoNormalGrantWhileARegisteredPodOnItsTemplateIsntReady", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "unready current")
		r := w.pass(ctx, t)
		for _, b := range r.post {
			if pre, ok := r.pre[b.Name]; !r.grantIssued(b) || b.HasNoDrainRollGrant() || ok && pre.HasUnexpiredRollGrant() {
				continue
			}
			for name, o := range r.pre {
				pod, post := w.pod(o), r.post[name]
				if o.IsDiskLost() || o.Spec.Decommission || pod == nil || post == nil || o.PodOutdated(pod) || post.PodOutdated(pod) ||
					!apimeta.IsStatusConditionTrue(o.Status.Conditions, "BrokerRegistered") {
					continue
				}
				require.True(t, utils.IsPodReady(pod), "granted %s a roll while %s isn't Ready", b.Name, name)
			}
		}
	}))

	// Quiescing ends the roll (V1 clears Restarting), so it waits for every pod.
	t.Run("NeverQuiescesWithAnOutdatedPod", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorld(t)
		r := w.pass(ctx, t)
		if !r.quiesced {
			return
		}
		for name, pre := range r.pre {
			pod, post := w.pod(pre), r.post[name]
			if pre.IsDiskLost() || pre.Spec.Decommission || pod == nil || post == nil || post.Spec.Decommission {
				continue
			}
			require.False(t, pre.PodOutdated(pod) && post.PodOutdated(pod), "quiesced with %s's pod outdated", name)
		}
	}))

	// A broker left unasked loses its say exactly while the cluster is
	// degraded, one asked twice costs a timeout twice, and one that can't
	// answer costs a timeout for nothing.
	t.Run("AsksEachBrokerThatCanAnswerOnce", rapid.MakeCheck(func(t *rapid.T) {
		w := genWorldFor(t, "lost disk")
		r := w.pass(ctx, t)
		if len(r.asked) == 0 {
			return
		}
		for _, b := range r.pre {
			pod := w.pod(b)
			// A tombstone's pod is its replacement's.
			if b.IsDiskLost() || pod == nil {
				continue
			}
			want := 0
			if w.answers(pod) {
				want = 1
			}
			require.Equal(t, want, r.asked[pod.Name], "asked %s's pod %d times", b.Name, r.asked[pod.Name])
		}
	}))

	t.Run("NoDrainGrantAndDecommissionNeverShareAPass", rapid.MakeCheck(func(t *rapid.T) {
		r := genWorld(t).pass(ctx, t)
		require.False(t, len(r.newNoDrainGrants()) > 0 && len(r.newlyDecommissioned()) > 0,
			"started a no-drain roll and a decommission in one pass")
	}))
}

// TestBrokerSetConvergedIsQuiescent: Brokers and pods that already match the
// desired pools must pass without a single write, or the controller writes on
// every reconcile.
func TestBrokerSetConvergedIsQuiescent(t *testing.T) {
	ctx := t.Context()

	rapid.Check(t, func(t *rapid.T) {
		r := genConvergedWorld(t).pass(ctx, t)
		require.Empty(t, r.errs)
		require.Empty(t, r.writes)
		require.True(t, r.quiesced)
		// Not even an outbound ask: only a decision warrants one.
		require.Empty(t, r.asked)
	})
}

const (
	propertyNamespace  = "test"
	propertyOwnerLabel = "test/owner"
	// Network indexes below propertyIndexes may hold a Broker.
	propertyIndexes = 4
)

var (
	propertyPools    = []string{"pa", "pb"}
	propertyVersions = []string{"v1", "v2"}
)

// propertyWorld is a cluster's Broker CRs and pods at the start of a pass.
type propertyWorld struct {
	scheme  *runtime.Scheme
	pools   []propertyPool
	brokers []*redpandav1alpha2.Broker
	pods    map[string]*corev1.Pod
	// states is each pod's state, which says whether its broker can answer:
	// one that runs, has started, and whose node is still there.
	states  map[string]string
	healthy bool
	// silent maps the pods whose broker doesn't answer.
	silent map[string]bool
	// unmigrated is a pool still run from a StatefulSet, whose brokers no
	// Broker names.
	unmigrated bool
	// misreports are where the answers part from the pods' state, as a stale
	// status or a broker its peers can't reach would have them.
	misreports []misreport
	// change is the one change a decision world made, "" in a random one.
	change string
	// staleReads serves the pass's reads from the pass-start state.
	staleReads bool
	// startsMidPass are pods whose brokers start, Ready, once the first pool
	// has run.
	startsMidPass []string
	// unrecorded maps a Broker whose broker joined to the ID its status hasn't
	// recorded, as while its outdated pod waits for a grant.
	unrecorded map[string]int
	// holder names the Broker a decision gave a grant in another pool than pa.
	holder string
}

// misreport has the answers of by, or of every broker if "", report broker's
// broker alive or down whatever its pod's state.
type misreport struct {
	broker, by string
	alive      bool
}

type propertyPool struct {
	name string
	// desired is nil for a pool removed from the spec.
	desired  *appsv1.StatefulSet
	replicas int32
}

// genWorld draws two pools of up to four Brokers in any mix of states the
// operator can leave behind: live or DiskLost, registered or not, specs and
// pods on either of two templates, with at most one roll-grant and one
// decommission in flight. Half the time it draws a decision instead, so each
// rule's case comes up.
func genWorld(t *rapid.T) *propertyWorld {
	if rapid.Bool().Draw(t, "decision") {
		return genDecision(t)
	}
	w := newPropertyWorld(t)
	var nextID int32
	for _, pool := range propertyPools {
		p := propertyPool{name: pool}
		if rapid.IntRange(0, 4).Draw(t, pool+" in spec") > 0 {
			p.desired = poolStatefulSet(pool, rapid.SampledFrom(propertyVersions).Draw(t, pool+" desired version"))
			p.replicas = rapid.Int32Range(0, propertyIndexes-1).Draw(t, pool+" desired replicas")
		}
		w.pools = append(w.pools, p)

		rendered := renderPool(t, w.scheme, pool)
		for idx := range int32(propertyIndexes) {
			key := fmt.Sprintf("%s/%d", pool, idx)
			switch rapid.SampledFrom([]string{"empty", "live", "live", "live", "disk lost", "released", "released and replaced"}).Draw(t, key) {
			case "live":
				w.addLive(t, key, rendered, idx, &nextID)
			case "disk lost":
				w.addTombstone(t, key, rendered, idx, false, &nextID)
			case "released":
				w.addTombstone(t, key, rendered, idx, true, &nextID)
			case "released and replaced":
				w.addTombstone(t, key, rendered, idx, true, &nextID)
				w.addLive(t, key, rendered, idx, &nextID)
			}
		}
	}
	w.addDecommissions(t)
	w.addGrant(t)
	for _, name := range slices.Sorted(maps.Keys(w.pods)) {
		w.silent[name] = rapid.IntRange(0, 3).Draw(t, name+" silent") == 0
	}
	w.unmigrated = rapid.IntRange(0, 5).Draw(t, "unmigrated pool") == 0
	return w
}

// decisionChanges are the one change genDecision makes, each a case a rule
// turns on.
var decisionChanges = []string{
	"none", "terminating", "missing", "unschedulable", "template down", "scale-down",
	"decommission in flight", "grant held into a decommission", "lost disk", "seen alive", "split views",
	"reported down", "silent", "starting", "hung", "hung seen alive", "node gone", "unmigrated pool", "no-drain held", "normal grant",
	"candidate hung", "kubelet restarted", "kubelet restarted, alone", "candidate unschedulable", "candidate not yet adopted",
	"candidate unschedulable beside one that runs", "candidate never joined",
	"stale grant on the candidate", "grant held by the candidate",
	"unready current", "candidate in the other pool", "scale-down removes a failing pod",
	"grant held in pb, beside its scale-down's excess",
}

// genWorldFor draws a world as genWorld does, but half the time the decision
// one of changes makes, for a property about rules only they turn on.
func genWorldFor(t *rapid.T, changes ...string) *propertyWorld {
	if rapid.Bool().Draw(t, "its decision") {
		return genDecisionWith(t, rapid.SampledFrom(changes).Draw(t, "change"))
	}
	return genWorld(t)
}

func genDecision(t *rapid.T) *propertyWorld {
	return genDecisionWith(t, rapid.SampledFrom(decisionChanges).Draw(t, "change"))
}

// genDecisionWith draws the case the no-drain rules decide on: both pools in
// the spec, every Broker registered on its pool's template, and in pool pa an
// outdated crash-looping candidate (pa/0) beside a current Broker that's Ready
// (pa/1), every broker reporting each as it is; then change, a case a rule
// turns on.
func genDecisionWith(t *rapid.T, change string) *propertyWorld {
	w := newPropertyWorld(t)
	w.change = change
	var nextID int32
	rendered := map[string]map[string][]redpandav1alpha2.Broker{}
	desired := map[string]string{}
	set := func(b *redpandav1alpha2.Broker, version, state string) {
		pool := b.Labels[redpandav1alpha2.NodePoolLabel]
		w.putPod(b, &rendered[pool][version][ptr.Deref(b.Spec.NetworkIndex, 0)], state)
	}
	other := func(pool string) string {
		return propertyVersions[1-slices.Index(propertyVersions, desired[pool])]
	}
	for _, pool := range propertyPools {
		desired[pool] = rapid.SampledFrom(propertyVersions).Draw(t, pool+" desired version")
		rendered[pool] = renderPool(t, w.scheme, pool)
		least := int32(1)
		if pool == "pb" && w.change == "started mid-pass" {
			least = 2
		}
		if pool == "pa" {
			least = 2
			if w.change == "unschedulable" || strings.HasPrefix(w.change, "scale-down") || w.change == "candidate unschedulable beside one that runs" {
				least = 3
			}
		}
		pbScalesDown := strings.HasSuffix(w.change, "in pb, beside its scale-down's excess")
		if pool == "pb" && pbScalesDown {
			least = 3
		}
		n := rapid.Int32Range(least, propertyIndexes).Draw(t, pool+" brokers")
		if pool == "pb" && pbScalesDown {
			n = 3
		}
		replicas := n
		if pool == "pa" && strings.HasPrefix(w.change, "scale-down") || pool == "pb" && pbScalesDown {
			replicas--
		}
		w.pools = append(w.pools, propertyPool{name: pool, desired: poolStatefulSet(pool, desired[pool]), replicas: replicas})
		for idx := range n {
			key := fmt.Sprintf("%s/%d", pool, idx)
			b := newLiveBroker(&rendered[pool][desired[pool]][idx], newBrokerID(&nextID), true)
			w.brokers = append(w.brokers, b)
			switch {
			case pool == "pa" && idx == 0:
				set(b, other(pool), "crash-looping")
			case pool == "pa" && idx == 1:
				set(b, desired[pool], "ready")
			default:
				version := desired[pool]
				if rapid.Bool().Draw(t, key+" outdated") {
					version = other(pool)
				}
				set(b, version, rapid.SampledFrom([]string{"ready", "ready", "unready", "crash-looping"}).Draw(t, key+" pod"))
			}
		}
	}
	candidate, current := w.brokers[0], w.brokers[1]
	rest := w.brokers[2:]
	pick := func(from []*redpandav1alpha2.Broker, label string) *redpandav1alpha2.Broker {
		return from[rapid.IntRange(0, len(from)-1).Draw(t, label)]
	}

	switch w.change {
	case "terminating":
		b := pick(w.brokers, "terminating")
		pod := w.pod(b)
		pod.DeletionTimestamp, pod.Finalizers = ptr.To(metav1.Now()), []string{"test/hold"}
	case "missing":
		delete(w.pods, pick(w.brokers[1:], "missing").PodName())
	case "unschedulable":
		set(w.brokers[2], desired["pa"], "unschedulable")
	case "template down":
		for _, b := range w.brokers[1:] {
			if b.Labels[redpandav1alpha2.NodePoolLabel] == "pa" && !b.PodOutdated(w.pod(b)) {
				set(b, desired["pa"], "crash-looping")
			}
		}
	case "decommission in flight", "grant held into a decommission":
		b := pick(w.brokers[1:], "decommissioning")
		b.Spec.Decommission, b.Status.Phase = true, redpandav1alpha2.BrokerPhaseDecommissioning
		if w.change == "grant held into a decommission" {
			b.SetRollGrant(b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation], time.Now().Add(5*time.Minute))
		}
	case "lost disk":
		// A disk lost under a Broker since replaced, its dead ID being
		// decommissioned.
		b := pick(w.brokers[1:], "replaced")
		pool := b.Labels[redpandav1alpha2.NodePoolLabel]
		w.addTombstone(t, b.Name, rendered[pool], ptr.Deref(b.Spec.NetworkIndex, 0), true, &nextID)
		tombstone := w.brokers[len(w.brokers)-1]
		tombstone.Status.BrokerID = newBrokerID(&nextID)
		tombstone.Spec.Decommission = true
	case "seen alive":
		// Its status is stale: it is back.
		w.misreports = append(w.misreports, misreport{broker: candidate.Name, alive: true})
	case "split views":
		w.misreports = append(w.misreports, misreport{broker: candidate.Name, alive: true, by: current.PodName()})
	case "reported down":
		w.misreports = append(w.misreports, misreport{broker: current.Name})
	case "silent":
		for name := range w.pods {
			w.silent[name] = true
		}
	case "starting", "hung", "node gone":
		// The current broker can't answer: still starting, hung starting, or
		// on a node that's gone with its status reading running.
		set(current, desired["pa"], w.change)
	case "candidate hung":
		set(candidate, other("pa"), "hung")
	case "candidate unschedulable", "candidate unschedulable beside one that runs":
		set(candidate, other("pa"), "unschedulable")
		if w.change == "candidate unschedulable beside one that runs" {
			set(w.brokers[2], other("pa"), "crash-looping")
		}
	case "candidate never joined":
		candidate.Status.BrokerID, candidate.Status.Conditions, candidate.Status.Phase = nil, nil, redpandav1alpha2.BrokerPhasePending
		if rapid.Bool().Draw(t, "unschedulable") {
			set(candidate, other("pa"), "unschedulable")
		}
	case "candidate not yet adopted":
		// Mid-migration, its StatefulSet was orphan-deleted, and its Broker
		// hasn't adopted its pod.
		w.pod(candidate).OwnerReferences = nil
	case "stale grant on the candidate", "grant held by the candidate":
		// Its template moved on mid-roll; it never could drain. Held, it was
		// re-keyed before its broker was seen failing.
		checksum := "stale"
		if w.change == "grant held by the candidate" {
			checksum = candidate.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		}
		candidate.SetRollGrant(checksum, time.Now().Add(5*time.Minute))
	case "no-drain held in pb, beside its scale-down's excess":
		// pb/0 holds a grant its pool allows: pb/1 runs on the old template
		// too, and pb/2, on its own, is one pb's scale-down removes. Another
		// pool sees pb/2 as on the current template, and down.
		pb := slices.DeleteFunc(slices.Clone(w.brokers), func(b *redpandav1alpha2.Broker) bool { return b.Labels[redpandav1alpha2.NodePoolLabel] != "pb" })
		set(pb[0], other("pb"), "crash-looping")
		set(pb[1], other("pb"), "ready")
		set(pb[2], desired["pb"], "crash-looping")
		pb[0].SetNoDrainRollGrant(pb[0].Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation], time.Now().Add(5*time.Minute))
		w.holder = pb[0].Name
	case "grant held in pb, beside its scale-down's excess":
		// pb/0 holds a grant keyed on its template, and is down; on that
		// template pb/1 is down too, and pb/2, which pb's scale-down removes,
		// runs. Only another pool sees that template as shown to run.
		pb := slices.DeleteFunc(slices.Clone(w.brokers), func(b *redpandav1alpha2.Broker) bool { return b.Labels[redpandav1alpha2.NodePoolLabel] != "pb" })
		set(pb[0], other("pb"), "crash-looping")
		set(pb[1], desired["pb"], "crash-looping")
		set(pb[2], desired["pb"], "ready")
		pb[0].SetRollGrant(pb[0].Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation], time.Now().Add(5*time.Minute))
		w.holder = pb[0].Name

	case "kubelet restarted", "kubelet restarted, alone":
		// It runs, but looks hung. Alone, every other broker is down, and its
		// pool has no pod on its template to hold it. Its Broker may not have
		// recorded its ID: its peers know it by address.
		set(candidate, other("pa"), "kubelet restarted")
		if rapid.Bool().Draw(t, "its ID unrecorded") {
			w.unrecorded[candidate.Name] = int(*candidate.Status.BrokerID)
			candidate.Status.BrokerID, candidate.Status.Conditions = nil, nil
		}
		if w.change == "kubelet restarted, alone" {
			for _, b := range w.brokers[1:] {
				version := desired[b.Labels[redpandav1alpha2.NodePoolLabel]]
				if b.Labels[redpandav1alpha2.NodePoolLabel] == "pa" {
					version = other("pa")
				}
				set(b, version, "crash-looping")
			}
		}
	case "started mid-pass":
		// Nothing answers when pool pa asks, then holds; pb/1 starts before
		// pool pb decides on what pa was told.
		for _, b := range w.brokers[1:] {
			pool := b.Labels[redpandav1alpha2.NodePoolLabel]
			set(b, desired[pool], "crash-looping")
		}
		pb := slices.DeleteFunc(slices.Clone(w.brokers), func(b *redpandav1alpha2.Broker) bool { return b.Labels[redpandav1alpha2.NodePoolLabel] != "pb" })
		set(pb[0], other("pb"), "crash-looping")
		if len(pb) > 1 {
			set(pb[1], desired["pb"], "starting")
			w.startsMidPass = append(w.startsMidPass, pb[1].PodName())
		}
	case "hung seen alive":
		// It heartbeats, but never starts: its template isn't shown to run.
		set(current, desired["pa"], "hung")
		w.misreports = append(w.misreports, misreport{broker: current.Name, alive: true})
	case "unmigrated pool":
		// Its brokers may be the only ones that answer.
		w.unmigrated = true
		for _, b := range w.brokers[1:] {
			set(b, other(b.Labels[redpandav1alpha2.NodePoolLabel]), "crash-looping")
		}
	case "no-drain held":
		w.change += ", " + rapid.SampledFrom([]string{
			"still down", "back and Ready", "back and Ready but unregistered", "back but not Ready", "back between crashes",
			"back on its old template", "back alone", "back alone beside brokers that never joined", "template moved on",
			"back as its pod is deleted", "template moved on as its pod is deleted", "back, every peer silent", "back, its peers split",
			"back alone beside an unmigrated pool", "back, only it seeing another down", "still down, its template down since",
			"still down by its status, seen alive", "still down, every peer silent", "template moved on beside another that can't start",
			"still down, another's pod terminating", "still down, another's pod missing", "unschedulable beside another that can't start",
		}).Draw(t, "holder")
		checksum := candidate.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		if strings.Contains(w.change, "template moved on") {
			checksum = "stale"
		}
		candidate.SetNoDrainRollGrant(checksum, time.Now().Add(5*time.Minute))
		// The holder is the one outdated, so a pass that drops it from the
		// roll quiesces.
		for _, b := range w.brokers[1:] {
			state := "ready"
			if strings.Contains(w.change, "back alone") {
				w.unmigrated = strings.HasSuffix(w.change, "unmigrated pool")
				state = rapid.SampledFrom([]string{"crash-looping", "starting", "hung", "node gone"}).Draw(t, b.Name+" down")
			}
			if strings.HasSuffix(w.change, "never joined") {
				b.Status.BrokerID, b.Status.Conditions, b.Status.Phase = nil, nil, redpandav1alpha2.BrokerPhasePending
			}
			set(b, desired[b.Labels[redpandav1alpha2.NodePoolLabel]], state)
		}
		if len(rest) > 0 && rapid.Bool().Draw(t, "another down") {
			b := pick(rest, "down")
			set(b, desired[b.Labels[redpandav1alpha2.NodePoolLabel]], "crash-looping")
		}
		switch {
		case strings.HasSuffix(w.change, "back and Ready"):
			set(candidate, desired["pa"], "ready")
		case strings.HasSuffix(w.change, "back and Ready but unregistered"):
			set(candidate, desired["pa"], "ready")
			candidate.Status.Conditions = nil
		case strings.HasSuffix(w.change, "back but not Ready"), strings.Contains(w.change, "back alone"):
			set(candidate, desired["pa"], "unready")
		case strings.HasSuffix(w.change, "back between crashes"):
			set(candidate, desired["pa"], "between crashes")
		case strings.HasSuffix(w.change, "back on its old template"):
			set(candidate, other("pa"), "ready")
		case strings.HasSuffix(w.change, "as its pod is deleted"):
			// Deleted as its container started: it runs as it shuts down.
			set(candidate, other("pa"), "terminating")
		case strings.HasSuffix(w.change, "beside another that can't start"):
			if strings.HasPrefix(w.change, "no-drain held, unschedulable") {
				set(candidate, other("pa"), "unschedulable")
			}
			if len(rest) > 0 && rest[0].Labels[redpandav1alpha2.NodePoolLabel] == "pa" {
				set(rest[0], other("pa"), "crash-looping")
			}
		case strings.HasSuffix(w.change, "another's pod terminating"):
			if len(rest) > 0 {
				b := pick(rest, "terminating")
				set(b, desired[b.Labels[redpandav1alpha2.NodePoolLabel]], "terminating")
			}
		case strings.HasSuffix(w.change, "another's pod missing"):
			if len(rest) > 0 {
				delete(w.pods, pick(rest, "missing").PodName())
			}
		case strings.HasSuffix(w.change, "by its status, seen alive"):
			// Its kubelet restarted: it looks hung, and runs.
			set(candidate, other("pa"), "kubelet restarted")
		case strings.HasSuffix(w.change, "still down, every peer silent"):
			for _, b := range w.brokers[1:] {
				w.silent[b.PodName()] = true
			}
		case strings.HasSuffix(w.change, "its template down since"):
			for _, b := range w.brokers[1:] {
				if b.Labels[redpandav1alpha2.NodePoolLabel] == "pa" {
					set(b, desired["pa"], "crash-looping")
				}
			}
		case strings.HasSuffix(w.change, "every peer silent"):
			set(candidate, desired["pa"], "ready")
			for _, b := range w.brokers[1:] {
				w.silent[b.PodName()] = true
			}
		case strings.HasSuffix(w.change, "its peers split"):
			set(candidate, desired["pa"], "ready")
			w.misreports = append(w.misreports, misreport{broker: candidate.Name, by: current.PodName()})
		case strings.HasSuffix(w.change, "only it seeing another down"):
			set(candidate, desired["pa"], "unready")
			w.misreports = append(w.misreports, misreport{broker: current.Name, by: candidate.PodName()})
		}
	case "normal grant":
		b := pick(w.brokers, "grantee")
		checksum := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		if rapid.Bool().Draw(t, "grant stale") {
			checksum = "stale"
		}
		deadline := time.Now().Add(5 * time.Minute)
		if rapid.Bool().Draw(t, "grant expired") {
			deadline = time.Now().Add(-time.Minute)
		}
		b.SetRollGrant(checksum, deadline)
	case "scale-down removes a failing pod":
		// The one failing pod is the one the scale-down removes, and a
		// decommission already in flight leaves its own unmarked this pass.
		for _, b := range w.brokers {
			set(b, desired[b.Labels[redpandav1alpha2.NodePoolLabel]], "ready")
		}
		excess := w.brokers[slices.IndexFunc(w.brokers, func(b *redpandav1alpha2.Broker) bool {
			return b.Labels[redpandav1alpha2.NodePoolLabel] == "pa" && ptr.Deref(b.Spec.NetworkIndex, 0) == w.desiredReplicas("pa")
		})]
		set(excess, other("pa"), "crash-looping")
		b := w.brokers[slices.IndexFunc(w.brokers, func(b *redpandav1alpha2.Broker) bool { return b.Labels[redpandav1alpha2.NodePoolLabel] == "pb" })]
		b.Spec.Decommission, b.Status.Phase = true, redpandav1alpha2.BrokerPhaseDecommissioning
	case "unready current", "candidate in the other pool":
		// Only one Broker outdated, its pod running, so the roll's next step
		// is the health-gated grant; or, in pool pb, a no-drain one.
		for _, b := range w.brokers[1:] {
			set(b, desired[b.Labels[redpandav1alpha2.NodePoolLabel]], "ready")
		}
		set(candidate, other("pa"), "ready")
		if w.change == "unready current" {
			set(current, desired["pa"], "unready")
			w.healthy = true
			break
		}
		b := w.brokers[slices.IndexFunc(w.brokers, func(b *redpandav1alpha2.Broker) bool { return b.Labels[redpandav1alpha2.NodePoolLabel] == "pb" })]
		set(b, other("pb"), "crash-looping")
	}
	// Or the candidate held a grant, its template since outran or not, so the
	// rule decides on a re-key or a switch to no-drain too.
	if !slices.Contains([]string{"normal grant", "grant held into a decommission", "decommission in flight", "lost disk", "stale grant on the candidate", "grant held by the candidate", "scale-down", "scale-down removes a failing pod", "started mid-pass", "grant held in pb, beside its scale-down's excess"}, w.change) &&
		!strings.HasPrefix(w.change, "no-drain held") && rapid.Bool().Draw(t, "via a held grant") {
		checksum := "stale"
		if rapid.Bool().Draw(t, "keyed on its template") {
			checksum = candidate.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		}
		candidate.SetRollGrant(checksum, time.Now().Add(5*time.Minute))
	}
	return w
}

func newPropertyWorld(t *rapid.T) *propertyWorld {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))
	return &propertyWorld{
		scheme:     scheme,
		pods:       map[string]*corev1.Pod{},
		states:     map[string]string{},
		silent:     map[string]bool{},
		unrecorded: map[string]int{},
		healthy:    rapid.Bool().Draw(t, "cluster healthy"),
		staleReads: rapid.Bool().Draw(t, "stale reads"),
	}
}

func poolStatefulSet(pool, version string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-" + pool, Namespace: propertyNamespace},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "rp",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{redpandav1alpha2.BrokerConfigChecksumAnnotation: "config-" + version},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: redpandachart.RedpandaContainerName, Image: "redpanda:" + version}},
					Volumes: []corev1.Volume{{Name: redpanda.DatadirVolumeName, VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: redpanda.DatadirVolumeName},
					}}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: redpanda.DatadirVolumeName}}},
		},
	}
}

// renderPool renders every index of pool on each template version.
func renderPool(t *rapid.T, scheme *runtime.Scheme, pool string) map[string][]redpandav1alpha2.Broker {
	rendered := map[string][]redpandav1alpha2.Broker{}
	for _, version := range propertyVersions {
		brokers, err := poolBrokerSet(scheme, nil, nil, nil, pool).RenderBrokers(poolStatefulSet(pool, version), propertyIndexes, false)
		require.NoError(t, err)
		rendered[version] = brokers
	}
	return rendered
}

func poolBrokerSet(scheme *runtime.Scheme, c k8sclient.Client, hooks OwnerHooks, arbitration *Arbitration, pool string) *BrokerSet {
	poolLabels := map[string]string{propertyOwnerLabel: "rp", redpandav1alpha2.NodePoolLabel: pool}
	brokerLabels := maps.Clone(poolLabels)
	brokerLabels[redpandav1alpha2.ClusterNameLabel] = "rp"
	return &BrokerSet{
		Client:            c,
		Scheme:            scheme,
		Owner:             propertyOwner(),
		ClusterRef:        redpandav1alpha2.ClusterRef{Name: "rp"},
		PoolName:          pool,
		BrokerLabels:      brokerLabels,
		PoolSelector:      k8slabels.SelectorFromSet(poolLabels),
		ClusterSelector:   k8slabels.SelectorFromSet(map[string]string{propertyOwnerLabel: "rp"}),
		PodSelector:       k8slabels.SelectorFromSet(poolLabels),
		ConfigChecksumKey: redpandav1alpha2.BrokerConfigChecksumAnnotation,
		Hooks:             hooks,
		Arbitration:       arbitration,
		Logger:            logr.Discard(),
	}
}

func propertyOwner() *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: propertyNamespace, UID: "owner-uid"}}
}

func (w *propertyWorld) addLive(t *rapid.T, key string, rendered map[string][]redpandav1alpha2.Broker, idx int32, nextID *int32) {
	w.addLiveOn(t, key, rendered, rapid.SampledFrom(propertyVersions).Draw(t, key+" spec version"), idx, nextID)
}

func (w *propertyWorld) addLiveOn(t *rapid.T, key string, rendered map[string][]redpandav1alpha2.Broker, version string, idx int32, nextID *int32) {
	src := rendered[version][idx]
	var b *redpandav1alpha2.Broker
	switch rapid.SampledFrom([]string{"registered", "registered", "unregistered", "no identity"}).Draw(t, key+" identity") {
	case "registered":
		b = newLiveBroker(&src, newBrokerID(nextID), true)
	case "unregistered":
		b = newLiveBroker(&src, newBrokerID(nextID), false)
	default:
		b = newLiveBroker(&src, nil, false)
		if rapid.Bool().Draw(t, key+" joined") {
			w.unrecorded[b.Name] = int(*newBrokerID(nextID))
		}
	}
	w.brokers = append(w.brokers, b)

	// Weighted toward crash loops, and away from the missing and terminating
	// pods that hold every no-drain grant, so its rules see cases.
	state := rapid.SampledFrom([]string{
		"missing", "ready", "ready", "ready", "unready", "starting", "hung", "kubelet restarted", "between crashes", "crash-looping",
		"crash-looping", "crash-looping", "pending", "unschedulable", "terminating", "node gone",
	}).Draw(t, key+" pod")
	if state != "missing" {
		podSrc := rendered[rapid.SampledFrom(propertyVersions).Draw(t, key+" pod version")][idx]
		w.putPod(b, &podSrc, state)
	}
}

// putPod gives b a pod built from src's template, in state.
func (w *propertyWorld) putPod(b, src *redpandav1alpha2.Broker, state string) {
	pod := newBrokerPod(b, src, state)
	w.pods[pod.Name] = pod
	w.states[pod.Name] = state
}

func newBrokerID(next *int32) *int32 {
	id := *next
	*next++
	return &id
}

func newLiveBroker(src *redpandav1alpha2.Broker, id *int32, registered bool) *redpandav1alpha2.Broker {
	b := src.DeepCopy()
	b.Name = b.PodName() + "-live"
	b.UID = types.UID(b.Name)
	b.Status.Phase = redpandav1alpha2.BrokerPhaseRunning
	b.Status.BrokerID = id
	if id == nil {
		b.Status.Phase = redpandav1alpha2.BrokerPhasePending
	}
	if registered {
		b.Status.Conditions = []metav1.Condition{{
			Type:               "BrokerRegistered",
			Status:             metav1.ConditionTrue,
			Reason:             "Registered",
			LastTransitionTime: metav1.Now(),
		}}
	}
	return b
}

// newBrokerPod builds b's pod from src's template, so it is outdated when src
// renders a different template version than b.
func newBrokerPod(b, src *redpandav1alpha2.Broker, state string) *corev1.Pod {
	src = src.DeepCopy()
	src.Status.BrokerID = b.Status.BrokerID
	pod := src.BuildPod(b.PodName())
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: redpandav1alpha2.GroupVersion.String(),
		Kind:       "Broker",
		Name:       b.Name,
		UID:        b.UID,
		Controller: ptr.To(true),
	}}

	now := time.Now()
	status := corev1.ContainerStatus{
		Name:    redpandachart.RedpandaContainerName,
		State:   corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-time.Hour))}},
		Started: ptr.To(true),
	}
	pod.Status = corev1.PodStatus{Phase: corev1.PodRunning, Conditions: podConditions(corev1.ConditionFalse)}
	switch state {
	case "ready", "terminating":
		status.Ready = true
		pod.Status.Conditions = podConditions(corev1.ConditionTrue)
		if state == "terminating" {
			pod.DeletionTimestamp = ptr.To(metav1.NewTime(now))
			pod.Finalizers = []string{"test/hold"}
		}
	case "starting":
		// Its startup probe hasn't passed yet: no admin API.
		status.State.Running.StartedAt = metav1.NewTime(now.Add(-time.Minute))
		status.Started = ptr.To(false)
	case "hung", "kubelet restarted":
		// Nor in a run that long; or its kubelet reset Started, until the
		// probe next passes.
		status.State.Running.StartedAt = metav1.NewTime(now.Add(-15 * time.Minute))
		status.Started = ptr.To(false)
	case "node gone":
		// Evicted from a node that's gone, its last status still running.
		pod.DeletionTimestamp = ptr.To(metav1.NewTime(now.Add(-10 * time.Minute)))
		pod.Finalizers = []string{"test/hold"}
	case "crash-looping", "between crashes":
		status.LastTerminationState = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			ExitCode:   1,
			StartedAt:  metav1.NewTime(now.Add(-40 * time.Second)),
			FinishedAt: metav1.NewTime(now.Add(-30 * time.Second)),
		}}
		status.State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
		if state == "between crashes" {
			status.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-20 * time.Second))}}
		}
	case "pending":
		pod.Status = corev1.PodStatus{Phase: corev1.PodPending}
		return pod
	case "unschedulable":
		pod.Status = corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute))},
		}}
		return pod
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{status}
	return pod
}

func podConditions(status corev1.ConditionStatus) []corev1.PodCondition {
	return []corev1.PodCondition{
		{Type: corev1.PodReady, Status: status},
		{Type: corev1.ContainersReady, Status: status},
	}
}

func (w *propertyWorld) addTombstone(t *rapid.T, key string, rendered map[string][]redpandav1alpha2.Broker, idx int32, released bool, nextID *int32) {
	b := rendered[propertyVersions[0]][idx].DeepCopy()
	b.Name = b.PodName() + "-lost"
	b.UID = types.UID(b.Name)
	b.Status.Phase = redpandav1alpha2.BrokerPhaseDiskLost
	b.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: released}
	if rapid.Bool().Draw(t, key+" dead id recorded") {
		b.Status.BrokerID = newBrokerID(nextID)
	}
	w.brokers = append(w.brokers, b)
}

// addDecommissions marks some Brokers already decommissioned and at most one
// in flight.
func (w *propertyWorld) addDecommissions(t *rapid.T) {
	for _, b := range w.brokers {
		if rapid.IntRange(0, 7).Draw(t, b.Name+" decommissioned") > 0 {
			continue
		}
		b.Spec.Decommission = true
		b.Status.Phase = redpandav1alpha2.BrokerPhaseDecommissioned
		b.Status.BrokerID = nil
		b.Status.Conditions = nil
		delete(w.unrecorded, b.Name)
		if !b.IsDiskLost() {
			delete(w.pods, b.PodName())
		}
	}

	candidates := slices.DeleteFunc(slices.Clone(w.brokers), func(b *redpandav1alpha2.Broker) bool { return b.Spec.Decommission })
	if len(candidates) == 0 || !rapid.Bool().Draw(t, "decommission in flight") {
		return
	}
	b := candidates[rapid.IntRange(0, len(candidates)-1).Draw(t, "decommissioning")]
	b.Spec.Decommission = true
	if !b.IsDiskLost() {
		b.Status.Phase = redpandav1alpha2.BrokerPhaseDecommissioning
	}
}

// addGrant gives at most one Broker a roll-grant: no-drain or normal,
// expired or not, keyed on its template or a stale one, perhaps on a Broker
// decommissioned since. A normal grant beside another Broker's decommission
// is expired, since none is issued during one.
func (w *propertyWorld) addGrant(t *rapid.T) {
	candidates := slices.DeleteFunc(slices.Clone(w.brokers), func(b *redpandav1alpha2.Broker) bool { return b.IsDiskLost() })
	// Weighted toward a live normal grant: the rules with the most branches
	// are about one.
	if len(candidates) == 0 || rapid.IntRange(0, 3).Draw(t, "grant held") == 0 {
		return
	}
	b := candidates[rapid.IntRange(0, len(candidates)-1).Draw(t, "grantee")]

	checksum := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	if rapid.Bool().Draw(t, "grant stale") {
		checksum = "stale"
	}
	noDrain := rapid.IntRange(0, 2).Draw(t, "no-drain grant") == 0
	deadline := time.Now().Add(5 * time.Minute)
	if rapid.IntRange(0, 2).Draw(t, "grant expired") == 0 || !noDrain && slices.ContainsFunc(w.brokers, func(o *redpandav1alpha2.Broker) bool { return o != b && isDecommissioning(o) }) {
		deadline = time.Now().Add(-time.Minute)
	}
	if noDrain {
		b.SetNoDrainRollGrant(checksum, deadline)
	} else {
		b.SetRollGrant(checksum, deadline)
	}
}

// passResult is the cluster before and after one pass.
type passResult struct {
	pre, post map[string]*redpandav1alpha2.Broker
	pods      map[string]*corev1.Pod
	// asked counts the asks of each pod's broker.
	asked map[string]int
	// rekeys maps each grant the pass moved to another template to the pool
	// that moved it, noDrainBy each no-drain grant it wrote.
	rekeys, noDrainBy map[string]string
	writes            []string
	podWrites         []string
	errs              []error
	quiesced          bool
	// firstPool is the pool that ran before w's startsMidPass started.
	firstPool string
}

// pass runs Ensure for every pool, desired pools first, then those removed
// from the spec, as reconcileBrokerPools does.
func (w *propertyWorld) pass(ctx context.Context, t *rapid.T) passResult {
	live := w.client()
	c := &passClient{Client: live, reads: live, rekeys: map[string]string{}, noDrainBy: map[string]string{}}
	if w.staleReads {
		c.reads = w.client()
	}
	hooks := w.hooks()
	arbitration := &Arbitration{}

	r := passResult{pre: map[string]*redpandav1alpha2.Broker{}, post: map[string]*redpandav1alpha2.Broker{}, pods: w.pods}
	for _, b := range w.brokers {
		r.pre[b.Name] = b
	}

	ensureAll := func() {
		for _, removed := range []bool{false, true} {
			for _, p := range w.pools {
				if (p.desired == nil) != removed {
					continue
				}
				key := types.NamespacedName{Namespace: propertyNamespace, Name: "rp-" + p.name}
				c.pool = p.name
				err := poolBrokerSet(w.scheme, c, hooks, arbitration, p.name).Ensure(ctx, key, p.desired.DeepCopy(), p.replicas)
				if r.firstPool == "" {
					r.firstPool = p.name
					w.startMidPass(ctx, t, live, c.reads)
				}
				if err == nil {
					continue
				}
				r.errs = append(r.errs, err)
				// Any other error abandons the pass.
				var requeue *RequeueAfterError
				if !errors.As(err, &requeue) {
					return
				}
			}
		}
	}
	ensureAll()

	var list redpandav1alpha2.BrokerList
	require.NoError(t, live.List(ctx, &list))
	for i := range list.Items {
		r.post[list.Items[i].Name] = &list.Items[i]
	}
	r.writes, r.podWrites, r.quiesced, r.asked, r.rekeys, r.noDrainBy = c.writes, c.podWrites, hooks.quiesced, hooks.asked, c.rekeys, c.noDrainBy
	return r
}

// startMidPass has w's startsMidPass start, as both of the pass's views
// show.
func (w *propertyWorld) startMidPass(ctx context.Context, t *rapid.T, views ...k8sclient.Reader) {
	for _, name := range w.startsMidPass {
		for _, view := range slices.Compact(views) {
			var pod corev1.Pod
			require.NoError(t, view.Get(ctx, types.NamespacedName{Namespace: propertyNamespace, Name: name}, &pod))
			pod.Status.Conditions = podConditions(corev1.ConditionTrue)
			pod.Status.ContainerStatuses[0].Started, pod.Status.ContainerStatuses[0].Ready = ptr.To(true), true
			require.NoError(t, view.(k8sclient.Client).Status().Update(ctx, &pod))
		}
	}
}

func (w *propertyWorld) client() k8sclient.Client {
	objs := []k8sclient.Object{propertyOwner()}
	for _, b := range w.brokers {
		objs = append(objs, b.DeepCopy())
	}
	for _, pod := range w.pods {
		objs = append(objs, pod.DeepCopy())
	}
	if w.unmigrated {
		sts := poolStatefulSet("pc", propertyVersions[0])
		sts.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "rp", UID: propertyOwner().UID, Controller: ptr.To(true)}}
		sts.Status.Replicas = 1
		objs = append(objs, sts)
	}
	return fake.NewClientBuilder().
		WithScheme(w.scheme).
		WithStatusSubresource(&redpandav1alpha2.Broker{}).
		WithObjects(objs...).
		Build()
}

// passClient serves reads from reads, the informer cache's view, and sends
// writes to the live client, recording them.
type passClient struct {
	k8sclient.Client
	reads     k8sclient.Reader
	writes    []string
	podWrites []string
	// pool is the pool whose invocation is running; rekeys records, for each
	// grant a pass moved to another template, the pool that moved it, and
	// noDrainBy for each no-drain grant it wrote.
	pool              string
	rekeys, noDrainBy map[string]string
}

func (c *passClient) Get(ctx context.Context, key k8sclient.ObjectKey, obj k8sclient.Object, opts ...k8sclient.GetOption) error {
	return c.reads.Get(ctx, key, obj, opts...)
}

func (c *passClient) List(ctx context.Context, list k8sclient.ObjectList, opts ...k8sclient.ListOption) error {
	return c.reads.List(ctx, list, opts...)
}

func (c *passClient) Create(ctx context.Context, obj k8sclient.Object, opts ...k8sclient.CreateOption) error {
	err := c.Client.Create(ctx, obj, opts...)
	c.record("create", obj)
	return err
}

func (c *passClient) Update(ctx context.Context, obj k8sclient.Object, opts ...k8sclient.UpdateOption) error {
	c.record("update", obj)
	return c.Client.Update(ctx, obj, opts...)
}

func (c *passClient) Patch(ctx context.Context, obj k8sclient.Object, patch k8sclient.Patch, opts ...k8sclient.PatchOption) error {
	c.record("patch", obj)
	if b, ok := obj.(*redpandav1alpha2.Broker); ok {
		var live redpandav1alpha2.Broker
		if err := c.Client.Get(ctx, k8sclient.ObjectKeyFromObject(b), &live); err == nil {
			if live.HasRollGrant() && b.HasRollGrant() {
				before, _, _ := live.ParseRollGrant()
				if after, _, _ := b.ParseRollGrant(); after != before {
					c.rekeys[b.Name] = c.pool
				}
			}
			if b.HasNoDrainRollGrant() && live.Annotations[redpandav1alpha2.RollGrantAnnotation] != b.Annotations[redpandav1alpha2.RollGrantAnnotation] {
				c.noDrainBy[b.Name] = c.pool
			}
		}
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *passClient) Delete(ctx context.Context, obj k8sclient.Object, opts ...k8sclient.DeleteOption) error {
	c.record("delete", obj)
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *passClient) record(op string, obj k8sclient.Object) {
	write := fmt.Sprintf("%s %T %s", op, obj, obj.GetName())
	c.writes = append(c.writes, write)
	if _, ok := obj.(*corev1.Pod); ok {
		c.podWrites = append(c.podWrites, write)
	}
}

// hooks answers for each broker with its view, and the cluster healthy only
// when drawn so and none is down.
func (w *propertyWorld) hooks() *propertyHooks {
	h := &propertyHooks{world: w, silent: w.silent, asked: map[string]int{}}
	h.healthy = w.healthy && len(w.view("").NodesDown) == 0
	return h
}

// view is what by's broker answers: every member, down unless its broker is
// up, but as w's misreports have it.
func (w *propertyWorld) view(by string) rpadmin.ClusterHealthOverview {
	var health rpadmin.ClusterHealthOverview
	for _, b := range w.brokers {
		id, ok := w.nodeID(b)
		if !ok {
			continue
		}
		health.AllNodes = append(health.AllNodes, id)
		pod := w.pod(b)
		down := b.IsDiskLost() || pod == nil || !w.alive(pod)
		for _, m := range w.misreports {
			if m.broker == b.Name && (m.by == "" || m.by == by) {
				down = !m.alive
			}
		}
		if down {
			health.NodesDown = append(health.NodesDown, id)
		}
	}
	return health
}

// nodeID is the ID b's broker joined as, recorded or not.
func (w *propertyWorld) nodeID(b *redpandav1alpha2.Broker) (int, bool) {
	if b.Status.BrokerID != nil {
		return int(*b.Status.BrokerID), true
	}
	id, ok := w.unrecorded[b.Name]
	return id, ok
}

// members is every member's registration, at its pod's address: a
// tombstone's dead ID at its replacement's.
func (w *propertyWorld) members() []rpadmin.Broker {
	var members []rpadmin.Broker
	for _, b := range w.brokers {
		if id, ok := w.nodeID(b); ok {
			members = append(members, rpadmin.Broker{NodeID: id, InternalRPCAddress: b.PodName() + ".rp." + propertyNamespace + ".svc:33145"})
		}
	}
	return members
}

// ids is what the rule can know of b's broker: its recorded ID, or, once a
// broker answers, every member registered at its pod's address.
func (w *propertyWorld) ids(b *redpandav1alpha2.Broker) []int {
	if b.Status.BrokerID != nil {
		return []int{int(*b.Status.BrokerID)}
	}
	pod := w.pod(b)
	if len(w.answering(b)) == 0 && (pod == nil || !w.answers(pod) || w.silent[pod.Name]) {
		return nil
	}
	var ids []int
	for _, o := range w.brokers {
		if id, ok := w.nodeID(o); ok && o.PodName() == b.PodName() && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// pod returns the pod named after b, which a DiskLost tombstone shares with
// its replacement.
func (w *propertyWorld) pod(b *redpandav1alpha2.Broker) *corev1.Pod {
	return w.pods[b.PodName()]
}

// answers reports whether pod's broker can answer, as the rule can see: it
// runs, has started, and its node is still there.
func (w *propertyWorld) answers(pod *corev1.Pod) bool {
	return slices.Contains([]string{"ready", "unready", "between crashes", "terminating"}, w.states[pod.Name])
}

// alive reports whether pod's broker is up, as its peers see: one whose
// kubelet restarted is, though it doesn't look it.
func (w *propertyWorld) alive(pod *corev1.Pod) bool {
	return pod != nil && (w.answers(pod) || w.states[pod.Name] == "kubelet restarted")
}

// recent reports whether pod's broker is crash looping, between crashes or not.
func (w *propertyWorld) recent(pod *corev1.Pod) bool {
	return w.states[pod.Name] == "crash-looping" || w.states[pod.Name] == "between crashes"
}

// propertyHooks is an owner whose brokers each answer with their view, but
// the silent ones, which don't answer.
type propertyHooks struct {
	world    *propertyWorld
	healthy  bool
	silent   map[string]bool
	quiesced bool
	mu       sync.Mutex
	asked    map[string]int
}

func (h *propertyHooks) IsClusterHealthy(context.Context) error {
	if h.healthy {
		return nil
	}
	return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "cluster unhealthy"}
}

func (h *propertyHooks) Ask(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
	h.mu.Lock()
	h.asked[pod.Name]++
	h.mu.Unlock()
	if h.silent[pod.Name] {
		return failedpods.Answer{}, errors.New("no answer")
	}
	return failedpods.Answer{Health: h.world.view(pod.Name), Brokers: h.world.members()}, nil
}

func (h *propertyHooks) OnQuiesced(context.Context) error {
	h.quiesced = true
	return nil
}

func (h *propertyHooks) MigrationBlockedReason(context.Context) string { return "" }

func countBrokers(brokers map[string]*redpandav1alpha2.Broker, match func(*redpandav1alpha2.Broker) bool) int {
	n := 0
	for _, b := range brokers {
		if match(b) {
			n++
		}
	}
	return n
}

func holdsUnexpiredGrant(b *redpandav1alpha2.Broker) bool {
	return !b.IsDiskLost() && b.HasUnexpiredRollGrant()
}

func isDecommissioning(b *redpandav1alpha2.Broker) bool {
	return b.Spec.Decommission && b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned
}

func (r passResult) newlyDecommissioned() []*redpandav1alpha2.Broker {
	var marked []*redpandav1alpha2.Broker
	for name, b := range r.post {
		if pre, ok := r.pre[name]; b.Spec.Decommission && (!ok || !pre.Spec.Decommission) {
			marked = append(marked, b)
		}
	}
	return marked
}

// newNoDrainGrants returns the Brokers whose no-drain grant the pass wrote,
// but for one re-keyed in place once its pod is gone, going, or on its
// template, as either side of the pass's spec updates spells it: that lets
// nothing more be deleted.
func (r passResult) newNoDrainGrants() []*redpandav1alpha2.Broker {
	var granted []*redpandav1alpha2.Broker
	for name, b := range r.post {
		if !b.HasNoDrainRollGrant() || !r.grantIssued(b) {
			continue
		}
		if pre, ok := r.pre[name]; ok && pre.HasNoDrainRollGrant() && r.rekeys[name] != "" {
			if pod := r.pods[b.PodName()]; pod == nil || pod.DeletionTimestamp != nil || !pre.PodOutdated(pod) || !b.PodOutdated(pod) {
				continue
			}
		}
		granted = append(granted, b)
	}
	return granted
}

// grantIssued reports whether the pass issued or renewed b's roll-grant.
func (r passResult) grantIssued(b *redpandav1alpha2.Broker) bool {
	if !b.HasRollGrant() {
		return false
	}
	pre, ok := r.pre[b.Name]
	return !ok || pre.Annotations[redpandav1alpha2.RollGrantAnnotation] != b.Annotations[redpandav1alpha2.RollGrantAnnotation]
}

// grantStale reports whether held's grant is keyed on a template other than
// b's.
func grantStale(held, b *redpandav1alpha2.Broker) bool {
	checksum, _, _ := held.ParseRollGrant()
	return checksum != b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
}

// rollComplete is when a normal grant's roll is done: the pod is back on b's
// template, Ready, and its broker re-registered.
func rollComplete(b *redpandav1alpha2.Broker, pod *corev1.Pod) bool {
	return pod != nil && !b.PodOutdated(pod) && utils.IsPodReady(pod) &&
		apimeta.IsStatusConditionTrue(b.Status.Conditions, "BrokerRegistered")
}

// nothingRuns reports whether b's pool has Brokers on their template, and
// none of them is up.
func (w *propertyWorld) nothingRuns(brokers map[string]*redpandav1alpha2.Broker, b *redpandav1alpha2.Broker) bool {
	// Only a member's broker shows its template runs; once a member answers,
	// one that never joined, waiting to, shows nothing either way.
	membersAnswered := slices.ContainsFunc(w.brokers, func(o *redpandav1alpha2.Broker) bool {
		pod := w.pod(o)
		return !o.IsDiskLost() && pod != nil && w.answers(pod) && !w.silent[pod.Name] && len(w.ids(o)) > 0
	})
	on := slices.DeleteFunc(w.onTemplate(brokers, b), func(o *redpandav1alpha2.Broker) bool {
		state := w.states[w.pod(o).Name]
		return membersAnswered && len(w.ids(o)) == 0 && state != "crash-looping" && state != "between crashes" && state != "unschedulable"
	})
	return len(on) > 0 && !slices.ContainsFunc(on, func(o *redpandav1alpha2.Broker) bool { return len(w.ids(o)) > 0 && w.up(o) })
}

// unschedulableOn reports whether a pod of b's pool on its template can't be
// scheduled.
func (w *propertyWorld) unschedulableOn(brokers map[string]*redpandav1alpha2.Broker, b *redpandav1alpha2.Broker) bool {
	return slices.ContainsFunc(w.onTemplate(brokers, b), func(o *redpandav1alpha2.Broker) bool {
		return slices.ContainsFunc(w.pod(o).Status.Conditions, func(c corev1.PodCondition) bool {
			return c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse
		})
	})
}

// onTemplate lists the Brokers of b's pool whose pods are on their template,
// as brokers spells it (each Broker's differs: it names the Broker's own
// hostname and volumes), but those a decommission or scale-down removes, and
// those on a node that's gone, which says nothing of the template.
func (w *propertyWorld) onTemplate(brokers map[string]*redpandav1alpha2.Broker, b *redpandav1alpha2.Broker) []*redpandav1alpha2.Broker {
	pool := b.Labels[redpandav1alpha2.NodePoolLabel]
	var on []*redpandav1alpha2.Broker
	for _, o := range brokers {
		pod := w.pod(o)
		if o.IsDiskLost() || o.Spec.Decommission || pod == nil || w.states[pod.Name] == "node gone" || o.Labels[redpandav1alpha2.NodePoolLabel] != pool ||
			ptr.Deref(o.Spec.NetworkIndex, 0) >= w.desiredReplicas(pool) || o.PodOutdated(pod) {
			continue
		}
		on = append(on, o)
	}
	return on
}

// others lists the pods besides b's of every live or decommissioning Broker:
// a DiskLost tombstone's pod is its replacement's.
func (w *propertyWorld) others(b *redpandav1alpha2.Broker) []*corev1.Pod {
	var pods []*corev1.Pod
	for _, o := range w.brokers {
		if pod := w.pod(o); !o.IsDiskLost() && o.PodName() != b.PodName() && pod != nil && !slices.Contains(pods, pod) {
			pods = append(pods, pod)
		}
	}
	return pods
}

// voices lists those of them whose broker can answer.
func (w *propertyWorld) voices(b *redpandav1alpha2.Broker) []string {
	var names []string
	for _, pod := range w.others(b) {
		if w.answers(pod) {
			names = append(names, pod.Name)
		}
	}
	return names
}

// answering lists those of them that answer.
func (w *propertyWorld) answering(b *redpandav1alpha2.Broker) []string {
	return slices.DeleteFunc(w.voices(b), func(name string) bool { return w.silent[name] })
}

// othersMayAnswer reports whether a broker besides b's can answer, seen or
// not.
func (w *propertyWorld) othersMayAnswer(b *redpandav1alpha2.Broker) bool {
	return w.unmigrated || len(w.voices(b)) > 0
}

// up is whether b's broker can answer and isn't between crashes, and, with
// another broker that can answer, one answering reports it alive and none
// down: with others but none answering it isn't.
func (w *propertyWorld) up(b *redpandav1alpha2.Broker) bool {
	pod := w.pod(b)
	if pod == nil || !w.answers(pod) || w.recent(pod) {
		return false
	}
	if !w.othersMayAnswer(b) {
		return true
	}
	ids := w.ids(b)
	if len(ids) == 0 || len(w.answering(b)) == 0 {
		return false
	}
	for _, name := range w.answering(b) {
		if slices.ContainsFunc(ids, func(id int) bool { return slices.Contains(w.view(name).NodesDown, id) }) {
			return false
		}
	}
	return true
}

// seenAlive is whether a broker besides b's answers that b's is alive.
func (w *propertyWorld) seenAlive(b *redpandav1alpha2.Broker) bool {
	ids := w.ids(b)
	return slices.ContainsFunc(w.answering(b), func(name string) bool {
		return slices.ContainsFunc(ids, func(id int) bool { return !slices.Contains(w.view(name).NodesDown, id) })
	})
}

// unheard is whether brokers besides b's can answer but none does.
func (w *propertyWorld) unheard(b *redpandav1alpha2.Broker) bool {
	return w.othersMayAnswer(b) && len(w.answering(b)) == 0
}

// anotherDown is whether a broker besides b's answers that one besides b's is
// down, or, with no other broker that can answer, b's own does: a member, as
// one that never joined isn't.
func (w *propertyWorld) anotherDown(b *redpandav1alpha2.Broker) bool {
	ids := w.ids(b)
	another := func(name string) bool {
		return slices.ContainsFunc(w.view(name).NodesDown, func(id int) bool { return !slices.Contains(ids, id) })
	}
	if !w.othersMayAnswer(b) {
		pod := w.pod(b)
		return pod != nil && w.answers(pod) && !w.silent[pod.Name] && another(pod.Name)
	}
	return len(ids) > 0 && slices.ContainsFunc(w.answering(b), another)
}

func (w *propertyWorld) desiredReplicas(pool string) int32 {
	for _, p := range w.pools {
		if p.name == pool && p.desired != nil {
			return p.replicas
		}
	}
	return 0
}

// genConvergedWorld draws pools whose Brokers all run their desired template,
// registered and Ready, with nothing granted or decommissioning.
func genConvergedWorld(t *rapid.T) *propertyWorld {
	w := newPropertyWorld(t)
	var nextID int32
	for _, pool := range propertyPools {
		version := rapid.SampledFrom(propertyVersions).Draw(t, pool+" version")
		p := propertyPool{
			name:     pool,
			desired:  poolStatefulSet(pool, version),
			replicas: rapid.Int32Range(0, propertyIndexes-1).Draw(t, pool+" replicas"),
		}
		w.pools = append(w.pools, p)

		rendered := renderPool(t, w.scheme, pool)[version]
		for idx := range p.replicas {
			b := newLiveBroker(&rendered[idx], newBrokerID(&nextID), true)
			w.brokers = append(w.brokers, b)
			w.putPod(b, &rendered[idx], "ready")
		}
	}
	return w
}
