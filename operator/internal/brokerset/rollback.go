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

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
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
	// DesiredStatefulSets renders the owner's current desired StatefulSets.
	// Optional. A cluster born in broker mode has no migration backup
	// ConfigMap; with this hook set, Rollback synthesizes one from the
	// render so adopted pods get revision bookkeeping — unlabeled pods are
	// skipped by the revision-based roll planners forever. V1 leaves it nil:
	// its roll signal is the per-pod config checksum, not ControllerRevisions.
	DesiredStatefulSets func(ctx context.Context) ([]*appsv1.StatefulSet, error)
	Logger              logr.Logger
}

func (cfg *RollbackConfig) listOwnedBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	var brokers []redpandav1alpha2.Broker
	var brokerList redpandav1alpha2.BrokerList
	if err := cfg.Client.List(ctx, &brokerList, &k8sclient.ListOptions{
		LabelSelector: cfg.ClusterSelector,
		Namespace:     cfg.Owner.GetNamespace(),
	}); err != nil {
		return nil, err
	}

	for i := range brokerList.Items {
		// this check is needed because Brokers can be owned either by v1 resource (Cluster)
		// or v2 (Redpanda). We need to make sure we're only rolling back Brokers that are owned by cfg.Owner
		if metav1.IsControlledBy(&brokerList.Items[i], cfg.Owner) {
			brokers = append(brokers, brokerList.Items[i])
		}
	}
	return brokers, nil
}

func (cfg *RollbackConfig) report(ctx context.Context, status corev1.ConditionStatus, reason, message string) {
	if cfg.Reporter != nil {
		cfg.Reporter.Report(ctx, status, reason, message)
	}
}

func VerifyRollbackPreconditions(l logr.Logger, brokers []redpandav1alpha2.Broker) error {
	block := func(reason string) error {
		l.Info("rollback blocked, waiting for in-flight operations to finish", "reason", reason)
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "rollback blocked: " + reason,
		}
	}

	for i := range brokers {
		b := &brokers[i]
		if b.IsDiskLost() {
			// A dead incarnation is pod-less by construction: its (possibly
			// unfinishable) dead-id decommission cannot fight the restored
			// StatefulSet over anything, and blocking the escape hatch on
			// it could wedge the rollback forever. Rollback deletes the
			// tombstone raw; the leaked node_id is logged there.
			continue
		}
		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				return block(fmt.Sprintf("Broker %s is decommissioning", b.Name))
			}
			continue
		}
		if b.HasUnexpiredRollGrant() {
			return block(fmt.Sprintf("Broker %s holds an active roll-grant", b.Name))
		}
	}
	return nil
}

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
// allowing the StatefulSet to re-adopt pods. It returns true when it acted.
func Rollback(ctx context.Context, cfg RollbackConfig) (bool, error) {
	c, l := cfg.Client, cfg.Logger

	brokers, err := cfg.listOwnedBrokers(ctx)
	if err != nil {
		return false, err
	}

	acted := len(brokers) > 0
	if acted {
		// Rollback is gated on LOCAL state only — never on admin-API health.
		// It is the escape hatch and must stay available on a degraded
		// cluster; it is blocked only while another disruptive operation is
		// mid-flight.
		if err := VerifyRollbackPreconditions(l, brokers); err != nil {
			var requeueErr *RequeueAfterError
			if errors.As(err, &requeueErr) {
				cfg.report(ctx, corev1.ConditionFalse, MigrationReasonBlocked, requeueErr.Msg)
			}
			return acted, err
		}

		// Synthesized before anything destructive: the ConfigMap is the
		// resume marker for everything after the CR deletions.
		if err := synthesizeBackupFromRender(ctx, cfg); err != nil {
			return acted, errors.Wrap(err, "synthesizing migration backup from the desired render")
		}

		l.Info("rollback: cleaning up Broker CRs", "count", len(brokers))

		// Strip Broker CR ownerRefs from pods so the STS can re-adopt.
		// A strategic-merge $patch:delete keyed on the Broker's
		// UID is a no-op when the object or the ref is already gone, and
		// unlike a read-modify-Update it cannot conflict with concurrent
		// writers (the Broker controller keeps reconciling until its CR is
		// deleted below).
		for i := range brokers {
			b := &brokers[i]
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: b.PodName(), Namespace: cfg.Owner.GetNamespace()}}
			if err := stripOwnerRef(ctx, c, pod, b.UID); err != nil {
				return acted, errors.Wrapf(err, "stripping Broker ownerRef from pod %s", pod.Name)
			}
		}

		for i := range brokers {
			b := &brokers[i]
			if b.DeletionTimestamp.IsZero() {
				if b.IsDiskLost() && b.Status.BrokerID != nil {
					// The tombstone's dead-id decommission will never run now.
					// The restored StatefulSet-mode reconciler's ghost
					// decommissioner (--unsafe-decommission-failed-brokers)
					// cleans it, or an operator decommissions it manually.
					l.Info("rollback deletes a DiskLost tombstone; its dead node_id remains a cluster member until ghost-decommissioned",
						"name", b.Name, "brokerID", *b.Status.BrokerID)
				}
				l.Info("rollback: deleting Broker CR", "name", b.Name)
				if err := c.Delete(ctx, b, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
					return acted, errors.Wrapf(err, "deleting Broker CR %s", b.Name)
				}
				continue
			}

			if controllerutil.ContainsFinalizer(b, BrokerDecommissionFinalizer) {
				l.Info("rollback: stripping finalizer from terminating Broker CR", "name", b.Name)
				stripped := b.DeepCopy()
				controllerutil.RemoveFinalizer(stripped, BrokerDecommissionFinalizer)
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"finalizers": stripped.Finalizers}})
				if err != nil {
					return acted, err
				}
				if err := c.Patch(ctx, b, k8sclient.RawPatch(types.MergePatchType, patch)); err != nil && !apierrors.IsNotFound(err) {
					return acted, errors.Wrapf(err, "stripping finalizer from Broker CR %s", b.Name)
				}
			}
		}
	}

	return acted, finalizeRollback(ctx, cfg, acted)
}

// finalizeRollback restores the pre-migration StatefulSets from the backup
// ConfigMap, waits for them to re-adopt the pods, and only then deletes the
// backup and reports success.
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
			if cfg.Reporter != nil && cfg.Reporter.ShouldReportRolledBack(ctx) {
				cfg.report(ctx, corev1.ConditionTrue,
					MigrationReasonRolledBack, "Broker CRs removed; StatefulSet manages all pods")
			}
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

	// Make sure we act only on the pods that belong to THIS rollback:
	// gated on the ordinal names of the backup's StatefulSets.
	expected := map[string]bool{}
	for _, data := range cm.Data {
		var backup appsv1.StatefulSet
		if err := json.Unmarshal([]byte(data), &backup); err != nil {
			continue // restoreStatefulSetsFromBackup already reported this
		}
		for i := int32(0); i < ptr.Deref(backup.Spec.Replicas, 1); i++ {
			expected[fmt.Sprintf("%s-%d", backup.Name, i)] = true
		}
	}

	revisions := map[string]string{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !expected[pod.Name] {
			continue
		}

		if owner := metav1.GetControllerOf(pod); owner != nil && owner.Kind == redpandav1alpha2.BrokerKind {
			l.Info("rollback: re-stripping Broker ownerRef from re-adopted pod", "pod", pod.Name)
			if err := stripOwnerRef(ctx, c, pod, owner.UID); err != nil {
				return errors.Wrapf(err, "re-stripping Broker ownerRef from pod %s", pod.Name)
			}
		}
		owner := metav1.GetControllerOf(pod)
		if owner == nil || owner.Kind != "StatefulSet" {
			msg := fmt.Sprintf("waiting for the StatefulSet to adopt pod %s", pod.Name)
			cfg.report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, msg)
			return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "rollback: " + msg}
		}
		// Pods the BROKER controller created (rotations, decommission
		// replacements) carry no controller-revision-hash — only the
		// StatefulSet controller stamps it. We need to stamp them here,
		// so in case of rollback, there's no pod rotation triggered by cluster (or Redpanda) controller.
		if pod.Labels[appsv1.StatefulSetRevisionLabel] == "" {
			rev, ok := revisions[owner.Name]
			if !ok {
				var sts appsv1.StatefulSet
				if err := c.Get(ctx, types.NamespacedName{Name: owner.Name, Namespace: cfg.Owner.GetNamespace()}, &sts); err != nil {
					return errors.Wrapf(err, "fetching restored StatefulSet %s for revision stamping", owner.Name)
				}
				rev = sts.Status.UpdateRevision
				revisions[owner.Name] = rev
			}
			if rev == "" {
				msg := fmt.Sprintf("waiting for the restored StatefulSet %s to publish its revision", owner.Name)
				cfg.report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, msg)
				return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "rollback: " + msg}
			}
			podPatch := k8sclient.MergeFrom(pod.DeepCopy())
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1.StatefulSetRevisionLabel] = rev
			l.Info("rollback: stamping restored StatefulSet revision onto Broker-created pod", "pod", pod.Name, "revision", rev)
			if err := c.Patch(ctx, pod, podPatch); err != nil {
				return errors.Wrapf(err, "stamping revision label on pod %s", pod.Name)
			}
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

// synthesizeBackupFromRender creates the migration backup ConfigMap from the
// owner's current desired render when none exists — the broker-born case. A
// migrated cluster restores its own backup, never the render.
func synthesizeBackupFromRender(ctx context.Context, cfg RollbackConfig) error {
	if cfg.DesiredStatefulSets == nil {
		return nil
	}
	c, l := cfg.Client, cfg.Logger

	cmName := migrationBackupName(cfg.Owner.GetName())
	var existing corev1.ConfigMap
	err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cfg.Owner.GetNamespace()}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	sets, err := cfg.DesiredStatefulSets(ctx)
	if err != nil {
		return errors.Wrap(err, "rendering desired StatefulSets")
	}
	if len(sets) == 0 {
		// Nothing rendered (e.g. a cluster being torn down).
		return nil
	}

	data := map[string]string{}
	for _, sts := range sets {
		payload, err := backupStatefulSetPayload(sts)
		if err != nil {
			return errors.Wrapf(err, "marshaling backup for StatefulSet %s", sts.Name)
		}
		data[fmt.Sprintf("%s.json", sts.Name)] = string(payload)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cfg.Owner.GetNamespace(),
			Labels:    map[string]string{"redpanda.com/migration": "statefulset-to-broker"},
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(cfg.Owner, cm, cfg.Scheme); err != nil {
		return errors.Wrap(err, "setting owner reference on synthesized backup")
	}
	l.Info("rollback: synthesizing migration backup from the current render (broker-born cluster)", "name", cmName, "statefulsets", len(sets))
	if err := c.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
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
