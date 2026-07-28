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
	"sort"
	"time"

	"github.com/go-logr/logr"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// EnsureRollGrants serializes disruptive pod actions across the whole cluster
// (all node pools): at most one Broker holds a valid roll-grant at any time.
// The cluster controller is the single writer of the annotation — it grants
// (health-gated) and revokes; the Broker controller only reads it.
//
// Grant lifecycle per reconcile:
//  1. Revoke grants that completed (pod matches the desired template, is
//     ready, and the broker is registered). A grant whose template hash went
//     stale mid-roll (desired-template change) is re-keyed IN PLACE — never
//     handed to another broker, since the holder may be half drained and
//     only one broker may be in maintenance mode at a time.
//  2. If any unexpired grant remains, wait — never double-grant. Expired
//     grants are treated as released (feature.RollGrantTTL is a safety valve
//     against controller restarts and wedged rolls).
//  3. Hold off while any decommission is in flight: one disruptive operation
//     at a time.
//  4. Grant the first Broker that needs a roll (outdated pod or Stuck phase),
//     preferring a broker with an expired grant (mid-roll), only when the
//     cluster is healthy.
func (s *BrokerSet) EnsureRollGrants(ctx context.Context, l logr.Logger) error {
	brokers, err := s.listClusterBrokers(ctx)
	if err != nil {
		return fmt.Errorf("listing cluster Broker CRs: %w", err)
	}

	now := time.Now()
	activeGrants := 0
	decommissionInFlight := false
	var candidates []*redpandav1alpha2.Broker

	for i := range brokers {
		b := &brokers[i]

		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				decommissionInFlight = true
			}
			continue
		}

		pod, err := s.getBrokerPod(ctx, b)
		if err != nil {
			return err
		}

		// Roll completion additionally requires a VERIFIED registration
		// (BrokerRegistered condition, recomputed by the Broker controller
		// from a live admin-API observation each reconcile): the rotated pod
		// must have rejoined under the same node_id (RFC rolling step 3).
		// The grant is keyed on the pod-template SPEC hash, so a desired-spec
		// change mid-roll marks it stale (metadata changes sync in place and
		// neither need nor invalidate a grant).
		desiredHash := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
		rollComplete := pod != nil &&
			!b.PodOutdated(pod) &&
			utils.IsPodReady(pod) &&
			apimeta.IsStatusConditionTrue(b.Status.Conditions, "BrokerRegistered")

		if grant := b.Annotations[feature.RollGrant.Key]; grant != "" {
			grantChecksum, deadline, ok := feature.ParseRollGrant(grant)
			switch {
			case !ok:
				l.Info("revoking malformed roll-grant", "broker", b.Name, "grant", grant)
				if err := s.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case rollComplete:
				l.Info("roll complete, revoking roll-grant", "broker", b.Name)
				if err := s.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case grantChecksum != desiredHash:
				// The desired template changed mid-roll. NEVER hand the
				// grant to another broker here: the holder may be half
				// drained, and a revocation would strand it in maintenance
				// mode — deadlocking the next grantee's drain (Redpanda
				// allows one broker in maintenance at a time). Re-key the
				// grant in place instead; the rotation applies the newest
				// template at pod recreation, so the roll converges.
				l.Info("re-keying mid-roll grant after desired-template change", "broker", b.Name)
				if err := s.grantRoll(ctx, b, desiredHash, now); err != nil {
					return err
				}
				activeGrants++
			case now.Before(deadline):
				activeGrants++
			default:
				// Expired grant on an unfinished roll: treated as released.
				// The broker sorts first among candidates and is re-granted
				// below, after a fresh health check.
				candidates = append(candidates, b)
			}
			continue
		}

		// Stuck brokers are roll candidates only when the pod is
		// unschedulable (the PV-affinity remediation case, where deleting
		// pod+PVC helps). Other Stuck classes — crash loops, image pull
		// failures, identity conflicts — would not be fixed by a rotation
		// and need an operator, not a grant.
		stuckUnschedulable := b.Status.Phase == redpandav1alpha2.BrokerPhaseStuck &&
			pod != nil && podUnschedulable(pod)
		needsRoll := (pod != nil && b.PodOutdated(pod)) || stuckUnschedulable
		if needsRoll {
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
		if s.OnQuiesced != nil {
			return s.OnQuiesced(ctx)
		}
		return nil
	}
	if decommissionInFlight {
		// Decommission progress is observed via Broker status updates, which
		// re-enqueue the owning cluster.
		l.Info("holding roll-grants while a decommission is in flight")
		return nil
	}

	// Expired-grant holders (mid-roll) first, then node pool + index for a
	// deterministic order.
	sort.Slice(candidates, func(i, j int) bool {
		gi := candidates[i].Annotations[feature.RollGrant.Key] != ""
		gj := candidates[j].Annotations[feature.RollGrant.Key] != ""
		if gi != gj {
			return gi
		}
		pi, pj := candidates[i].Labels[redpandav1alpha2.NodePoolLabel], candidates[j].Labels[redpandav1alpha2.NodePoolLabel]
		if pi != pj {
			return pi < pj
		}
		return ptr.Deref(candidates[i].Spec.NetworkIndex, 0) < ptr.Deref(candidates[j].Spec.NetworkIndex, 0)
	})

	// RFC step 1: confirm cluster health before granting. Returns a
	// RequeueAfterError when unhealthy.
	if s.IsClusterHealthy != nil {
		if err := s.IsClusterHealthy(ctx); err != nil {
			return err
		}
	}

	granted := candidates[0]
	templateHash := granted.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	l.Info("granting roll", "broker", granted.Name, "outstanding", len(candidates))
	if err := s.grantRoll(ctx, granted, templateHash, now); err != nil {
		return err
	}

	return &RequeueAfterError{
		RequeueAfter: RequeueDuration,
		Msg:          fmt.Sprintf("granted roll to Broker %s", granted.Name),
	}
}

// grantRoll stamps a roll-grant keyed on the given template hash with a
// fresh deadline, both for first issuance and for re-keying a mid-roll grant
// after a desired-template change.
func (s *BrokerSet) grantRoll(ctx context.Context, b *redpandav1alpha2.Broker, templateHash string, now time.Time) error {
	p := k8sclient.MergeFrom(b.DeepCopy())
	if b.Annotations == nil {
		b.Annotations = map[string]string{}
	}
	b.Annotations[feature.RollGrant.Key] = feature.FormatRollGrant(templateHash, now.Add(feature.RollGrantTTL))
	if err := s.Client.Patch(ctx, b, p); err != nil {
		return fmt.Errorf("granting roll to Broker %s: %w", b.Name, err)
	}
	return nil
}

func (s *BrokerSet) revokeRollGrant(ctx context.Context, b *redpandav1alpha2.Broker) error {
	p := k8sclient.MergeFrom(b.DeepCopy())
	delete(b.Annotations, feature.RollGrant.Key)
	if err := s.Client.Patch(ctx, b, p); err != nil {
		return fmt.Errorf("revoking roll-grant on Broker %s: %w", b.Name, err)
	}
	return nil
}
