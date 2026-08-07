// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package brokerset implements CR-agnostic management of Broker custom
// resources for a single node pool: rendering Broker CRs from a desired
// StatefulSet template, steady-state reconciliation (create/update/scale-down
// via decommission intent), cluster-wide roll-grant serialization, the
// StatefulSet→Broker migration state machine, and its rollback.
//
// Everything specific to the owning cluster CR — V1 Cluster or V2 Redpanda —
// is injected through [BrokerSet]'s fields: the owner object, the ClusterRef
// stamped into Broker specs, label sets and selectors, the config-checksum
// annotation key, and callbacks for health gating and migration progress
// reporting. The hard-won invariants (grant re-keying in place, decommission
// intent never unset, annotation-based rotation identity) live here, once.
package brokerset

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
)

const (
	// NetworkIndexLabelKey carries the Broker's network index on the Broker
	// CR and its pod template for cheap label selection. The network index
	// selects the broker's stable network identity (pod name, advertised
	// address slot) and survives pod replacement; it is deliberately not
	// called "ordinal" — Broker CR names are non-ordinal, and only the PODS
	// keep StatefulSet ordinal names.
	NetworkIndexLabelKey = "cluster.redpanda.com/network-index"

	// BrokerDecommissionFinalizer is the Broker controller's finalizer;
	// rollback removes it so CR deletion cannot trigger a decommission.
	BrokerDecommissionFinalizer = "cluster.redpanda.com/broker-decommission"
)

// Migration condition reasons, shared by every owning-CR flavor (the V1
// BrokerMigration condition uses these same strings).
const (
	MigrationReasonBlocked    = "Blocked"
	MigrationReasonInProgress = "InProgress"
	MigrationReasonComplete   = "Complete"
	MigrationReasonRolledBack = "RolledBack"
)

// MigrationReporter records STS→Broker migration progress on the owning
// cluster's status. The migration state itself is always derived from world
// state, never persisted — reporting is best-effort observability and must
// not fail the migration.
type MigrationReporter interface {
	// Report records the given condition value. Implementations should
	// de-duplicate writes when nothing changed.
	Report(ctx context.Context, status corev1.ConditionStatus, reason, message string)
	// NeedsCompletion reports whether migration progress was previously
	// recorded and not yet marked complete. Steady state promotes such a
	// record to Complete; clusters that never migrated never get one.
	NeedsCompletion(ctx context.Context) bool
}

// BrokerSet manages the Broker CRs of one node pool on behalf of an owning
// cluster resource. Construct one per pool per reconcile; it holds no state
// beyond its configuration.
type BrokerSet struct {
	Client k8sclient.Client
	Scheme *runtime.Scheme

	// Owner is the cluster resource that controller-owns every Broker CR
	// and the migration backup ConfigMap.
	Owner k8sclient.Object
	// ClusterRef is stamped into each Broker's spec.clusterRef.
	ClusterRef redpandav1alpha2.ClusterRef
	// PoolName is the node pool this set manages (backup ConfigMap key).
	PoolName string

	// BrokerLabels label every Broker CR of this pool and its pod template.
	// Callers MUST include redpandav1alpha2.ClusterNameLabel (Broker.PodName
	// relies on it when ClusterRef names a pool rather than the cluster) and
	// whatever labels PoolSelector and ClusterSelector match on.
	BrokerLabels map[string]string
	// PoolSelector selects this pool's Broker CRs.
	PoolSelector k8slabels.Selector
	// ClusterSelector selects ALL the owner's Broker CRs across pools —
	// roll serialization and decommission mutual exclusion are cluster-wide.
	ClusterSelector k8slabels.Selector
	// PodSelector selects this pool's pods (migration preconditions).
	PodSelector k8slabels.Selector

	// ConfigChecksumKey is the pod-template annotation on the rendered
	// StatefulSet carrying the node-config checksum (V1:
	// redpanda.vectorized.io/configmap-hash, V2: config.redpanda.com/checksum).
	// Its value is copied to BrokerConfigChecksumAnnotation — one of the three
	// rotation-identity keys.
	ConfigChecksumKey string

	// IsClusterHealthy gates roll-grant issuance and the migration's
	// destructive step. Return a *RequeueAfterError when unhealthy so the
	// owning reconciler backs off instead of erroring. nil means
	// always-healthy (tests only).
	IsClusterHealthy func(ctx context.Context) error
	// OnQuiesced runs when no rolls are outstanding and no grants are active
	// (V1 clears Status.Restarting here). Optional.
	OnQuiesced func(ctx context.Context) error
	// MigrationBlockedReason contributes owner-specific quiescence checks to
	// the migration preconditions (V1: cluster restarting, decommission
	// recorded in status). Return a non-empty human-readable reason to block
	// the migration this pass. Optional.
	MigrationBlockedReason func(ctx context.Context) (string, error)
	// Reporter records migration progress. Optional.
	Reporter MigrationReporter

	Logger logr.Logger
}

// report is the nil-safe Reporter.Report.
func (s *BrokerSet) report(ctx context.Context, status corev1.ConditionStatus, reason, message string) {
	if s.Reporter != nil {
		s.Reporter.Report(ctx, status, reason, message)
	}
}

// Ensure runs the full per-pool Broker lifecycle. If a live StatefulSet named
// stsKey exists the migration state machine runs; otherwise steady-state
// reconciliation. desiredSTS is the rendered desired StatefulSet — the
// canonical pod template converted into Broker CRs; it may be mutated. A nil
// desiredSTS (deleted pool, nothing rendered) skips migration and renders no
// desired Brokers, leaving only excess-broker reconciliation and roll grants.
func (s *BrokerSet) Ensure(ctx context.Context, stsKey types.NamespacedName, desiredSTS *appsv1.StatefulSet, desiredReplicas int32) error {
	l := s.Logger.WithValues("nodepool", s.PoolName)

	existingSTS, err := s.getExistingStatefulSet(ctx, stsKey)
	if err != nil {
		return errors.Wrap(err, "checking for existing StatefulSet")
	}

	if existingSTS != nil && desiredSTS != nil {
		return s.ensureMigration(ctx, l, existingSTS, desiredSTS)
	}

	return s.ensureBrokers(ctx, l, desiredSTS, desiredReplicas)
}

// ensureBrokers is the steady-state broker lifecycle (new cluster or
// post-migration).
func (s *BrokerSet) ensureBrokers(ctx context.Context, l logr.Logger, desiredSTS *appsv1.StatefulSet, desiredReplicas int32) error {
	// Migration completion is observed, not recorded: reaching steady state
	// (no StatefulSet left) IS completion. Only progress an existing
	// condition — clusters that never migrated don't get one.
	if s.Reporter != nil && s.Reporter.NeedsCompletion(ctx) {
		s.report(ctx, corev1.ConditionTrue,
			MigrationReasonComplete, "StatefulSet removed; Broker CRs manage all pods")
	}

	var desired []redpandav1alpha2.Broker
	if desiredSTS != nil {
		var err error
		desired, err = s.RenderBrokers(desiredSTS, desiredReplicas, false)
		if err != nil {
			return errors.Wrap(err, "rendering desired Broker CRs")
		}
	}

	existing, err := s.listBrokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing existing Broker CRs")
	}

	// Decommission mutual exclusion is CLUSTER-wide (see ClusterSelector):
	// one disruptive operation at a time across ALL pools, so the in-flight
	// flag must be computed over every pool's brokers — a pool-scoped view
	// would let two pools scaled down together start two concurrent
	// decommissions. DiskLost tombstones mid-decommission count like any other.
	clusterBrokers, err := s.listClusterBrokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing cluster Broker CRs")
	}
	decommissionInFlight := slices.ContainsFunc(clusterBrokers, func(b redpandav1alpha2.Broker) bool {
		return b.Spec.Decommission && b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned
	})
	// A decommission must also never START while a rotation holds an
	// unexpired roll-grant — the grantee's pod may be down mid-roll, and
	// draining a second broker would take two out at once. This is the
	// reverse of EnsureRollGrants' hold-while-decommissioning. Grants
	// stranded on DiskLost tombstones don't count: their holder is already
	// down either way and EnsureRollGrants revokes them.
	now := time.Now()
	rollGrantHeld := slices.ContainsFunc(clusterBrokers, func(b redpandav1alpha2.Broker) bool {
		if b.IsDiskLost() {
			return false
		}
		grant := b.Annotations[feature.RollGrant.Key]
		if grant == "" {
			return false
		}
		_, deadline, ok := feature.ParseRollGrant(grant)
		return ok && now.Before(deadline)
	})

	// DiskLost tombstones (dead incarnations) never enter the ordinary
	// desired/excess matching: once released, a tombstone and its replacement
	// share a network index and would collide in IndexBrokers. They are
	// driven by ReconcileDiskLostBrokers instead. Unreleased tombstones still
	// pin their index — their pod/PVC names are not free yet.
	live, tombstones := PartitionDiskLostBrokers(existing)
	existingByIndex := IndexBrokers(live)
	// The desired loop consumes existingByIndex; the tombstone lifecycle needs
	// an intact live-by-index view to find replacements.
	liveByIndex := IndexBrokers(live)

	pinned := map[int32]bool{}
	for _, t := range tombstones {
		if !t.DiskLostReleased() {
			pinned[ptr.Deref(t.Spec.NetworkIndex, 0)] = true
		}
	}

	for i := range desired {
		d := &desired[i]
		idx := *d.Spec.NetworkIndex

		existingBroker, ok := existingByIndex[idx]
		if !ok {
			if pinned[idx] {
				// A dead incarnation is still dismantling at this index;
				// creating the replacement now would collide on pod/PVC
				// names.
				continue
			}
			if err := s.createBroker(ctx, l, desiredSTS.Name, d); err != nil {
				return err
			}
			continue
		}
		delete(existingByIndex, idx)

		if err := s.EnsureDesiredBroker(ctx, l, existingBroker, d); err != nil {
			return err
		}
	}

	// Tombstone lifecycle runs BEFORE excess handling so tombstone decommissions
	// take priority deterministically; it feeds the updated in-flight state
	// onward.
	decommissionInFlight, err = s.ReconcileDiskLostBrokers(ctx, l, tombstones, liveByIndex, desiredReplicas, decommissionInFlight, rollGrantHeld)
	if err != nil {
		return err
	}

	if err := s.ReconcileExcessBrokers(ctx, l, existingByIndex, desiredReplicas, decommissionInFlight, rollGrantHeld); err != nil {
		return err
	}

	return s.EnsureRollGrants(ctx, l)
}

// PartitionDiskLostBrokers splits Broker CRs into live brokers and DiskLost
// tombstones (dead incarnations lingering as decommission records). Tombstones
// must never enter the ordinary desired/excess paths — after index release,
// a tombstone and its replacement share a network index and would collide in
// IndexBrokers. Tombstones are sorted (creationTimestamp, then name) for
// deterministic handling: an index can legitimately carry two tombstones when
// the replacement's node also dies.
func PartitionDiskLostBrokers(brokers []redpandav1alpha2.Broker) (live []redpandav1alpha2.Broker, tombstones []*redpandav1alpha2.Broker) {
	for i := range brokers {
		if brokers[i].IsDiskLost() {
			tombstones = append(tombstones, &brokers[i])
			continue
		}
		live = append(live, brokers[i])
	}
	sort.Slice(tombstones, func(i, j int) bool {
		ti, tj := tombstones[i].CreationTimestamp, tombstones[j].CreationTimestamp
		if !ti.Equal(&tj) {
			return ti.Before(&tj)
		}
		return tombstones[i].Name < tombstones[j].Name
	})
	return live, tombstones
}

// ReconcileDiskLostBrokers drives dead-incarnation tombstones to completion:
//
//  1. a tombstone that reached Decommissioned is deleted — the dedicated
//     deletion site: tombstones are excluded from both the desired matching
//     and the excess path, so nothing else would ever delete them;
//  2. a released tombstone with no recorded BrokerID is deleted outright —
//     nothing provably registered, so there is nothing to decommission
//     (resolving by pod name is forbidden: the name may already belong to
//     the replacement);
//  3. a released tombstone at a still-desired index is marked for decommission
//     only once the replacement at that index has registered — the dead id
//     first would stall on small clusters that cannot re-replicate onto the
//     survivors;
//  4. a released tombstone at an undesired index (scale-down / pool deletion)
//     is marked immediately — no replacement will come.
//
// Marks respect the one-disruptive-operation-at-a-time invariant — no
// concurrent decommission anywhere in the cluster and no unexpired
// roll-grant — and the updated in-flight state is returned for the excess
// path.
func (s *BrokerSet) ReconcileDiskLostBrokers(ctx context.Context, l logr.Logger, tombstones []*redpandav1alpha2.Broker, liveByIndex map[int32]*redpandav1alpha2.Broker, desiredReplicas int32, decommissionInFlight, rollGrantHeld bool) (bool, error) {
	for _, t := range tombstones {
		if !t.DeletionTimestamp.IsZero() {
			continue
		}
		if t.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned {
			l.Info("deleting decommissioned DiskLost tombstone", "name", t.Name)
			if err := s.Client.Delete(ctx, t); err != nil && !apierrors.IsNotFound(err) {
				return decommissionInFlight, errors.Wrapf(err, "deleting DiskLost tombstone %s", t.Name)
			}
			continue
		}
		if t.Spec.Decommission {
			// In flight; the Broker controller drives it pod-less against
			// the recorded id.
			continue
		}
		if !t.DiskLostReleased() {
			// Still dismantling; its index is pinned above.
			continue
		}
		if t.Status.BrokerID == nil {
			l.Info("deleting DiskLost tombstone without a recorded node_id; a ghost member may remain and needs manual or ghost decommissioning", "name", t.Name)
			if err := s.Client.Delete(ctx, t); err != nil && !apierrors.IsNotFound(err) {
				return decommissionInFlight, errors.Wrapf(err, "deleting DiskLost tombstone %s", t.Name)
			}
			continue
		}

		idx := ptr.Deref(t.Spec.NetworkIndex, 0)
		if idx < desiredReplicas {
			// Wait for the replacement to REGISTER, not merely exist:
			// decommissioning the dead id needs the replacement as a
			// re-replication target (a 3-node RF=3 cluster cannot drain
			// onto 2 survivors). Status.BrokerID is the durable signal.
			replacement := liveByIndex[idx]
			if replacement == nil || replacement.Status.BrokerID == nil {
				continue
			}
		}
		if decommissionInFlight || rollGrantHeld {
			// Never start a second decommission, and never start one while
			// a rotation holds the roll-grant — one disruptive operation at
			// a time, cluster-wide.
			continue
		}
		l.Info("marking DiskLost tombstone for decommission of its dead node_id", "name", t.Name, "brokerID", *t.Status.BrokerID)
		t.Spec.Decommission = true
		if err := s.Client.Update(ctx, t); err != nil {
			return decommissionInFlight, errors.Wrapf(err, "setting decommission on DiskLost tombstone %s", t.Name)
		}
		decommissionInFlight = true
	}
	return decommissionInFlight, nil
}

// EnsureDesiredBroker reconciles one desired Broker against the existing CR
// at the same index.
//
// Decommission intent — whether set by scale-down or manually by a human —
// is never unset by the operator (recommission was dropped from RFC Q2): a
// broker with the intent set is left alone until the decommission completes.
// Once terminal, the CR is deleted so the next pass creates a fresh Broker
// (and node identity) for the still-desired index. Re-creation waits for the
// old CR to be fully gone so its deletion handling cannot race the
// replacement's identically-named pod.
func (s *BrokerSet) EnsureDesiredBroker(ctx context.Context, l logr.Logger, existingBroker, d *redpandav1alpha2.Broker) error {
	if existingBroker.Spec.Decommission {
		if existingBroker.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned && existingBroker.DeletionTimestamp.IsZero() {
			l.Info("replacing decommissioned Broker at desired index", "name", existingBroker.Name, "index", *existingBroker.Spec.NetworkIndex)
			if err := s.Client.Delete(ctx, existingBroker); err != nil && !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "deleting decommissioned Broker %s", existingBroker.Name)
			}
		}
		return nil
	}

	return s.UpdateBroker(ctx, l, existingBroker, d)
}

// ReconcileExcessBrokers drives scale-down: excess brokers (index >=
// desiredReplicas, highest first) are marked for decommission strictly one at
// a time — any decommission already in flight (including a manual one on a
// desired index, in ANY pool) or an unexpired roll-grant held by a rotation
// blocks marking the next one — and deleted once they reach the
// Decommissioned phase.
func (s *BrokerSet) ReconcileExcessBrokers(ctx context.Context, l logr.Logger, existingByIndex map[int32]*redpandav1alpha2.Broker, desiredReplicas int32, decommissionInFlight, rollGrantHeld bool) error {
	var excess []*redpandav1alpha2.Broker
	for idx, b := range existingByIndex {
		if idx >= desiredReplicas {
			excess = append(excess, b)
		}
	}
	sort.Slice(excess, func(i, j int) bool {
		return ptr.Deref(excess[i].Spec.NetworkIndex, 0) > ptr.Deref(excess[j].Spec.NetworkIndex, 0)
	})

	for _, b := range excess {
		if b.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned {
			l.Info("deleting decommissioned Broker CR", "name", b.Name)
			if err := s.Client.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "deleting decommissioned Broker %s", b.Name)
			}
			continue
		}
		if decommissionInFlight || rollGrantHeld {
			// A decommission is already in flight (anywhere in the cluster)
			// or a rotation holds the roll-grant — one disruptive operation
			// at a time; wait for it to complete.
			break
		}
		l.Info("marking Broker for decommission (scale-down)", "name", b.Name, "index", *b.Spec.NetworkIndex)
		b.Spec.Decommission = true
		if err := s.Client.Update(ctx, b); err != nil {
			return errors.Wrapf(err, "setting decommission on Broker %s", b.Name)
		}
		break // one at a time
	}

	return nil
}

// RenderBrokers converts a StatefulSet spec into Broker CRs, one per ordinal.
// When migration=false (new cluster), Brokers use VolumeClaimTemplates. When
// migration=true (STS→Broker migration), Brokers use ExistingClaims to
// reference StatefulSet-created PVCs and replicas should come from the live
// STS.
func (s *BrokerSet) RenderBrokers(sts *appsv1.StatefulSet, replicas int32, migration bool) ([]redpandav1alpha2.Broker, error) {
	stsName := sts.Name

	configHash := sts.Spec.Template.Annotations[s.ConfigChecksumKey]

	vctNames := map[string]bool{}
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		vctNames[vct.Name] = true
	}

	var brokerVCTs []redpandav1alpha2.BrokerVolumeClaim
	if !migration {
		for _, vct := range sts.Spec.VolumeClaimTemplates {
			brokerVCTs = append(brokerVCTs, redpandav1alpha2.BrokerVolumeClaim{
				Name: vct.Name,
				Spec: vct.Spec,
			})
		}
	}

	var brokers []redpandav1alpha2.Broker
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)

		podSpec := *sts.Spec.Template.Spec.DeepCopy()
		podSpec.Hostname = podName
		// The StatefulSet controller would set the subdomain to the STS's
		// serviceName (the headless service) — replicate that for identical
		// pod DNS records.
		podSpec.Subdomain = sts.Spec.ServiceName
		normalizePodSpecDefaults(&podSpec)

		// Broker-mode drain and maintenance-mode handling is controller-driven
		// (BrokerReconciler.ensureDrained + the registration pass's disable);
		// the StatefulSet-era lifecycle hook scripts are redundant here, and
		// their render gate (CalculateCurrentReplicas > 1) reads volatile
		// cluster STATUS — keeping them would churn the pod-template hash
		// through bootstrap and migration and trigger spurious rotations.
		for ci := range podSpec.Containers {
			podSpec.Containers[ci].Lifecycle = nil
		}

		for vi := range podSpec.Volumes {
			v := &podSpec.Volumes[vi]
			if v.PersistentVolumeClaim != nil && vctNames[v.PersistentVolumeClaim.ClaimName] {
				v.PersistentVolumeClaim.ClaimName = fmt.Sprintf("%s-%s", v.PersistentVolumeClaim.ClaimName, podName)
			}
		}

		podAnnotations := maps.Clone(sts.Spec.Template.Annotations)
		podAnnotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = configHash

		brokerLabels := maps.Clone(s.BrokerLabels)
		brokerLabels[NetworkIndexLabelKey] = fmt.Sprintf("%d", i)

		storage := redpandav1alpha2.BrokerStorage{
			VolumeClaimTemplates: brokerVCTs,
		}
		if migration {
			var claims []redpandav1alpha2.ExistingClaim
			for _, vct := range sts.Spec.VolumeClaimTemplates {
				claims = append(claims, redpandav1alpha2.ExistingClaim{
					Name: fmt.Sprintf("%s-%s", vct.Name, podName),
				})
			}
			storage = redpandav1alpha2.BrokerStorage{
				ExistingClaims: claims,
			}
		}

		// Propagate the deletion policy so it stays readable during cluster
		// teardown, after the owning cluster object itself is gone.
		var brokerAnnotations map[string]string
		if policy, ok := s.Owner.GetAnnotations()[feature.BrokerDeletionPolicy.Key]; ok {
			brokerAnnotations = map[string]string{feature.BrokerDeletionPolicy.Key: policy}
		}

		podTemplate := redpandav1alpha2.BrokerPodTemplate{
			Labels:      brokerLabels,
			Annotations: podAnnotations,
			Spec:        podSpec,
		}
		// The rotation identity: any pod SPEC change — image, resources, env
		// — must reach live pods, not only changes that alter the rendered
		// node config. Template metadata is excluded: the Broker controller
		// syncs it onto live pods in place, without a rotation (see
		// BrokerPodTemplateHashAnnotation).
		podTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = podTemplate.Hash()

		broker := redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   s.Owner.GetNamespace(),
				Labels:      brokerLabels,
				Annotations: brokerAnnotations,
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef:   *s.ClusterRef.DeepCopy(),
				NetworkIndex: ptr.To(i),
				PodTemplate:  podTemplate,
				Storage:      storage,
			},
		}

		if err := controllerutil.SetControllerReference(s.Owner, &broker, s.Scheme); err != nil {
			return nil, errors.Wrapf(err, "setting owner reference on Broker ordinal %d", i)
		}

		brokers = append(brokers, broker)
	}

	return brokers, nil
}

// normalizePodSpecDefaults aligns the rendered pod spec with the values the
// Broker CRD's structural defaulting applies on write (container port
// protocol defaults to TCP). Without this the in-memory render never compares
// equal to the stored object — the API server re-adds the default on every
// write — and each reconcile issues a pointless no-op update.
func normalizePodSpecDefaults(spec *corev1.PodSpec) {
	defaultPorts := func(containers []corev1.Container) {
		for i := range containers {
			for j := range containers[i].Ports {
				if containers[i].Ports[j].Protocol == "" {
					containers[i].Ports[j].Protocol = corev1.ProtocolTCP
				}
			}
		}
	}
	defaultPorts(spec.InitContainers)
	defaultPorts(spec.Containers)
}

// listBrokers lists this pool's Broker CRs owned by the owner.
func (s *BrokerSet) listBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	var list redpandav1alpha2.BrokerList
	if err := s.Client.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: s.PoolSelector,
		Namespace:     s.Owner.GetNamespace(),
	}); err != nil {
		return nil, err
	}
	// Filter to only those owned by this cluster.
	var owned []redpandav1alpha2.Broker
	for _, b := range list.Items {
		if metav1.IsControlledBy(&b, s.Owner) {
			owned = append(owned, b)
		}
	}
	return owned, nil
}

// listClusterBrokers lists Broker CRs owned by this cluster across ALL node
// pools — roll serialization is cluster-wide, not per-pool.
func (s *BrokerSet) listClusterBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	var list redpandav1alpha2.BrokerList
	if err := s.Client.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: s.ClusterSelector,
		Namespace:     s.Owner.GetNamespace(),
	}); err != nil {
		return nil, err
	}
	var owned []redpandav1alpha2.Broker
	for _, b := range list.Items {
		if metav1.IsControlledBy(&b, s.Owner) {
			owned = append(owned, b)
		}
	}
	return owned, nil
}

func (s *BrokerSet) createBroker(ctx context.Context, l logr.Logger, stsName string, desired *redpandav1alpha2.Broker) error {
	desired.GenerateName = stsName + "-"

	l.Info("creating Broker CR", "generateName", desired.GenerateName, "index", *desired.Spec.NetworkIndex)
	return s.Client.Create(ctx, desired)
}

// UpdateBroker syncs the desired pod template (and propagated annotations)
// onto an existing Broker CR, skipping no-op writes.
func (s *BrokerSet) UpdateBroker(ctx context.Context, l logr.Logger, existing, desired *redpandav1alpha2.Broker) error {
	// Preserve the restart marker: it is stamped by MarkForRestart, not by
	// the renderer, and must survive PodTemplate syncs until the restart has
	// rolled through.
	restartKey := redpandav1alpha2.BrokerClusterConfigVersionAnnotation
	if v, ok := existing.Spec.PodTemplate.Annotations[restartKey]; ok {
		if desired.Spec.PodTemplate.Annotations == nil {
			desired.Spec.PodTemplate.Annotations = map[string]string{}
		}
		if _, set := desired.Spec.PodTemplate.Annotations[restartKey]; !set {
			desired.Spec.PodTemplate.Annotations[restartKey] = v
		}
	}

	policyKey := feature.BrokerDeletionPolicy.Key
	desiredPolicy, desiredPolicySet := desired.Annotations[policyKey]
	existingPolicy, existingPolicySet := existing.Annotations[policyKey]
	policyChanged := desiredPolicy != existingPolicy || desiredPolicySet != existingPolicySet

	if equality.Semantic.DeepEqual(existing.Spec.PodTemplate, desired.Spec.PodTemplate) &&
		!policyChanged {
		return nil
	}
	// Spec.Decommission is deliberately not synced: decommission intent is
	// never unset by the operator, not even when the index is desired again.
	// Terminal brokers at desired indices are replaced by EnsureDesiredBroker.
	existing.Spec.PodTemplate = desired.Spec.PodTemplate
	if desiredPolicySet {
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[policyKey] = desiredPolicy
	} else {
		delete(existing.Annotations, policyKey)
	}

	l.V(1).Info("updating Broker CR", "name", existing.Name, "index", *existing.Spec.NetworkIndex)
	return s.Client.Update(ctx, existing)
}

func (s *BrokerSet) getBrokerPod(ctx context.Context, b *redpandav1alpha2.Broker) (*corev1.Pod, error) {
	var pod corev1.Pod
	err := s.Client.Get(ctx, types.NamespacedName{Name: b.PodName(), Namespace: b.Namespace}, &pod)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "getting pod for Broker %s", b.Name)
	}
	return &pod, nil
}

// IndexBrokers indexes Broker CRs by their network index.
func IndexBrokers(brokers []redpandav1alpha2.Broker) map[int32]*redpandav1alpha2.Broker {
	m := map[int32]*redpandav1alpha2.Broker{}
	for i := range brokers {
		if brokers[i].Spec.NetworkIndex != nil {
			m[*brokers[i].Spec.NetworkIndex] = &brokers[i]
		}
	}
	return m
}

// MarkForRestart records a restart-requiring cluster-config version in each
// Broker's desired pod template (the `kubectl rollout restart` pattern): pods
// inherit the annotation at creation, so a live pod whose value differs from
// the desired template needs a rotation. The resulting restarts ride the
// roll-grant machinery one broker at a time. Only Broker SPECS are written —
// the cluster controller never touches pods, which belong to the Broker
// controller.
func MarkForRestart(ctx context.Context, c k8sclient.Client, owner k8sclient.Object, selector k8slabels.Selector, version string) error {
	var list redpandav1alpha2.BrokerList
	if err := c.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: selector,
		Namespace:     owner.GetNamespace(),
	}); err != nil {
		return errors.Wrap(err, "listing Broker CRs")
	}

	for i := range list.Items {
		b := &list.Items[i]
		if !metav1.IsControlledBy(b, owner) {
			continue
		}
		if b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation] == version {
			continue
		}
		patch := k8sclient.MergeFrom(b.DeepCopy())
		if b.Spec.PodTemplate.Annotations == nil {
			b.Spec.PodTemplate.Annotations = map[string]string{}
		}
		b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation] = version
		if err := c.Patch(ctx, b, patch); err != nil {
			return errors.Wrapf(err, "marking Broker %s for restart", b.Name)
		}
	}
	return nil
}
