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
	stderrors "errors"
	"fmt"
	"time"

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
// decommission is mid-flight: every non-decommissioned Broker must have its
// pod present and no Broker may hold an unexpired roll-grant. Fully
// Decommissioned brokers (scale-down leftovers) are inert and do not block.
func VerifyRollbackPreconditions(ctx context.Context, c k8sclient.Client, l logr.Logger, brokers []redpandav1alpha2.Broker) error {
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
		var pod corev1.Pod
		if err := c.Get(ctx, types.NamespacedName{Name: b.PodName(), Namespace: b.Namespace}, &pod); err != nil {
			if apierrors.IsNotFound(err) {
				return block(fmt.Sprintf("pod %s is missing (rotation in flight)", b.PodName()))
			}
			return err
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
//
// Returns false when there is no backup to restore from — the cluster
// controller then falls back to re-rendering the StatefulSet.
func restoreStatefulSetsFromBackup(ctx context.Context, cfg RollbackConfig) (bool, error) {
	cmName := migrationBackupName(cfg.Owner.GetName())
	var cm corev1.ConfigMap
	if err := cfg.Client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cfg.Owner.GetNamespace()}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			cfg.Logger.Info("rollback: no migration backup ConfigMap, StatefulSets will be re-rendered", "name", cmName)
			return false, nil
		}
		return false, err
	}

	for key, data := range cm.Data {
		var backup appsv1.StatefulSet
		if err := json.Unmarshal([]byte(data), &backup); err != nil {
			return false, fmt.Errorf("unmarshaling StatefulSet backup %s/%s: %w", cmName, key, err)
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
			return false, fmt.Errorf("setting owner reference on restored StatefulSet %s: %w", restored.Name, err)
		}
		cfg.Logger.Info("rollback: restoring StatefulSet from migration backup", "sts", restored.Name, "key", key)
		if err := cfg.Client.Create(ctx, restored); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, fmt.Errorf("restoring StatefulSet %s: %w", restored.Name, err)
		}
	}
	return true, nil
}

// Rollback cleans up Broker CRs when the migration annotation is removed,
// allowing the StatefulSet to re-adopt pods.
//
// Rollback is resumable at any point: a transient error (e.g. an update
// conflict against an object the Broker controller is also writing) aborts
// the pass and bubbles out of the owning Reconcile, which requeues; the
// retry re-derives everything from world state, and every mutation is
// freshly-read and idempotent. The one asymmetric window: a failure after
// the last Broker CR is deleted but before the backup restore runs — the
// retry then finds no Brokers and returns nil, and the owning reconciler's
// ordinary StatefulSet ensure recreates the STS from the RENDER instead of
// the backup (the same fallback as a broker-born cluster with no backup).
// The leftover backup ConfigMap is refreshed by the next migration
// (ensureBackupConfigMap updates in place), so it cannot serve a stale
// restore.
func Rollback(ctx context.Context, cfg RollbackConfig) error {
	c, l := cfg.Client, cfg.Logger

	var brokerList redpandav1alpha2.BrokerList
	if err := c.List(ctx, &brokerList, &k8sclient.ListOptions{
		LabelSelector: cfg.ClusterSelector,
		Namespace:     cfg.Owner.GetNamespace(),
	}); err != nil {
		return err
	}

	if len(brokerList.Items) == 0 {
		return nil
	}

	// Rollback is gated on LOCAL state only — never on admin-API health.
	// It is the escape hatch and must stay available on a degraded cluster;
	// it is blocked only while another disruptive operation is mid-flight.
	if err := VerifyRollbackPreconditions(ctx, c, l, brokerList.Items); err != nil {
		var requeueErr *RequeueAfterError
		if stderrors.As(err, &requeueErr) {
			cfg.report(ctx, corev1.ConditionFalse, MigrationReasonBlocked, requeueErr.Msg)
		}
		return err
	}

	l.Info("rollback: cleaning up Broker CRs", "count", len(brokerList.Items))

	// Strip Broker CR ownerRefs from pods so the STS can re-adopt.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		podName := b.PodName()
		var pod corev1.Pod
		if err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cfg.Owner.GetNamespace()}, &pod); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if removeOwnerRef(&pod, b.UID) {
			l.Info("rollback: stripping Broker ownerRef from pod", "pod", podName)
			if err := c.Update(ctx, &pod); err != nil {
				return fmt.Errorf("stripping Broker ownerRef from pod %s: %w", podName, err)
			}
		}
	}

	// Strip Broker CR ownerRefs from PVCs.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		for _, ec := range b.Spec.Storage.ExistingClaims {
			var pvc corev1.PersistentVolumeClaim
			if err := c.Get(ctx, types.NamespacedName{Name: ec.Name, Namespace: cfg.Owner.GetNamespace()}, &pvc); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return err
			}
			if removeOwnerRef(&pvc, b.UID) {
				l.Info("rollback: stripping Broker ownerRef from PVC", "pvc", ec.Name)
				if err := c.Update(ctx, &pvc); err != nil {
					return fmt.Errorf("stripping Broker ownerRef from PVC %s: %w", ec.Name, err)
				}
			}
		}
	}

	// Remove finalizers and delete Broker CRs with orphan propagation.
	// Finalizer removal prevents the Broker controller's reconcileDelete from
	// running decommission. Orphan propagation prevents the GC from cascade-
	// deleting pods that the Broker controller re-adopted between our ownerRef
	// strip above and the delete here.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		if controllerutil.RemoveFinalizer(b, BrokerDecommissionFinalizer) {
			if err := c.Update(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("removing finalizer from Broker CR %s: %w", b.Name, err)
			}
		}
		l.Info("rollback: deleting Broker CR", "name", b.Name)
		if err := c.Delete(ctx, b, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting Broker CR %s: %w", b.Name, err)
		}
	}

	// Recreate the StatefulSets from the migration backup, then drop the
	// backup — only after a successful restore, so a failed rollback pass
	// never loses the one non-derivable piece of state.
	restored, err := restoreStatefulSetsFromBackup(ctx, cfg)
	if err != nil {
		return err
	}
	if restored {
		cmName := migrationBackupName(cfg.Owner.GetName())
		var cm corev1.ConfigMap
		if err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cfg.Owner.GetNamespace()}, &cm); err == nil {
			l.Info("rollback: deleting migration backup ConfigMap", "name", cmName)
			if err := c.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	cfg.report(ctx, corev1.ConditionTrue,
		MigrationReasonRolledBack, "Broker CRs removed; StatefulSet manages all pods")

	return nil
}

func removeOwnerRef(obj k8sclient.Object, uid types.UID) bool {
	refs := obj.GetOwnerReferences()
	for i, ref := range refs {
		if ref.UID == uid {
			obj.SetOwnerReferences(append(refs[:i], refs[i+1:]...))
			return true
		}
	}
	return false
}
