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
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
)

// RollbackConfig carries the owner-specific pieces of a Broker CR rollback.
// Rollback is cluster-scoped (all pools at once), unlike [BrokerSet].
type RollbackConfig struct {
	Client k8sclient.Client
	Scheme *runtime.Scheme
	// Owner is the cluster resource whose Broker CRs are rolled back; the
	// restored StatefulSets are controller-owned by it.
	Owner k8sclient.Object
	// ClusterSelector selects ALL the owner's Broker CRs across pools.
	ClusterSelector k8slabels.Selector
	// Reporter records rollback progress. Optional.
	Reporter MigrationReporter
	Logger   logr.Logger
}

func (cfg *RollbackConfig) report(ctx context.Context, status corev1.ConditionStatus, reason, message string) {
	if cfg.Reporter != nil {
		cfg.Reporter.Report(ctx, status, reason, message)
	}
}

// VerifyRollbackPreconditions blocks rollback while a rotation or
// decommission is mid-flight, reading only the Broker CRs: no Broker may be
// mid-decommission and no Broker may hold an unexpired roll-grant (an
// actively progressing rotation always holds one). Fully Decommissioned
// brokers (scale-down leftovers) are inert and do not block. A missing pod
// deliberately does NOT block: with no unexpired grant it is an abandoned
// rotation (e.g. a wedged Broker controller) or a manual deletion, and the
// restored StatefulSet recreates the pod — whose postStart hook clears
// maintenance mode — so proceeding recovers toward a working cluster where
// waiting would deadlock on the very controller being rolled away from.
func VerifyRollbackPreconditions(l logr.Logger, brokers []redpandav1alpha2.Broker) error {
	block := func(reason string) error {
		l.Info("rollback blocked, waiting for in-flight operations to finish", "reason", reason)
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "rollback blocked: " + reason,
		}
	}

	now := time.Now()
	for i := range brokers {
		b := &brokers[i]
		if b.IsDiskLostTicket() {
			// A dead incarnation is pod-less by construction: its (possibly
			// unfinishable) dead-id decommission cannot fight the restored
			// StatefulSet over anything, and blocking the escape hatch on
			// it could wedge the rollback forever. Rollback deletes the
			// ticket raw; the leaked node_id is logged there.
			continue
		}
		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				return block(fmt.Sprintf("Broker %s is decommissioning", b.Name))
			}
			continue
		}
		if grant := b.Annotations[feature.RollGrant.Key]; grant != "" {
			if _, deadline, ok := feature.ParseRollGrant(grant); ok && now.Before(deadline) {
				return block(fmt.Sprintf("Broker %s holds an active roll-grant", b.Name))
			}
		}
	}
	return nil
}

// restoreStatefulSetsFromBackup recreates the pre-migration StatefulSets from
// the migration backup ConfigMap (RFC Q1: the backup is the one piece of
// state not derivable). Re-rendering could differ from what the pods were
// built from — e.g. after an operator upgrade mid-migration — and an STS that
// doesn't match its re-adopted pods would immediately roll them. Restoring
// the exact backup re-adopts without disruption; any genuine drift then rolls
// through the normal health-gated StatefulSet update flow.
func restoreStatefulSetsFromBackup(ctx context.Context, cfg RollbackConfig, cm *corev1.ConfigMap) error {
	for key, data := range cm.Data {
		var backup appsv1.StatefulSet
		if err := json.Unmarshal([]byte(data), &backup); err != nil {
			return errors.Wrapf(err, "unmarshaling StatefulSet backup %s/%s", cm.Name, key)
		}
		restored := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backup.Name,
				Namespace:   cfg.Owner.GetNamespace(),
				Labels:      backup.Labels,
				Annotations: backup.Annotations,
			},
			Spec: backup.Spec,
		}
		if err := controllerutil.SetControllerReference(cfg.Owner, restored, cfg.Scheme); err != nil {
			return errors.Wrapf(err, "setting owner reference on restored StatefulSet %s", restored.Name)
		}
		cfg.Logger.Info("rollback: restoring StatefulSet from migration backup", "sts", restored.Name, "key", key)
		if err := cfg.Client.Create(ctx, restored); err != nil && !apierrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "restoring StatefulSet %s", restored.Name)
		}
	}
	return nil
}

// Rollback cleans up Broker CRs when the migration annotation is removed,
// allowing the StatefulSet to re-adopt pods.
//
// Rollback is resumable at any point: a transient error aborts the pass and
// bubbles out of the owning Reconcile, which requeues; the retry re-derives
// everything from world state, and every mutation is idempotent. The backup
// ConfigMap is the resume marker for the tail of the flow — it is deleted
// only after the restored StatefulSets have re-adopted the pods, so a pass
// interrupted after the last Broker CR was deleted still finishes the
// restore-and-verify phase on the next reconcile (finalizeRollback keys on
// the ConfigMap's existence, not on Broker CRs remaining).
func Rollback(ctx context.Context, cfg RollbackConfig) error {
	c, l := cfg.Client, cfg.Logger

	var brokerList redpandav1alpha2.BrokerList
	if err := c.List(ctx, &brokerList, &k8sclient.ListOptions{
		LabelSelector: cfg.ClusterSelector,
		Namespace:     cfg.Owner.GetNamespace(),
	}); err != nil {
		return err
	}

	if len(brokerList.Items) > 0 {
		// Rollback is gated on LOCAL state only — never on admin-API health.
		// It is the escape hatch and must stay available on a degraded
		// cluster; it is blocked only while another disruptive operation is
		// mid-flight.
		if err := VerifyRollbackPreconditions(l, brokerList.Items); err != nil {
			var requeueErr *RequeueAfterError
			if errors.As(err, &requeueErr) {
				cfg.report(ctx, corev1.ConditionFalse, MigrationReasonBlocked, requeueErr.Msg)
			}
			return err
		}

		l.Info("rollback: cleaning up Broker CRs", "count", len(brokerList.Items))

		// Strip Broker CR ownerRefs from pods and PVCs so the STS can
		// re-adopt. A strategic-merge $patch:delete keyed on the Broker's
		// UID is a no-op when the object or the ref is already gone, and
		// unlike a read-modify-Update it cannot conflict with concurrent
		// writers (the Broker controller keeps reconciling until its CR is
		// deleted below).
		for i := range brokerList.Items {
			b := &brokerList.Items[i]
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: b.PodName(), Namespace: cfg.Owner.GetNamespace()}}
			if err := stripOwnerRef(ctx, c, pod, b.UID); err != nil {
				return errors.Wrapf(err, "stripping Broker ownerRef from pod %s", pod.Name)
			}
			for _, ec := range b.Spec.Storage.ExistingClaims {
				pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: ec.Name, Namespace: cfg.Owner.GetNamespace()}}
				if err := stripOwnerRef(ctx, c, pvc, b.UID); err != nil {
					return errors.Wrapf(err, "stripping Broker ownerRef from PVC %s", ec.Name)
				}
			}
		}

		// Delete Broker CRs with orphan propagation, leaving each CR's
		// finalizer alone: the Broker controller removes its own finalizer in
		// reconcileDelete — the pod's ownerRef was stripped above, so it takes
		// the no-decommission rollback branch (raw CR deletion never
		// decommissions, RFC Q2). Removing the finalizer from here instead
		// would race the Broker controller, which re-adds it on every
		// reconcile of a live CR — a conflict tug-of-war that stalled
		// rollback for minutes. Orphan propagation prevents the GC from
		// cascade-deleting pods that the Broker controller re-adopted between
		// our ownerRef strip above and the delete here.
		for i := range brokerList.Items {
			b := &brokerList.Items[i]
			if b.DeletionTimestamp.IsZero() {
				if b.IsDiskLostTicket() && b.Status.BrokerID != nil {
					// The ticket's dead-id decommission will never run now.
					// The restored StatefulSet-mode reconciler's ghost
					// decommissioner (--unsafe-decommission-failed-brokers)
					// cleans it, or an operator decommissions it manually.
					l.Info("rollback deletes a DiskLost ticket; its dead node_id remains a cluster member until ghost-decommissioned",
						"name", b.Name, "brokerID", *b.Status.BrokerID)
				}
				l.Info("rollback: deleting Broker CR", "name", b.Name)
				if err := c.Delete(ctx, b, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
					return errors.Wrapf(err, "deleting Broker CR %s", b.Name)
				}
				continue
			}
			// Already terminating on a later pass: normally the Broker
			// controller has stripped its own finalizer by now, so a lingering
			// one means the controller is degraded or disabled — exactly when
			// rollback must still work. Strip it ourselves. This cannot
			// re-enter the tug-of-war (the controller never re-adds a
			// finalizer on a terminating CR), and the merge patch avoids
			// optimistic-locking conflicts with its status writes.
			if controllerutil.ContainsFinalizer(b, BrokerDecommissionFinalizer) {
				l.Info("rollback: stripping finalizer from terminating Broker CR", "name", b.Name)
				stripped := b.DeepCopy()
				controllerutil.RemoveFinalizer(stripped, BrokerDecommissionFinalizer)
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"finalizers": stripped.Finalizers}})
				if err != nil {
					return err
				}
				if err := c.Patch(ctx, b, k8sclient.RawPatch(types.MergePatchType, patch)); err != nil && !apierrors.IsNotFound(err) {
					return errors.Wrapf(err, "stripping finalizer from Broker CR %s", b.Name)
				}
			}
		}
	}

	return finalizeRollback(ctx, cfg, len(brokerList.Items) > 0)
}

// finalizeRollback restores the pre-migration StatefulSets from the backup
// ConfigMap, waits for them to re-adopt the pods, and only then deletes the
// backup and reports success. It keys on the ConfigMap's existence so an
// interrupted pass — even one that already deleted every Broker CR — resumes
// here on the next reconcile. A leaked backup on a steady-state StatefulSet
// cluster is cleaned up by the same path (the restore no-ops on
// AlreadyExists and the pods are already adopted).
func finalizeRollback(ctx context.Context, cfg RollbackConfig, cleanedThisPass bool) error {
	c, l := cfg.Client, cfg.Logger

	cmName := migrationBackupName(cfg.Owner.GetName())
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cfg.Owner.GetNamespace()}, &cm); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if !cleanedThisPass {
			// Steady state: no Broker CRs and no pending restore.
			return nil
		}
		// No backup exists (a broker-born cluster was never migrated from a
		// StatefulSet): the owning reconciler's ordinary ensure recreates the
		// StatefulSet from the render — in the V1 cluster controller that is
		// ar.statefulSet + ar.Ensure(), landing in StatefulSetResource.Ensure
		// (obj() + CreateIfNotExists).
		l.Info("rollback: no migration backup ConfigMap, StatefulSet will be re-rendered by the owning reconciler", "name", cmName)
		cfg.report(ctx, corev1.ConditionTrue,
			MigrationReasonRolledBack, "Broker CRs removed; StatefulSet manages all pods")
		return nil
	}

	if err := restoreStatefulSetsFromBackup(ctx, cfg, &cm); err != nil {
		return err
	}

	// Report success only once the restored StatefulSets have actually
	// re-adopted the pods; until then keep the backup (the one piece of
	// non-derivable state) and requeue.
	var pods corev1.PodList
	if err := c.List(ctx, &pods, &k8sclient.ListOptions{
		LabelSelector: cfg.ClusterSelector,
		Namespace:     cfg.Owner.GetNamespace(),
	}); err != nil {
		return err
	}
	for i := range pods.Items {
		owner := metav1.GetControllerOf(&pods.Items[i])
		if owner == nil || owner.Kind != "StatefulSet" {
			msg := fmt.Sprintf("waiting for the StatefulSet to adopt pod %s", pods.Items[i].Name)
			cfg.report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, msg)
			return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "rollback: " + msg}
		}
	}

	l.Info("rollback: deleting migration backup ConfigMap", "name", cmName)
	if err := c.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	cfg.report(ctx, corev1.ConditionTrue,
		MigrationReasonRolledBack, "Broker CRs removed; StatefulSet manages all pods")
	return nil
}

// stripOwnerRef removes the ownerReference with the given UID from obj via a
// strategic-merge patch ($patch: delete keyed on uid, the list's merge key).
// Missing objects and already-absent refs are no-ops.
func stripOwnerRef(ctx context.Context, c k8sclient.Client, obj k8sclient.Object, uid types.UID) error {
	patch := []byte(fmt.Sprintf(`{"metadata":{"ownerReferences":[{"$patch":"delete","uid":"%s"}]}}`, uid))
	if err := c.Patch(ctx, obj, k8sclient.RawPatch(types.StrategicMergePatchType, patch)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
