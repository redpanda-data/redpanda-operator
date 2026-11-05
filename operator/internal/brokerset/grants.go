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
	"slices"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// EnsureRollGrants serializes disruptive pod actions across the whole cluster
// (all node pools): at most one Broker holds an unexpired roll-grant at a time.
// keep is how many of this pool's Brokers its desired spec keeps.
//
// Grant lifecycle per reconcile:
//  1. Revoke completed grants, any on a Broker that won't roll, and a no-drain
//     grant on an undeleted pod the rule no longer allows or whose template
//     moved on; release expired ones; hold one while its pod terminates.
//     Re-key this pool's grants whose template went stale mid-roll IN PLACE:
//     the holder may be half drained, one broker at a time in maintenance.
//  2. Wait while any unexpired grant remains.
//  3. This pool's first outdated pod that cannot start and failedpods allows
//     gets a no-drain grant, without the health check its own outage fails,
//     even mid-decommission, unless this pass marked it.
//  4. Otherwise, unless a decommission is in flight, grant the first
//     outdated, live Broker, preferring an expired-grant holder (mid-roll),
//     only when the cluster is healthy.
//
// A no-drain grant lets the Broker controller only delete a pod that cannot
// start. It completes once the broker is back, and Ready unless another is down.
func (s *BrokerSet) EnsureRollGrants(ctx context.Context, l logr.Logger, keep int32) error {
	brokers, err := s.listClusterBrokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing cluster Broker CRs")
	}

	now := time.Now()
	activeGrants := 0
	// Seed both gates from the pass-local arbitration state: the cache
	// cannot see a decommission marked (or a grant issued by another pool's
	// invocation) earlier in this same reconcile.
	if s.Arbitration.RollGranted() {
		activeGrants++
	}
	decommissionInFlight := s.Arbitration.DecommissionMarked()

	// Every Broker's pod is gathered before any grant is decided: whether a
	// broker is up takes its running peers' word, a decommissioning one's too.
	// This pool's Brokers past keep are a scale-down's, so being removed.
	// podMissing holds no-drain grants while a deleted pod hasn't been
	// recreated; notReady names a registered Broker whose pod is on its current
	// template but not Ready.
	type liveBroker struct {
		*redpandav1alpha2.Broker
		pod        *corev1.Pod
		view       failedpods.Pod
		registered bool
	}
	var live []liveBroker
	var pods []failedpods.Pod
	views := map[string]failedpods.Pod{}
	podMissing := false
	var notReady string
	for i := range brokers {
		b := &brokers[i]

		// A Broker being decommissioned, or a DiskLost tombstone (a dead
		// incarnation), never completes a roll: a grant it still holds from
		// mid-roll must not serialize the fleet, nor sit beside another.
		if (b.Spec.Decommission || b.IsDiskLost()) && b.HasRollGrant() {
			l.Info("revoking roll-grant on a Broker that won't roll", "broker", b.Name)
			if err := s.revokeRollGrant(ctx, b); err != nil {
				return err
			}
		}

		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				decommissionInFlight = true
			}
			pod, err := s.getBrokerPod(ctx, b)
			if err != nil {
				return err
			}
			// A tombstone's pod, by name, may be its replacement's.
			if pod != nil && !b.IsDiskLost() {
				pods = append(pods, failedpods.Pod{Pod: pod, BrokerID: brokerID(b), Removing: true})
			}
			continue
		}

		if b.IsDiskLost() {
			continue
		}

		pod, err := s.getBrokerPod(ctx, b)
		if err != nil {
			return err
		}
		lb := liveBroker{Broker: b, pod: pod, registered: apimeta.IsStatusConditionTrue(b.Status.Conditions, "BrokerRegistered")}
		if pod == nil {
			podMissing = true
		} else {
			lb.view = failedpods.Pod{
				Pod:      pod,
				Pool:     b.Labels[redpandav1alpha2.NodePoolLabel],
				Current:  !b.PodOutdated(pod),
				BrokerID: brokerID(b),
				Removing: s.ownBroker(b) && ptr.Deref(b.Spec.NetworkIndex, 0) >= keep,
			}
			pods = append(pods, lb.view)
			views[b.Name] = lb.view
			if lb.view.Current && lb.registered && !utils.IsPodReady(pod) && notReady == "" {
				notReady = b.Name
			}
		}
		live = append(live, lb)
	}

	// Peers are asked only once a decision needs them.
	peers := func() failedpods.Peers {
		return s.Arbitration.Peers(func() failedpods.Peers {
			return failedpods.AskPeers(ctx, pods, s.unmigratedPool(ctx, brokers), s.Hooks.Ask)
		})
	}
	// noDrainHold returns why b's pod may not be deleted without the drain and
	// the health check, or "" if it may.
	noDrainHold := func(b *redpandav1alpha2.Broker) string {
		view, ok := views[b.Name]
		switch {
		case !ok || !failedpods.MayBeCandidate(view):
			return "its broker can start"
		case !metav1.IsControlledBy(view.Pod, b):
			// A shadow Broker mid-migration: its controller never acts on a
			// grant.
			return "the Broker doesn't control its pod"
		case DiskLostRefusal(view.Pod) != nil:
			// Its replacement would pass the guard and re-register on the
			// empty disk under a new node_id.
			return "detect-disk-lost refused its disk"
		case s.Arbitration.DecommissionMarked():
			// The cache may not show that mark yet, so the grant could land
			// on the Broker being decommissioned.
			return "a decommission was just marked"
		case podMissing:
			return "a Broker's pod is being recreated"
		case !failedpods.Candidate(view, peers()):
			return "a peer sees its broker alive, or no running broker answered"
		case failedpods.Waits(pods, view, func(failedpods.Pod) bool { return true }):
			return "another of its pool's pods is down, and replacing it first shows its template runs"
		}
		return failedpods.Hold(pods, view, peers())
	}

	var candidates []*redpandav1alpha2.Broker
	for _, lb := range live {
		b, pod, view := lb.Broker, lb.pod, lb.view

		// Roll completion additionally requires a VERIFIED registration
		// (BrokerRegistered condition, recomputed by the Broker controller
		// from a live admin-API observation each reconcile): the rotated pod
		// must have rejoined under the same node_id.
		// The grant is keyed on the pod-template SPEC hash, so a desired-spec
		// change mid-roll marks it stale (metadata changes sync in place and
		// neither need nor invalidate a grant).
		desiredHash := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		rollComplete := pod != nil &&
			!b.PodOutdated(pod) &&
			utils.IsPodReady(pod) &&
			lb.registered
		if b.HasNoDrainRollGrant() {
			// Registered too: one that never joined counts as up as soon as
			// it runs.
			rollComplete = view.Current && lb.registered && peers().Up(view) &&
				(utils.IsPodReady(pod) || peers().AnotherDown(view))
		}

		if b.HasRollGrant() {
			grantChecksum, deadline, ok := b.ParseRollGrant()
			switch {
			case !ok:
				l.Info("revoking malformed roll-grant", "broker", b.Name)
				if err := s.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case rollComplete:
				l.Info("roll complete, revoking roll-grant", "broker", b.Name)
				if err := s.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case b.HasNoDrainRollGrant() && pod != nil && pod.DeletionTimestamp == nil && !view.Current &&
				(grantChecksum != desiredHash || s.ownBroker(b) && noDrainHold(b) != ""):
				// Before its pod was deleted, the template moved on, or the
				// rule, as its own pool spells it, no longer allows it, as once
				// it came back. The grant skipped the drain and the health
				// check for a down broker on that template: the rule decides
				// again next pass, and the normal, health-gated grant can drive
				// a live roll. Its holder never drained, so nothing is
				// stranded. Once the pod is deleted, though, it holds until the
				// pod is back.
				l.Info("revoking a no-drain roll-grant decided on what has changed", "broker", b.Name)
				if err := s.revokeRollGrant(ctx, b); err != nil {
					return err
				}
				// Its deletion may be in flight, as the Broker controller
				// judged it: nothing else goes this pass, in any pool.
				s.Arbitration.MarkRollGrant()
				activeGrants++
			case !now.Before(deadline):
				// Expired: released, as the Broker controller already treats it
				// (parked, maintenance mode cleared). Re-keying it would hand a
				// live broker a grant with no health check; instead it sorts
				// first among candidates, re-granted below through the health
				// check or as a no-drain grant.
				candidates = append(candidates, b)
			case grantChecksum != desiredHash && s.ownBroker(b):
				// The desired template changed mid-roll. NEVER hand the
				// grant to another broker here: the holder may be half
				// drained, and a revocation would strand it in maintenance
				// mode — deadlocking the next grantee's drain (Redpanda
				// allows one broker in maintenance at a time). Re-key the
				// grant in place instead; the rotation applies the newest
				// template at pod recreation, so the roll converges. Only
				// its own pool's pass knows whether a scale-down removes it,
				// so whether the re-keyed grant may skip the drain. A no-drain
				// grant gets here only once its pod is gone, going, or on its
				// template: a pod is created on the newest.
				l.Info("re-keying mid-roll grant after desired-template change", "broker", b.Name)
				if err := s.grantRoll(ctx, b, desiredHash, now, b.HasNoDrainRollGrant() || noDrainHold(b) == ""); err != nil {
					return err
				}
				activeGrants++
			default:
				activeGrants++
			}
			continue
		}

		// Grants are rotation-only: disk loss has its own replacement flow.
		if pod != nil && b.PodOutdated(pod) {
			candidates = append(candidates, b)
		}
	}

	if activeGrants > 0 {
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "roll in flight, waiting for granted Broker to finish",
		}
	}
	if len(candidates) == 0 {
		// Quiescent: no roll outstanding and no grant active. If a
		// restart-requiring config change was being rolled out, it has now
		// reached every pod (V1 clears Restarting via OnQuiesced — the
		// broker-mode counterpart of the StatefulSet rolling-update path).
		return s.Hooks.OnQuiesced(ctx)
	}

	// Expired-grant holders (mid-roll) first, then node pool + index for a
	// deterministic order.
	sort.Slice(candidates, func(i, j int) bool {
		gi := candidates[i].HasRollGrant()
		gj := candidates[j].HasRollGrant()
		if gi != gj {
			return gi
		}
		pi, pj := candidates[i].Labels[redpandav1alpha2.NodePoolLabel], candidates[j].Labels[redpandav1alpha2.NodePoolLabel]
		if pi != pj {
			return pi < pj
		}
		return ptr.Deref(candidates[i].Spec.NetworkIndex, 0) < ptr.Deref(candidates[j].Spec.NetworkIndex, 0)
	})

	// The first of this pool's candidates whose pod can't start that the rule
	// allows gets a no-drain grant.
	held := ""
	for _, b := range candidates {
		if view, ok := views[b.Name]; !ok || !s.ownBroker(b) || !failedpods.MayBeCandidate(view) {
			continue
		}
		reason := noDrainHold(b)
		if reason == "" {
			l.Info("granting a no-drain roll to a pod whose broker cannot start", "broker", b.Name)
			templateHash := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
			if err := s.grantRoll(ctx, b, templateHash, now, true); err != nil {
				return err
			}
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("granted a no-drain roll to Broker %s", b.Name),
			}
		}
		if held == "" {
			held = fmt.Sprintf("%s: %s", b.Name, reason)
		}
	}
	if held != "" {
		// The normal, health-gated grant still runs in its usual order, as
		// without this feature: skipping the held pods would hand it to a
		// live broker instead, onto the template that may be failing.
		l.Info("holding a no-drain roll of a pod whose broker cannot start", "reason", held)
	}

	if decommissionInFlight {
		// Decommission progress is observed via Broker status updates, which
		// re-enqueue the owning cluster.
		l.Info("holding roll-grants while a decommission is in flight")
		return nil
	}

	// A normal roll waits for its pod to turn Ready, but a no-drain grant may
	// complete once the broker is up, so wait for that here: otherwise a
	// template whose pods never turn Ready rolls one Ready broker further.
	if notReady != "" {
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          fmt.Sprintf("waiting for Broker %s's pod to turn Ready", notReady),
		}
	}

	// confirm cluster health before granting. Returns a
	// RequeueAfterError when unhealthy.
	if err := s.Hooks.IsClusterHealthy(ctx); err != nil {
		return err
	}

	granted := candidates[0]
	templateHash := granted.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	l.Info("granting roll", "broker", granted.Name, "outstanding", len(candidates))
	if err := s.grantRoll(ctx, granted, templateHash, now, false); err != nil {
		return err
	}

	return &RequeueAfterError{
		RequeueAfter: RequeueDuration,
		Msg:          fmt.Sprintf("granted roll to Broker %s", granted.Name),
	}
}

// unmigratedPool reports whether a StatefulSet the owner controls runs a pod
// no Broker names, or more than this pass lists: a pool not yet migrated, or
// grown mid-migration, whose brokers this pass can't see. A tombstone's pod is
// its replacement's, and a failed list counts as one.
func (s *BrokerSet) unmigratedPool(ctx context.Context, brokers []redpandav1alpha2.Broker) bool {
	var sets appsv1.StatefulSetList
	if err := s.Client.List(ctx, &sets, k8sclient.InNamespace(s.Owner.GetNamespace())); err != nil {
		return true
	}
	for i := range sets.Items {
		sts := &sets.Items[i]
		if !metav1.IsControlledBy(sts, s.Owner) {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		if err != nil {
			return true
		}
		var pods corev1.PodList
		if err := s.Client.List(ctx, &pods, k8sclient.InNamespace(sts.Namespace), k8sclient.MatchingLabelsSelector{Selector: selector}); err != nil {
			return true
		}
		var owned int32
		for j := range pods.Items {
			pod := &pods.Items[j]
			if !metav1.IsControlledBy(pod, sts) {
				continue
			}
			owned++
			if !slices.ContainsFunc(brokers, func(b redpandav1alpha2.Broker) bool { return !b.IsDiskLost() && b.PodName() == pod.Name }) {
				return true
			}
		}
		if sts.Status.Replicas > owned {
			return true
		}
	}
	return false
}

// ownBroker reports whether b is in this BrokerSet's pool.
func (s *BrokerSet) ownBroker(b *redpandav1alpha2.Broker) bool {
	return b.Labels[redpandav1alpha2.NodePoolLabel] == s.PoolName
}

// brokerID returns b's registered broker ID, or nil.
func brokerID(b *redpandav1alpha2.Broker) *int {
	if b.Status.BrokerID == nil {
		return nil
	}
	return ptr.To(int(*b.Status.BrokerID))
}

// grantRoll stamps a roll-grant keyed on the given template hash with a
// fresh deadline, both for first issuance and for re-keying a mid-roll grant
// after a desired-template change. noDrain marks a no-drain grant.
func (s *BrokerSet) grantRoll(ctx context.Context, b *redpandav1alpha2.Broker, templateHash string, now time.Time, noDrain bool) error {
	p := k8sclient.MergeFrom(b.DeepCopy())
	if b.Annotations == nil {
		b.Annotations = map[string]string{}
	}
	if noDrain {
		b.SetNoDrainRollGrant(templateHash, now.Add(redpandav1alpha2.RollGrantTTL))
	} else {
		b.SetRollGrant(templateHash, now.Add(redpandav1alpha2.RollGrantTTL))
	}
	if err := s.Client.Patch(ctx, b, p); err != nil {
		return errors.Wrapf(err, "granting roll to Broker %s", b.Name)
	}
	s.Arbitration.MarkRollGrant()
	return nil
}

// revokeRollGrant revokes only the grant it read: a stale read must not
// remove one issued since, as another pool's invocation may have.
func (s *BrokerSet) revokeRollGrant(ctx context.Context, b *redpandav1alpha2.Broker) error {
	p := k8sclient.MergeFromWithOptions(b.DeepCopy(), k8sclient.MergeFromWithOptimisticLock{})
	if removed := b.RemoveRollGrant(); !removed {
		// grant was already removed, no need to patch
		return nil
	}
	if err := s.Client.Patch(ctx, b, p); err != nil {
		if apierrors.IsConflict(err) {
			return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: fmt.Sprintf("Broker %s changed since it was read", b.Name)}
		}
		return errors.Wrapf(err, "revoking roll-grant on Broker %s", b.Name)
	}
	return nil
}
