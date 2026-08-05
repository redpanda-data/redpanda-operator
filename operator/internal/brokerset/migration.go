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
	"k8s.io/apimachinery/pkg/types"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// migrationBackupName returns the name of the ConfigMap holding the
// pre-migration StatefulSet specs of the given owning cluster.
func migrationBackupName(ownerName string) string {
	return fmt.Sprintf("%s-migration-backup", ownerName)
}

// ensureMigration runs the StatefulSet→Broker CR migration state machine.
//
// State 0→1: Create shadow Broker CRs + back up STS spec to ConfigMap.
// State 1→2: Verify PVC retention, orphan-delete STS.
//
// The Broker controller handles pod adoption once pods are orphaned.
//
// Every pass first re-verifies the migration preconditions: the migration
// must never start (or progress to the destructive step) while a rollout is
// pending or the cluster is anything but healthy and stable.
func (s *BrokerSet) ensureMigration(ctx context.Context, l logr.Logger, sts, desired *appsv1.StatefulSet) error {
	l.Info("migration: StatefulSet exists, running state machine", "sts", sts.Name)

	stsReplicas := int(ptr.Deref(sts.Spec.Replicas, 1))

	// Shadow Brokers are rendered from the desired STS, not the live one.
	// This ensures the config checksum matches what ensureBrokers will
	// compute post-migration, preventing spurious pod rotation during
	// adoption.
	desired.Spec.Replicas = ptr.To(int32(stsReplicas))

	if err := s.VerifyMigrationPreconditions(ctx, l, sts, desired); err != nil {
		return err
	}

	shadowBrokers, err := s.RenderBrokers(desired, int32(stsReplicas), true)
	if err != nil {
		return errors.Wrap(err, "rendering shadow Broker CRs")
	}

	existing, err := s.listBrokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing Broker CRs")
	}
	existingByIndex := IndexBrokers(existing)

	// Prune shadows above the live replica count: an STS scale-down between
	// shadow creation and handover removed those ordinals' brokers through
	// the StatefulSet machinery, so their shadows are stale. They must not
	// survive into steady state, where they would read as excess brokers to
	// decommission — a decommission that can never complete, since the
	// Redpanda node behind them was already removed by the scale-down. Raw
	// CR deletion never decommissions (RFC Q2), so deleting an inert shadow
	// is safe.
	for idx, b := range existingByIndex {
		if int(idx) >= stsReplicas {
			l.Info("migration: pruning stale shadow Broker above live replica count", "name", b.Name, "index", idx, "replicas", stsReplicas)
			if err := s.Client.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "pruning stale shadow Broker %s", b.Name)
			}
			delete(existingByIndex, idx)
		}
	}

	// State 0→1: create shadow Broker CRs for missing ordinals.
	for i := range shadowBrokers {
		d := &shadowBrokers[i]
		idx := *d.Spec.NetworkIndex
		if _, ok := existingByIndex[idx]; !ok {
			d.GenerateName = desired.Name + "-"
			l.Info("migration: creating shadow Broker CR", "generateName", d.GenerateName, "index", idx)
			if err := s.Client.Create(ctx, d); err != nil {
				return errors.Wrapf(err, "creating shadow Broker ordinal %d", idx)
			}
		}
	}

	if err := s.ensureBackupConfigMap(ctx, l, sts); err != nil {
		return errors.Wrap(err, "backing up StatefulSet")
	}

	// Re-list to confirm every needed shadow exists. The check is
	// index-based, not count-based: shadows pruned above may still be listed
	// while their finalizer runs, and counting them would let the destructive
	// step proceed with a needed ordinal missing.
	existing, err = s.listBrokers(ctx)
	if err != nil {
		return err
	}
	byIndex := IndexBrokers(existing)
	for i := int32(0); i < int32(stsReplicas); i++ {
		if b, ok := byIndex[i]; !ok || !b.DeletionTimestamp.IsZero() {
			l.Info("migration: waiting for all shadow Broker CRs", "missingIndex", i, "want", stsReplicas)
			s.report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, "creating shadow Broker CRs")
			return nil
		}
	}

	// State 1→2: verify retention, orphan-delete STS.
	// We rely solely on orphan propagation to strip the STS ownerRef from
	// pods and PVCs. Manually stripping ownerRefs before deletion creates a
	// race where the STS controller re-adopts pods in the gap, which can
	// lead to pod deletion.
	if err := verifyPVCRetention(sts); err != nil {
		return errors.Wrap(err, "migration precondition failed")
	}

	l.Info("migration: orphan-deleting StatefulSet", "name", sts.Name)
	if err := s.Client.Delete(ctx, sts, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrapf(err, "orphan-deleting StatefulSet %s", sts.Name)
	}

	l.Info("migration: StatefulSet deleted, GC will strip ownerRefs, Broker controller will adopt pods")
	s.report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, "StatefulSet deleted; Broker controller adopting pods")
	return nil
}

// VerifyMigrationPreconditions blocks the migration state machine until the
// cluster is quiescent: the live StatefulSet has fully rolled out, every pod
// is ready and running the DESIRED configuration, no restart or decommission
// is in flight (owner-specific, via MigrationBlockedReason), and the cluster
// reports healthy via the admin API.
//
// The desired-config check is what makes adoption safe: shadow Brokers are
// rendered from the desired spec and adoption marks pods as current, so a
// config rollout pending at migration time would otherwise be silently
// cancelled.
func (s *BrokerSet) VerifyMigrationPreconditions(ctx context.Context, l logr.Logger, liveSTS, desiredSTS *appsv1.StatefulSet) error {
	block := func(reason string) error {
		l.Info("migration blocked, waiting for the cluster to become stable", "reason", reason)
		s.report(ctx, corev1.ConditionFalse, MigrationReasonBlocked, reason)
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "migration blocked: " + reason,
		}
	}

	if s.MigrationBlockedReason != nil {
		reason, err := s.MigrationBlockedReason(ctx)
		if err != nil {
			return err
		}
		if reason != "" {
			return block(reason)
		}
	}

	// Revision-based signals (UpdatedReplicas, CurrentRevision vs
	// UpdateRevision) are intentionally not consulted here. The operator's
	// StatefulSets use the OnDelete update strategy, and after a rollback the
	// StatefulSet adopts Broker-controller-created pods that carry no
	// controller-revision-hash label — under OnDelete kube never re-labels or
	// rolls them, so those counters never converge and would block
	// re-migration forever. The per-pod readiness and desired-config checks
	// below are the actual rollout-completeness signal.
	specReplicas := ptr.Deref(liveSTS.Spec.Replicas, 1)
	st := liveSTS.Status
	if st.ObservedGeneration != liveSTS.Generation ||
		st.Replicas != specReplicas ||
		st.ReadyReplicas != specReplicas {
		return block("StatefulSet rollout has not completed")
	}

	desiredHash := desiredSTS.Spec.Template.Annotations[s.ConfigChecksumKey]
	if liveSTS.Spec.Template.Annotations[s.ConfigChecksumKey] != desiredHash {
		return block("config change pending, StatefulSet not yet updated")
	}

	var pods corev1.PodList
	if err := s.Client.List(ctx, &pods, &k8sclient.ListOptions{
		Namespace:     s.Owner.GetNamespace(),
		LabelSelector: s.PodSelector,
	}); err != nil {
		return errors.Wrap(err, "listing pods for migration preconditions")
	}
	if int32(len(pods.Items)) != specReplicas {
		return block(fmt.Sprintf("expected %d pods, found %d", specReplicas, len(pods.Items)))
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !utils.IsPodReady(pod) {
			return block(fmt.Sprintf("pod %s is not ready", pod.Name))
		}
		if pod.Annotations[s.ConfigChecksumKey] != desiredHash {
			return block(fmt.Sprintf("pod %s is not running the desired configuration", pod.Name))
		}
	}

	// Finally the cluster itself must be healthy; IsClusterHealthy returns a
	// RequeueAfterError when it is not.
	if s.IsClusterHealthy != nil {
		return s.IsClusterHealthy(ctx)
	}
	return nil
}

func (s *BrokerSet) getExistingStatefulSet(ctx context.Context, stsKey types.NamespacedName) (*appsv1.StatefulSet, error) {
	var sts appsv1.StatefulSet
	err := s.Client.Get(ctx, stsKey, &sts)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sts, nil
}

func (s *BrokerSet) ensureBackupConfigMap(ctx context.Context, l logr.Logger, sts *appsv1.StatefulSet) error {
	cmName := migrationBackupName(s.Owner.GetName())
	poolKey := fmt.Sprintf("%s.json", s.PoolName)

	// Store only what restoreStatefulSetsFromBackup consumes. Marshaling the
	// live object verbatim would embed resourceVersion/status churn and make
	// the staleness comparison below dirty on every pass.
	backup := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        sts.Name,
			Labels:      sts.Labels,
			Annotations: sts.Annotations,
		},
		Spec: sts.Spec,
	}
	data, err := json.Marshal(backup)
	if err != nil {
		return err
	}

	// Server-side apply of this pool's key only, with a per-pool field
	// manager: pools co-own the shared ConfigMap without read-modify-write
	// races, a stale entry (the live StatefulSet keeps converging until the
	// handover; a leaked backup from an aborted rollback may predate a whole
	// earlier migration) is refreshed in place — rollback restores the backup
	// verbatim, so restoring anything but the CURRENT StatefulSet would roll
	// the re-adopted pods — and an unchanged payload is a no-op.
	gvk, err := apiutil.GVKForObject(s.Owner, s.Scheme)
	if err != nil {
		return errors.Wrap(err, "resolving owner GVK")
	}
	cm := applycorev1.ConfigMap(cmName, s.Owner.GetNamespace()).
		WithLabels(map[string]string{
			"redpanda.com/migration": "statefulset-to-broker",
		}).
		WithData(map[string]string{
			poolKey: string(data),
		}).
		WithOwnerReferences(applymetav1.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(s.Owner.GetName()).
			WithUID(s.Owner.GetUID()).
			WithController(true).
			WithBlockOwnerDeletion(true))
	l.Info("migration: applying STS backup ConfigMap", "name", cmName, "pool", s.PoolName)
	return s.Client.Apply(ctx, cm,
		k8sclient.ForceOwnership, k8sclient.FieldOwner("brokerset-migration-"+s.PoolName))
}

func verifyPVCRetention(sts *appsv1.StatefulSet) error {
	p := sts.Spec.PersistentVolumeClaimRetentionPolicy
	if p == nil {
		return nil // default is Retain/Retain
	}
	if p.WhenDeleted == appsv1.DeletePersistentVolumeClaimRetentionPolicyType ||
		p.WhenScaled == appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
		return errors.Newf("StatefulSet %s has PVC retention policy with Delete; refusing migration to avoid data loss", sts.Name)
	}
	return nil
}
