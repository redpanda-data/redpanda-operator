// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"fmt"
	"time"

	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// reconcileChangedIdentity consumes the IdentityChanged signal (K8S-977): a
// same-node disk wipe keeps the Node object (no PV-affinity disk-loss proof)
// and the repaired pod re-registers under a NEW node_id, leaving
// Status.BrokerID's old id in the controller Raft group as a dead ghost
// nobody would decommission.
//
// The remediation reuses the disk-loss machinery wholesale: convert this CR
// into a RELEASED DiskLost tombstone and hand its pod over. The old id stays
// pinned in Status.BrokerID; the pool machinery creates a replacement at the
// freed index, the replacement adopts the released pod and the new identity,
// and ReconcileDiskLostBrokers decommissions the pinned id once the
// replacement has registered — behind executeDecommission's liveness guard,
// which refuses to decommission an id that is provably alive (K8S-976).
// ResourcesReleased is latched immediately because the handover IS the
// release: unlike the dead-node flavor, the pod and PVCs must survive for
// the replacement, so the dismantle path never runs.
//
// A wrong conversion costs an unnecessary broker replacement, not data: the
// destructive call stays gated downstream. Conversion still requires, in
// order: the pod's current registration is active and alive (a dead match
// may be a crash-looping pod, not a settled takeover); an owning cluster
// whose pool machinery will actually create the replacement; and the exact
// conflict having held for MarkDiskLostAfter, clocked by the
// BrokerRegistered condition — any flap rewrites the message, which bumps
// LastTransitionTime and restarts the window. Anything short of that parks
// the CR in Stuck as before.
func (r *BrokerReconciler) reconcileChangedIdentity(ctx context.Context, state *brokerReconciliationState, k8sCluster cluster.Cluster, resolved *rpadmin.Broker) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	broker := state.broker
	oldID := *broker.Status.BrokerID
	newID := int32(resolved.NodeID)
	conflict := fmt.Sprintf("broker re-registered with node_id %d, expected %d", newID, oldID)

	park := func(reason string) (ctrl.Result, error) {
		state.registrationConflict = conflict
		state.phase = redpandav1alpha2.BrokerPhaseStuck
		l.Error(fmt.Errorf("node_id changed from %d to %d", oldID, newID),
			"broker identity changed; not converting to a tombstone", "reason", reason)
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

	if !brokerActiveAndAlive(resolved) {
		return park("the new identity is not active and alive")
	}
	if _, found, err := getBrokerOwner(ctx, k8sCluster.GetClient(), broker); err != nil || !found {
		return park("no owning cluster to create a replacement")
	}
	if state.pod == nil || !metav1.IsControlledBy(state.pod, broker) {
		return park("the pod is not controlled by this Broker")
	}
	if held := identityConflictHeldFor(broker, conflict); held < r.MarkDiskLostAfter {
		remaining := r.MarkDiskLostAfter - held
		state.registrationConflict = conflict
		state.phase = redpandav1alpha2.BrokerPhaseStuck
		l.Info("broker identity changed; holding conversion until the conflict has held",
			"oldID", oldID, "newID", newID, "remaining", remaining.Round(time.Second))
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Release the pod before latching the tombstone: a released tombstone's
	// reconcile stops before the pod logic, so only this ordering converges
	// if the status write fails — the orphan is re-adopted next pass and the
	// conversion retries.
	if err := stripPodOwnerRef(ctx, k8sCluster.GetClient(), broker, state.pod.Name); err != nil {
		return ctrl.Result{}, err
	}
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
	state.phase = redpandav1alpha2.BrokerPhaseDiskLost
	l.Info("broker identity superseded; converted to a released DiskLost tombstone",
		"oldID", oldID, "newID", newID, "pod", state.pod.Name)
	return ctrl.Result{RequeueAfter: requeueShort}, nil
}

// identityConflictHeldFor reports how long the BrokerRegistered condition
// has carried exactly this conflict, i.e. how long the supersession has been
// continuously observed. Any other content counts as zero: either the first
// observation (the condition is written at the end of this pass) or a flap
// that rewrote the message and, with it, LastTransitionTime.
func identityConflictHeldFor(broker *redpandav1alpha2.Broker, conflict string) time.Duration {
	cond := apimeta.FindStatusCondition(broker.Status.Conditions, statuses.BrokerBrokerRegistered)
	if cond == nil ||
		cond.Reason != string(statuses.BrokerBrokerRegisteredReasonIdentityChanged) ||
		cond.Message != conflict {
		return 0
	}
	return time.Since(cond.LastTransitionTime.Time)
}
