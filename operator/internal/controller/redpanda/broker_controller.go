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
	"net"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/finalizers,verbs=update
// +kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters,verbs=get
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list

const brokerFinalizerName = "cluster.redpanda.com/broker-decommission"

// defaultUnbindPVCsAfter is the Broker controller's disk-loss detection
// timeout, used when --unbind-pvcs-after is not set. Detection must not
// silently stay disabled just because the (PVCUnbinder-oriented) flag was
// left at its zero default: marking a Broker DiskLost requires a pod stuck
// Pending on PV node affinity for at least this long AND the dead-node
// proof (the pinned Node OBJECT deleted from the API) — the timeout exists
// so a rebooting node has time to come back before the terminal marking.
// TODO: give the Broker controller its own flag (e.g.
// --broker-unbind-pvcs-after) when the feature grows its GA config surface.
const defaultUnbindPVCsAfter = 5 * time.Minute

const requeueShort = 2 * time.Second

// requeueDrain paces the leadership-drain poll during a granted rotation.
// Deliberately much shorter than periodicRequeue: rolls are serialized
// cluster-wide by the roll-grant, so drain-wait latency accumulates across
// every broker in the fleet.
const requeueDrain = 10 * time.Second

// requeueDecommission paces the completion poll of an in-flight
// decommission. Completion happens on the Redpanda side, so no watch event
// fires for it — polling is the only signal — and decommissions are
// serialized one broker at a time, so this latency accumulates across every
// broker of a scale-down or pool drain. The status check is a single cheap
// admin GET; polling at periodicRequeue would put a multi-minute floor under
// each drained broker.
const requeueDecommission = 10 * time.Second

type BrokerReconciler struct {
	Manager       multicluster.Manager
	ClientFactory internalclient.ClientFactory
	// UnbindPVCsAfter is the duration a pod must be stuck in Pending
	// with volume node-affinity conflict before the broker may be marked
	// DiskLost (given the dead-node proof also holds). Zero disables the
	// detection.
	UnbindPVCsAfter time.Duration
}

func SetupBrokerController(_ context.Context, mgr multicluster.Manager, clientFactory internalclient.ClientFactory, namespace string, unbindPVCsAfter time.Duration) error {
	// --unbind-pvcs-after is honored when set; otherwise fall back to the
	// controller's own default instead of disabling remediation.
	if unbindPVCsAfter <= 0 {
		unbindPVCsAfter = defaultUnbindPVCsAfter
	}
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		// Tests register several reconcilers against one manager under the
		// same default controller names; production registers each once.
		SkipNameValidation: ptr.To(true),
	}).For(
		&redpandav1alpha2.Broker{},
		mcbuilder.WithEngageWithLocalCluster(true),
		mcbuilder.WithEngageWithProviderClusters(true),
	).
		Owns(&corev1.Pod{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Owns(&corev1.PersistentVolumeClaim{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Complete(
			controller.FilterNamespaceReconciler(
				namespace,
				observability.Wrap[mcreconcile.Request](&BrokerReconciler{
					Manager:         mgr,
					ClientFactory:   clientFactory,
					UnbindPVCsAfter: unbindPVCsAfter,
				}, "Broker", periodicRequeue)))
}

type brokerReconciliationState struct {
	broker  *redpandav1alpha2.Broker
	pod     *corev1.Pod                  // nil when pod does not exist yet
	phase   redpandav1alpha2.BrokerPhase // empty = compute from pod status
	granted bool
	// clusterName is the multicluster-runtime cluster this request came
	// from. Admin-API clients must be built for it: the Broker's pods live
	// there, and targeting the local cluster's Redpanda instead would
	// register/decommission/drain against the wrong cluster.
	clusterName string
	// initialStatus snapshots Status as fetched, so syncBrokerStatus can
	// skip the API write when nothing changed (RFC Q11: rate-limit status
	// updates).
	initialStatus *redpandav1alpha2.BrokerStatus
	// registrationVerified is set when THIS reconcile confirmed via the
	// admin API that the pod is registered under the expected node_id. The
	// BrokerRegistered condition mirrors it, so it never reports stale
	// pre-rotation state.
	registrationVerified bool
	// registrationConflict carries the identity-mismatch message when the
	// pod re-registered under an unexpected node_id.
	registrationConflict string
}

type brokerReconcilerFn func(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error)

func (r *BrokerReconciler) fetchState(ctx context.Context, k8sClient client.Client, broker *redpandav1alpha2.Broker) (*brokerReconciliationState, error) {
	state := &brokerReconciliationState{
		broker:        broker,
		granted:       hasValidRollGrant(ctx, broker),
		initialStatus: broker.Status.DeepCopy(),
	}

	var pod corev1.Pod
	err := k8sClient.Get(ctx, client.ObjectKey{Name: broker.PodName(), Namespace: broker.Namespace}, &pod)
	switch {
	case apierrors.IsNotFound(err):
		// state.pod remains nil
	case err != nil:
		return nil, err
	default:
		state.pod = &pod
	}

	if broker.IsDiskLost() && state.pod != nil && !metav1.IsControlledBy(state.pod, broker) {
		// The dead incarnation released its network index and a replacement
		// Broker CR owns the pod of this name now. For this tombstone, the pod
		// does not exist: reasoning about the replacement's pod here would
		// short-circuit the tombstone's decommission (shadow-mode branch) and,
		// worse, let the decommission completion delete the replacement's
		// pod by name.
		state.pod = nil
	}

	return state, nil
}

func (r *BrokerReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("BrokerReconciler.Reconcile")
	l.Info("Reconciling", "object", req.NamespacedName.String(), "cluster", req.ClusterName)

	k8sCluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	k8sClient := k8sCluster.GetClient()

	var broker redpandav1alpha2.Broker
	if err := k8sClient.Get(ctx, req.NamespacedName, &broker); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if broker.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&broker, brokerFinalizerName) {
			controllerutil.AddFinalizer(&broker, brokerFinalizerName)
			if err := k8sClient.Update(ctx, &broker); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		return r.reconcileDelete(ctx, l, k8sClient, req.ClusterName, &broker, broker.PodName())
	}

	// A NodePool-referenced Broker without the cluster-name label cannot
	// derive its pod name — it would come out as "-<pool>-<index>", an
	// invalid DNS name rejected on every pod create. Surface Stuck instead
	// of error-looping. (The label is convention until CEL/webhook
	// validation exists.)
	if broker.Spec.ClusterRef.IsNodePool() && broker.Labels[redpandav1alpha2.ClusterNameLabel] == "" {
		l.Info("NodePool-referenced Broker is missing the cluster-name label; cannot derive its pod name",
			"label", redpandav1alpha2.ClusterNameLabel)
		state := &brokerReconciliationState{
			broker:        &broker,
			phase:         redpandav1alpha2.BrokerPhaseStuck,
			clusterName:   req.ClusterName,
			initialStatus: broker.Status.DeepCopy(),
		}
		return r.syncBrokerStatus(ctx, state, k8sCluster, ctrl.Result{RequeueAfter: periodicRequeue})
	}

	state, err := r.fetchState(ctx, k8sClient, &broker)
	if err != nil {
		return ctrl.Result{}, err
	}
	state.clusterName = req.ClusterName

	reconcilers := []brokerReconcilerFn{
		// reconcileDiskLost runs first: once a Broker is marked as a dead
		// incarnation, pod-ensure and PVC-ensure below must never run for
		// it again (they would resurrect the resources being dismantled).
		r.reconcileDiskLost,
		r.reconcilePVCs,
		r.reconcilePod,
		r.reconcilePVCAdoption,
		r.reconcilePodRotation,
		r.reconcilePodMetadata,
		r.reconcileBrokerRegistration,
		r.reconcileDecommission,
	}

	for _, fn := range reconcilers {
		result, err := fn(ctx, state, k8sCluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !result.IsZero() {
			return r.syncBrokerStatus(ctx, state, k8sCluster, result)
		}
	}

	return r.syncBrokerStatus(ctx, state, k8sCluster, ctrl.Result{})
}

func (r *BrokerReconciler) reconcilePVCs(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	if broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}

	l := log.FromContext(ctx)
	k8sClient := cluster.GetClient()
	scheme := cluster.GetScheme()
	podName := broker.PodName()

	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		pvcName := fmt.Sprintf("%s-%s", vct.Name, podName)
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: broker.Namespace}, &pvc); err == nil {
			// A PVC mid-deletion (e.g. PV-affinity remediation) must be fully
			// gone before recreating it or the pod: pvc-protection releases a
			// Terminating PVC only once no pod references it, so recreating
			// the pod first pins the old PVC forever and the pod never
			// schedules.
			if !pvc.DeletionTimestamp.IsZero() {
				l.Info("waiting for PVC deletion to complete", "pvc", pvcName)
				return ctrl.Result{RequeueAfter: requeueShort}, nil
			}
		} else {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			pvc = corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvcName,
					Namespace: broker.Namespace,
					Labels:    broker.Spec.PodTemplate.Labels,
				},
				Spec: vct.Spec,
			}
			if err := controllerutil.SetControllerReference(broker, &pvc, scheme); err != nil {
				return ctrl.Result{}, err
			}
			l.Info("creating PVC", "name", pvcName)
			if err := k8sClient.Create(ctx, &pvc); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *BrokerReconciler) reconcilePod(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	k8sClient := cluster.GetClient()
	scheme := cluster.GetScheme()
	broker := state.broker
	podName := broker.PodName()

	if state.pod == nil {
		if broker.Spec.Decommission {
			// A decommission that has already resolved its identity (or
			// finished) must not resurrect the pod: mid-flight the broker
			// drains via other members, and Decommissioned is terminal. But
			// with NO identity yet there is nothing to decommission — and on
			// a single-broker cluster the pod is the only admin endpoint, so
			// staying podless deadlocks reconcileDecommission's identity
			// resolution forever (e.g. a rotation deleted the pod right as
			// the intent was set). Recreate the pod so the decommission can
			// actually start. Side effect: a Broker created WITH the intent
			// already set briefly runs (and joins) before decommissioning —
			// preferable to wedging in Decommissioning.
			if broker.Status.BrokerID != nil || broker.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned {
				return ctrl.Result{}, nil
			}
		}
		// Pod-ensure is deliberately NOT gated on a roll-grant (RFC Q5):
		// creation is non-disruptive, initial cluster bootstrap needs all
		// pods in parallel, and an expired grant must never strand a broker
		// between rotation's delete and the recreate. Only disruptive
		// actions (rotation, PV remediation) require a grant.
		l.Info("creating pod (no existing pod found)", "name", podName)
		newPod := broker.BuildPod(podName)
		if err := controllerutil.SetControllerReference(broker, newPod, scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := k8sClient.Create(ctx, newPod); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	pod := state.pod
	ownerRef := metav1.GetControllerOf(pod)
	if ownerRef != nil && !metav1.IsControlledBy(pod, broker) {
		l.Info("pod owned by another controller (shadow mode)", "owner", ownerRef.Kind+"/"+ownerRef.Name)
		state.phase = redpandav1alpha2.BrokerPhasePending
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}
	if ownerRef == nil {
		l.Info("adopting orphaned pod", "name", podName)
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		// Stamp desired rotation keys ONLY when the pod carries none — the
		// STS→Broker migration case, where preconditions verified the pod
		// already runs the desired config and adoption must not queue a
		// pointless rotation. A pod that already carries a value
		// (self-heal re-adoption after a raw CR deletion) keeps its live
		// one: overwriting it with the desired value would mark a stale pod
		// current and silently skip its rotation.
		backfillRotationKeys(broker, pod)
		if err := controllerutil.SetControllerReference(broker, pod, scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := k8sClient.Update(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Owned pods created before pod-template-hash tracking existed lack the
	// hash annotation; treating them as outdated would roll the whole fleet
	// on operator upgrade. Backfill the desired hash instead — template
	// drift predating the hash is undetectable either way, and every future
	// template change rotates through the freshly stamped value.
	if _, ok := pod.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]; !ok {
		if backfillRotationKeys(broker, pod) {
			l.Info("backfilling pod-template-hash on pre-hash pod", "name", podName)
			if err := k8sClient.Update(ctx, pod); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

// backfillRotationKeys copies each desired rotation annotation onto the pod
// when the pod carries none, reporting whether anything was stamped. It
// never overwrites a live value.
func backfillRotationKeys(broker *redpandav1alpha2.Broker, pod *corev1.Pod) bool {
	keys := []string{
		redpandav1alpha2.BrokerPodTemplateHashAnnotation,
		redpandav1alpha2.BrokerConfigChecksumAnnotation,
	}
	stamped := false
	for _, key := range keys {
		if _, ok := pod.Annotations[key]; ok {
			continue
		}
		if desired := broker.Spec.PodTemplate.Annotations[key]; desired != "" {
			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			pod.Annotations[key] = desired
			stamped = true
		}
	}
	return stamped
}

func (r *BrokerReconciler) reconcilePVCAdoption(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pod == nil {
		return ctrl.Result{}, nil
	}
	// Don't re-adopt claims on a decommissioning broker: the completion
	// branch is about to delete them anyway.
	if state.broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
	l := log.FromContext(ctx)
	k8sClient := cluster.GetClient()
	scheme := cluster.GetScheme()
	broker := state.broker

	for _, ec := range broker.Spec.Storage.ExistingClaims {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: ec.Name, Namespace: broker.Namespace}, &pvc); err != nil {
			l.Info("could not get PVC for adoption", "name", ec.Name, "error", err)
			continue
		}
		if metav1.GetControllerOf(&pvc) != nil {
			continue
		}
		if err := controllerutil.SetControllerReference(broker, &pvc, scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := k8sClient.Update(ctx, &pvc); err != nil {
			return ctrl.Result{}, err
		}
		l.Info("adopted PVC", "name", ec.Name)
	}
	return ctrl.Result{}, nil
}

// reconcileDiskLost owns the disk-loss lifecycle of a dead incarnation:
// detection (mark the durable DiskLost latch FIRST — the point of no
// return), dismantle (idempotent pod+PVC deletion until confirmed gone on
// an uncached read), and the index-release checkpoint that lets the owning
// engine create a replacement Broker under the same pod and PVC names.
// While the latch is set and no decommission intent exists, it
// short-circuits the chain: pod-ensure and PVC-ensure are disabled for a
// dead incarnation. Once the engine sets Spec.Decommission (after the
// replacement registers), it steps aside and the ordinary decommission
// machinery runs pod-less against the recorded BrokerID.
func (r *BrokerReconciler) reconcileDiskLost(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	broker := state.broker

	if !broker.IsDiskLost() {
		return r.detectDiskLost(ctx, l, state, cluster)
	}

	if broker.Spec.Decommission {
		if broker.Status.BrokerID == nil {
			// Belt-and-braces (the engine deletes unregistered tombstones
			// without marking them): never let a tombstone without a recorded
			// identity reach the decommission resolve-by-pod-name — the
			// name may belong to the replacement by now, and resolving it
			// would decommission the replacement's node_id.
			l.Info("DiskLost tombstone has no recorded node_id; nothing to decommission")
			state.phase = redpandav1alpha2.BrokerPhaseDecommissioned
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		// Fall through: reconcileDecommission runs the pure admin-API
		// decommission against the recorded id; every pod/PVC-touching
		// reconciler in between already skips on Spec.Decommission.
		return ctrl.Result{}, nil
	}

	return r.dismantleDiskLost(ctx, l, state, cluster)
}

// detectDiskLost marks the Broker as a dead incarnation when its pod is
// provably unschedulable because its storage is pinned to a node that no
// longer exists. Nothing is deleted on the marking pass: dismantle only
// acts on the persisted latch, which is what makes the mark crash-safe.
func (r *BrokerReconciler) detectDiskLost(ctx context.Context, l logr.Logger, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker

	// A decommissioning broker is draining: its storage and identity belong
	// to reconcileDecommission. A shadow/STS-owned pod (migration window)
	// must never be marked — the PVCUnbinder owns remediation until
	// handover.
	if broker.Spec.Decommission ||
		state.pod == nil ||
		!metav1.IsControlledBy(state.pod, broker) ||
		!pvcunbinder.PodHasVolumeAffinityUnschedulable(state.pod) {
		return ctrl.Result{}, nil
	}

	if r.UnbindPVCsAfter <= 0 {
		// Unreachable through SetupBrokerController (which defaults the
		// value); guards direct construction only.
		l.Info("disk-loss detection disabled (UnbindPVCsAfter=0); broker will stay Stuck until the node returns or the PVC is removed manually")
		return ctrl.Result{}, nil
	}

	// Timeout: only accept the proof after the pod has been stuck long
	// enough for a rebooting node to have come back.
	for _, cond := range state.pod.Status.Conditions {
		if cond.Type != corev1.PodScheduled || cond.Status != corev1.ConditionFalse {
			continue
		}
		if delta := r.UnbindPVCsAfter - time.Since(cond.LastTransitionTime.Time); delta > 0 {
			l.Info("pod stuck on PV node affinity but disk-loss timeout not reached", "remaining", delta.Round(time.Second))
			return ctrl.Result{RequeueAfter: delta}, nil
		}
	}

	// Re-qualify the pod on an uncached read, decoded into a fresh object,
	// immediately before accepting the proof: a stale informer copy of a
	// since-resolved (or since-replaced) pod must not authorize the
	// terminal marking.
	apiReader := cluster.GetAPIReader()
	fresh := &corev1.Pod{}
	if err := apiReader.Get(ctx, client.ObjectKeyFromObject(state.pod), fresh); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if fresh.UID != state.pod.UID || !pvcunbinder.PodHasVolumeAffinityUnschedulable(fresh) {
		l.Info("pod no longer qualifies on uncached re-read; skipping disk-loss marking", "pod", state.pod.Name)
		return ctrl.Result{}, nil
	}

	lost, err := pvcunbinder.LostDiskClaims(ctx, apiReader, fresh)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(lost) == 0 {
		// No dead-node proof (e.g. a live-node mis-pin): stays Stuck via
		// defaultPhase; an operator has to intervene.
		return ctrl.Result{}, nil
	}

	names := make([]string, 0, len(lost))
	for i := range lost {
		names = append(names, lost[i].Name)
	}
	l.Info("marking Broker as a dead incarnation: storage pinned to a node that no longer exists",
		"claims", names, "brokerID", broker.Status.BrokerID)
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}
	state.phase = redpandav1alpha2.BrokerPhaseDiskLost
	return ctrl.Result{RequeueAfter: requeueShort}, nil
}

// dismantleDiskLost deletes the dead incarnation's pod and PVCs, repeatably,
// and sets the ResourcesReleased checkpoint only once every resource is
// confirmed gone on an uncached read — from then on the network index is
// free for a replacement Broker. Claim names derive from the SPEC, not the
// pod: the pod may already be gone.
func (r *BrokerReconciler) dismantleDiskLost(ctx context.Context, l logr.Logger, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	state.phase = redpandav1alpha2.BrokerPhaseDiskLost

	if broker.DiskLostReleased() {
		// Waiting for the engine: replacement creation, then the
		// decommission mark.
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

	k8sClient := cluster.GetClient()
	if state.pod != nil {
		l.Info("disk-lost dismantle: deleting pod", "pod", state.pod.Name)
		if err := k8sClient.Delete(ctx, state.pod); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	podName := broker.PodName()
	claimNames := make([]string, 0, len(broker.Spec.Storage.VolumeClaimTemplates)+len(broker.Spec.Storage.ExistingClaims))
	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		claimNames = append(claimNames, fmt.Sprintf("%s-%s", vct.Name, podName))
	}
	for _, ec := range broker.Spec.Storage.ExistingClaims {
		claimNames = append(claimNames, ec.Name)
	}
	for _, name := range claimNames {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: broker.Namespace}, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		// Guard against name reuse: never delete a claim another owner
		// already controls (the replacement's fresh claim, should orderings
		// ever race).
		if owner := metav1.GetControllerOf(&pvc); owner != nil && !metav1.IsControlledBy(&pvc, broker) {
			continue
		}
		l.Info("disk-lost dismantle: deleting PVC", "pvc", name)
		if err := k8sClient.Delete(ctx, &pvc); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Confirm on the uncached reader: the release checkpoint authorizes a
	// replacement under these very names, so a stale "gone" must not set it.
	apiReader := cluster.GetAPIReader()
	var pod corev1.Pod
	err := apiReader.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, &pod)
	switch {
	case err == nil:
		if metav1.IsControlledBy(&pod, broker) {
			return ctrl.Result{RequeueAfter: requeueShort}, nil // still terminating
		}
	case !apierrors.IsNotFound(err):
		return ctrl.Result{}, err
	}
	for _, name := range claimNames {
		var pvc corev1.PersistentVolumeClaim
		err := apiReader.Get(ctx, client.ObjectKey{Name: name, Namespace: broker.Namespace}, &pvc)
		switch {
		case err == nil:
			if owner := metav1.GetControllerOf(&pvc); owner == nil || metav1.IsControlledBy(&pvc, broker) {
				return ctrl.Result{RequeueAfter: requeueShort}, nil // still terminating
			}
		case !apierrors.IsNotFound(err):
			return ctrl.Result{}, err
		}
	}

	l.Info("disk-lost dismantle complete: pod and PVCs gone, network index released")
	broker.Status.DiskLost.ResourcesReleased = true
	return ctrl.Result{RequeueAfter: requeueShort}, nil
}

func (r *BrokerReconciler) reconcilePodRotation(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pod == nil {
		return ctrl.Result{}, nil
	}
	broker := state.broker
	// Never rotate a decommissioning broker: the pod must keep running
	// (draining) until the decommission completes and deletes it.
	if broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
	// PodOutdated covers the pod SPEC hash (image, resources, env, ...), the
	// config checksum and the restart-requiring cluster-config version (see
	// resources.MarkBrokersForRestart); the recreated pod inherits all of
	// them from the desired template, clearing the drift. Template metadata
	// never rotates a pod — reconcilePodMetadata syncs it in place.
	if !broker.PodOutdated(state.pod) {
		return ctrl.Result{}, nil
	}

	l := log.FromContext(ctx)
	if !state.granted {
		l.Info("pod needs rotation but no roll-grant", "name", state.pod.Name)
		// An interrupted drain (grant expired or re-keyed away mid-roll,
		// controller restart) must not strand this broker in maintenance
		// mode: only one broker may be in maintenance at a time, so a
		// stranded one deadlocks every future grantee's drain. Parking here
		// short-circuits the chain before reconcileBrokerRegistration's
		// disable, so undo it best-effort now.
		if broker.Status.BrokerID != nil {
			if err := r.disableMaintenanceMode(ctx, state.clusterName, broker); err != nil {
				l.V(1).Info("could not disable maintenance mode while parked without a grant", "error", err)
			}
		}
		state.phase = redpandav1alpha2.BrokerPhaseRunning
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}
	// A READY pod without an adopted identity registers within seconds —
	// hold the rotation until then so it always drains first. Rotating in
	// that window deletes a serving broker undrained, and (on a
	// single-broker cluster) can race a decommission intent into a podless
	// deadlock. An UNREADY pod deliberately keeps rotating without an
	// identity: it may never register at all (e.g. crash-looping on the very
	// config this rotation fixes), and there is no leadership to drain.
	//
	// This MUST return a zero Result: a non-zero one stops the reconciler
	// chain before reconcileBrokerRegistration ever runs, so the identity
	// this branch is waiting on would never be adopted — a livelock.
	// Falling through lets registration adopt the ID (its status update
	// triggers the next pass, which rotates with a drain) or schedule its
	// own retry when membership hasn't caught up yet.
	if broker.Status.BrokerID == nil && isPodReady(state.pod) {
		l.Info("pod needs rotation but broker identity not yet adopted, deferring", "name", state.pod.Name)
		state.phase = redpandav1alpha2.BrokerPhaseRunning
		return ctrl.Result{}, nil
	}
	if broker.Status.BrokerID != nil {
		drained, err := r.ensureDrained(ctx, state.clusterName, broker)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("draining broker %d: %w", *broker.Status.BrokerID, err)
		}
		if !drained {
			// Re-check quickly: while this broker holds the roll-grant, no
			// other broker can roll — every second spent waiting here extends
			// the whole fleet's roll duration. The broker is registered and
			// serving while it drains, so the phase stays Running — anything
			// else would blink a false "being created" on every rotation.
			l.Info("waiting for leadership drain before rotation", "brokerID", *broker.Status.BrokerID)
			state.phase = redpandav1alpha2.BrokerPhaseRunning
			return ctrl.Result{RequeueAfter: requeueDrain}, nil
		}
	}
	l.Info("rotating pod", "name", state.pod.Name,
		"oldChecksum", state.pod.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation],
		"newChecksum", broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation],
		"oldTemplateHash", state.pod.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation],
		"newTemplateHash", broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation])
	if err := cluster.GetClient().Delete(ctx, state.pod); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// reconcilePodMetadata converges the live pod's labels and annotations
// toward the desired pod template IN PLACE — metadata is mutable on running
// pods, so propagating it must not cost a drain and a restart the way spec
// changes do. The sync is deliberately NOT grant-gated (it is
// non-disruptive) and deliberately additive: keys removed from
// the template stay on the pod until its next rotation rebuilds it from the
// template — deleting would require knowing which pod keys the template ever
// owned, versus keys added by kubelet, the CNI, or the cluster controller.
// The rotation identity keys (RotationAnnotations) are NEVER synced: on a
// pod they mean "what this pod was created from", and overwriting them with
// desired values would silently swallow a pending rotation.
func (r *BrokerReconciler) reconcilePodMetadata(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pod == nil || state.broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
	pod := state.pod
	tpl := state.broker.Spec.PodTemplate

	patch := client.MergeFrom(pod.DeepCopy())
	changed := false
	for k, v := range tpl.Annotations {
		if slices.Contains(redpandav1alpha2.RotationAnnotations, k) {
			continue
		}
		if pod.Annotations[k] != v {
			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			pod.Annotations[k] = v
			changed = true
		}
	}
	for k, v := range tpl.Labels {
		if pod.Labels[k] != v {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[k] = v
			changed = true
		}
	}
	if !changed {
		return ctrl.Result{}, nil
	}

	log.FromContext(ctx).Info("syncing pod metadata in place", "name", pod.Name)
	if err := cluster.GetClient().Patch(ctx, pod, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("syncing metadata onto pod %s: %w", pod.Name, err)
	}
	return ctrl.Result{}, nil
}

// reconcileBrokerRegistration verifies cluster membership on EVERY pass with
// a ready pod: it resolves the node_id the pod currently reports and checks
// registration continuity (RFC rolling step 3: after a rotation the broker
// must come back with the SAME node_id). The BrokerRegistered condition is
// recomputed from this pass's observation, so it can never report stale
// pre-rotation state, and the cluster controller only revokes a roll-grant
// once continuity has been re-verified.
func (r *BrokerReconciler) reconcileBrokerRegistration(ctx context.Context, state *brokerReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	// A decommissioning broker is leaving the cluster: there is no
	// registration to maintain, and blocking the chain here would prevent
	// reconcileDecommission from ever observing completion (the broker
	// disappears from membership when the decommission finishes).
	if broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
	// Discovery must not wait for Kubernetes readiness: the readiness probe
	// reflects CLUSTER health (rpk cluster health), which can be false for
	// exactly the reason registration needs to resolve — a DiskLost
	// replacement registers its fresh node_id while the dead member still
	// keeps the health overview unhealthy, and the dead id's decommission
	// waits for that registration. Gating on readiness would deadlock the
	// three of them. A Running pod with an IP is resolvable (resolveBroker
	// matches by address); a not-yet-registered broker resolves to nothing
	// and requeues. BrokerRegistered is documented as orthogonal to Ready.
	if state.pod == nil || state.pod.Status.Phase != corev1.PodRunning || state.pod.Status.PodIP == "" {
		return ctrl.Result{}, nil
	}

	l := log.FromContext(ctx)
	podName := broker.PodName()

	resolved, found, err := r.resolveBroker(ctx, state.clusterName, broker, state.pod, podName)
	if err != nil {
		l.Info("could not resolve broker ID, will retry", "error", err)
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}
	if !found {
		if broker.Status.BrokerID != nil {
			// Was registered, no longer a member (e.g. removed out of
			// band): report unverified but let the chain continue.
			l.Info("broker no longer present in cluster membership", "brokerID", *broker.Status.BrokerID)
			return ctrl.Result{}, nil
		}
		// Pod is up but not yet a cluster member.
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}
	currentID := ptr.To(int32(resolved.NodeID))

	if broker.Status.BrokerID == nil {
		// Adopt only an active, alive member. Right after a decommission the
		// membership list can briefly retain the dead predecessor's entry
		// under this very pod name — adopting its id would poison the
		// continuity check below for the rest of this Broker's life.
		if !brokerActiveAndAlive(resolved) {
			l.Info("matched membership entry not active/alive yet, deferring identity adoption",
				"nodeID", resolved.NodeID, "membership", resolved.MembershipStatus)
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		broker.Status.BrokerID = currentID
	}
	if *currentID != *broker.Status.BrokerID {
		// The pod re-registered with a fresh identity — its data dir did not
		// survive. Refuse to adopt the new ID and surface the conflict; this
		// needs an operator decision (replace the broker), not silent
		// acceptance.
		state.registrationConflict = fmt.Sprintf(
			"broker re-registered with node_id %d, expected %d", *currentID, *broker.Status.BrokerID)
		state.phase = redpandav1alpha2.BrokerPhaseStuck
		l.Error(fmt.Errorf("node_id changed from %d to %d", *broker.Status.BrokerID, *currentID),
			"broker identity changed — not disabling maintenance mode")
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

	state.registrationVerified = true
	if err := r.disableMaintenanceMode(ctx, state.clusterName, broker); err != nil {
		l.Info("could not disable maintenance mode", "error", err)
	}

	return ctrl.Result{}, nil
}

func (r *BrokerReconciler) reconcileDecommission(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	if !broker.Spec.Decommission {
		// Unsetting the intent field mid-decommission recommissions the
		// broker via the admin API (RFC Q2 review decision).
		if broker.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioning && broker.Status.BrokerID != nil {
			return r.executeRecommission(ctx, state)
		}
		return ctrl.Result{}, nil
	}

	if broker.Status.BrokerID == nil {
		// Decommissioned is terminal: the identity was removed and BrokerID
		// cleared on completion — don't relabel it as in-progress.
		if broker.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned {
			state.phase = redpandav1alpha2.BrokerPhaseDecommissioned
			return ctrl.Result{}, nil
		}
		// reconcileBrokerRegistration skips decommissioning brokers, so the
		// ID must be resolved live here — mirroring the deletion path —
		// or an intent set before the first registration (or right after a
		// status-update race) never actually starts the decommission and the
		// broker stays a full cluster member while reporting Decommissioning.
		resolved, found, err := r.resolveBroker(ctx, state.clusterName, broker, state.pod, broker.PodName())
		if err != nil || !found || resolved.MembershipStatus != rpadmin.MembershipStatusActive {
			if err != nil {
				log.FromContext(ctx).Info("could not resolve broker ID before decommission, will retry", "error", err)
			}
			state.phase = redpandav1alpha2.BrokerPhaseDecommissioning
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		broker.Status.BrokerID = ptr.To(int32(resolved.NodeID))
	}

	decommResult, err := r.executeDecommission(ctx, state.clusterName, broker)
	if err != nil {
		return ctrl.Result{}, err
	}
	state.phase = decommResult.phase
	if decommResult.requeue {
		return ctrl.Result{RequeueAfter: requeueDecommission}, nil
	}

	if state.phase == redpandav1alpha2.BrokerPhaseDecommissioned {
		// The identity is gone from the cluster for good. Clearing it lets
		// a later revival (intent unset after completion) adopt the fresh
		// node_id instead of tripping the rotation continuity check.
		broker.Status.BrokerID = nil
		l := log.FromContext(ctx)
		k8sClient := cluster.GetClient()
		podName := broker.PodName()

		// Every delete below is guarded by controller ownership: a DiskLost
		// tombstone's decommission completes AFTER a replacement Broker took
		// over the network index, so the pod and PVCs answering to these
		// names belong to the replacement and must be left alone.
		if state.pod != nil && metav1.IsControlledBy(state.pod, broker) {
			l.Info("deleting pod after decommission", "name", podName)
			if err := k8sClient.Delete(ctx, state.pod); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		claimNames := make([]string, 0, len(broker.Spec.Storage.VolumeClaimTemplates)+len(broker.Spec.Storage.ExistingClaims))
		for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
			claimNames = append(claimNames, fmt.Sprintf("%s-%s", vct.Name, podName))
		}
		for _, ec := range broker.Spec.Storage.ExistingClaims {
			claimNames = append(claimNames, ec.Name)
		}
		for _, pvcName := range claimNames {
			var pvc corev1.PersistentVolumeClaim
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: broker.Namespace}, &pvc); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				continue
			}
			if owner := metav1.GetControllerOf(&pvc); owner != nil && !metav1.IsControlledBy(&pvc, broker) {
				continue
			}
			l.Info("deleting PVC after decommission", "name", pvcName)
			if err := k8sClient.Delete(ctx, &pvc); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// defaultPhase derives the informational phase for a reconcile pass that
// didn't pin an explicit one (Pending/Decommissioning/Decommissioned/Stuck
// are all set explicitly by their handlers). Registration is a one-way gate
// out of Provisioning: a broker that has joined the cluster stays Running
// through pod readiness dips — the Ready condition carries transient health.
// The pod readiness probe is cluster-scoped, so a single restarting broker
// would otherwise regress every registered broker's phase to Provisioning.
func defaultPhase(broker *redpandav1alpha2.Broker, pod *corev1.Pod) redpandav1alpha2.BrokerPhase {
	if broker.IsDiskLost() {
		// The latch is terminal; any pass that falls through to the default
		// must never regress a dead incarnation's phase. Decommissioning /
		// Decommissioned still win — they arrive via explicit state.phase
		// and bypass this function.
		return redpandav1alpha2.BrokerPhaseDiskLost
	}
	phase := redpandav1alpha2.BrokerPhaseProvisioning
	if broker.Status.BrokerID != nil || isPodReady(pod) {
		phase = redpandav1alpha2.BrokerPhaseRunning
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == "Unschedulable" {
			phase = redpandav1alpha2.BrokerPhaseStuck
		}
	}
	if reason := podStuckReason(pod); reason != "" {
		phase = redpandav1alpha2.BrokerPhaseStuck
	}
	return phase
}

func (r *BrokerReconciler) syncBrokerStatus(ctx context.Context, state *brokerReconciliationState, k8sCluster cluster.Cluster, result ctrl.Result) (ctrl.Result, error) {
	broker := state.broker
	pod := state.pod
	if pod == nil {
		pod = &corev1.Pod{}
	}
	k8sClient := k8sCluster.GetClient()
	status := statuses.NewBroker()

	phase := state.phase
	if phase == "" {
		phase = defaultPhase(broker, pod)
	}
	if state.registrationConflict != "" {
		phase = redpandav1alpha2.BrokerPhaseStuck
	}

	broker.Status.Phase = phase
	broker.Status.PodName = pod.Name
	broker.Status.PodIP = pod.Status.PodIP

	if isPodReady(pod) {
		status.SetReady(statuses.BrokerReadyReasonReady)
	} else {
		status.SetReady(statuses.BrokerReadyReasonNotReady, "Pod is not ready")
	}

	switch state.pod {
	case nil:
		status.SetPodScheduled(statuses.BrokerPodScheduledReasonPodMissing, "Pod not found")
	default:
		scheduledMessage := "Pod is not scheduled"
		scheduled := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodScheduled {
				scheduled = c.Status == corev1.ConditionTrue
				if c.Message != "" {
					scheduledMessage = c.Message
				}
				break
			}
		}
		if scheduled {
			status.SetPodScheduled(statuses.BrokerPodScheduledReasonScheduled)
		} else {
			status.SetPodScheduled(statuses.BrokerPodScheduledReasonUnschedulable, scheduledMessage)
		}
	}

	// BrokerRegistered mirrors THIS pass's admin-API observation, so it can
	// never report stale pre-rotation registration (RFC rolling step 3: a
	// rotated broker must come back with the same node_id before the roll is
	// considered complete).
	switch {
	case state.registrationConflict != "":
		status.SetBrokerRegistered(statuses.BrokerBrokerRegisteredReasonIdentityChanged, state.registrationConflict)
	case state.registrationVerified:
		status.SetBrokerRegistered(statuses.BrokerBrokerRegisteredReasonRegistered, fmt.Sprintf("Broker ID %d", *broker.Status.BrokerID))
	default:
		status.SetBrokerRegistered(statuses.BrokerBrokerRegisteredReasonNotRegistered, "Broker registration not verified this reconcile")
	}

	if broker.PodOutdated(pod) {
		status.SetConfigSynced(statuses.BrokerConfigSyncedReasonOutdated,
			fmt.Sprintf("desired checksum=%s version=%s, pod checksum=%s version=%s",
				broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation],
				broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation],
				pod.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation],
				pod.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation]))
	} else {
		status.SetConfigSynced(statuses.BrokerConfigSyncedReasonSynced)
	}

	allBound := true
	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		pvcName := fmt.Sprintf("%s-%s", vct.Name, broker.PodName())
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: broker.Namespace}, &pvc); err != nil || pvc.Status.Phase != corev1.ClaimBound {
			allBound = false
			break
		}
	}
	for _, ec := range broker.Spec.Storage.ExistingClaims {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: ec.Name, Namespace: broker.Namespace}, &pvc); err != nil || pvc.Status.Phase != corev1.ClaimBound {
			allBound = false
			break
		}
	}
	if allBound {
		status.SetStorageBound(statuses.BrokerStorageBoundReasonBound)
	} else {
		status.SetStorageBound(statuses.BrokerStorageBoundReasonPending, "One or more PVCs are not bound")
	}

	// Quiesced and Stable are derived by the generated roll-up: Quiesced when
	// every condition above was evaluated without transient errors, Stable
	// when Ready, StorageBound, BrokerRegistered, ConfigSynced, and Quiesced
	// are all True (statuses.yaml rollup).
	//
	// Skip the API write when nothing changed (RFC Q11): reconciles fire on
	// every pod/PVC event plus a periodic requeue, and unconditional status
	// PUTs across N brokers add up.
	conditionsChanged := status.UpdateConditions(broker)
	initial := state.initialStatus
	fieldsChanged := initial == nil ||
		initial.Phase != broker.Status.Phase ||
		initial.PodName != broker.Status.PodName ||
		initial.PodIP != broker.Status.PodIP ||
		!ptr.Equal(initial.BrokerID, broker.Status.BrokerID) ||
		!reflect.DeepEqual(initial.DiskLost, broker.Status.DiskLost)
	if conditionsChanged || fieldsChanged {
		if err := k8sClient.Status().Update(ctx, broker); err != nil {
			return ctrl.Result{}, err
		}
	}
	if !result.IsZero() {
		return result, nil
	}
	return ctrl.Result{RequeueAfter: periodicRequeue}, nil
}

// executeRecommission cancels an in-flight decommission after the intent
// field was unset. Recommission is only possible while the decommission is
// still in progress; if it already completed (or the broker is unknown), the
// phase simply falls back to being computed from pod state and the standard
// pod-ensure path revives the broker slot.
func (r *BrokerReconciler) executeRecommission(ctx context.Context, state *brokerReconciliationState) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	broker := state.broker
	brokerID := int(*broker.Status.BrokerID)

	admin, err := r.ClientFactory.RedpandaAdminClientForCluster(ctx, broker, state.clusterName)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer admin.Close()

	l.Info("recommissioning broker, decommission intent was removed", "brokerID", brokerID)
	if err := admin.RecommissionBroker(ctx, brokerID); err != nil {
		// Not decommissioning (never started, or already finished): nothing
		// to cancel — let the phase recompute from pod state.
		if strings.Contains(err.Error(), "is not decommissioning") {
			l.Info("broker is not decommissioning, nothing to recommission", "brokerID", brokerID)
		} else {
			return ctrl.Result{}, fmt.Errorf("recommissioning broker %d: %w", brokerID, err)
		}
	}
	state.phase = "" // recompute from pod state
	return ctrl.Result{}, nil
}

type decommissionResult struct {
	phase   redpandav1alpha2.BrokerPhase
	requeue bool
}

func (r *BrokerReconciler) executeDecommission(ctx context.Context, clusterName string, broker *redpandav1alpha2.Broker) (decommissionResult, error) {
	l := log.FromContext(ctx)
	brokerID := int(*broker.Status.BrokerID)

	admin, err := r.ClientFactory.RedpandaAdminClientForCluster(ctx, broker, clusterName)
	if err != nil {
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}
	defer admin.Close()

	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}

	// A re-entry after a lost terminal status write — the completion pass
	// crashed, or its status Update hit a conflict — arrives here with a
	// recorded BrokerID that is no longer a cluster member. Detect that
	// BEFORE the last-broker guard: the guard cannot tell "would remove the
	// last member" from "already removed, stale retry", and on a drain that
	// just shrank the membership it would park this Broker in Stuck forever.
	member := false
	for i := range brokers {
		if brokers[i].NodeID == brokerID {
			member = true
			break
		}
	}
	if !member {
		l.Info("broker already absent from cluster membership, decommission finished", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioned}, nil
	}

	// Last-broker guard (RFC Q2).
	if len(brokers) <= 1 {
		l.Info("blocking decommission: last broker in cluster", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseStuck}, nil
	}

	status, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			l.Info("initiating decommission", "brokerID", brokerID)
			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
			}
			return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning, requeue: true}, nil
		}
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}

	if !status.Finished {
		l.Info("decommission in progress", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning, requeue: true}, nil
	}

	l.Info("decommission finished", "brokerID", brokerID)
	return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioned}, nil
}

// reconcileDelete handles Broker CR deletion (RFC Q2):
//
//   - Decommission runs ONLY when Spec.Decommission is set — never on raw CR
//     deletion alone, and never during owner teardown (the admin API is dying
//     under it; see the case body) — and a Stuck result (e.g. last-broker
//     guard) blocks deletion instead of falling through to pod removal.
//   - Raw deletion while the owning cluster is alive RELEASES the pod and
//     PVCs (ownerRefs stripped): the broker keeps running and data survives.
//     Once an owning cluster controller manages Broker CRs (none is wired up
//     yet), it will recreate a Broker CR that re-adopts the pod — accidental
//     deletion then self-heals without a restart.
//   - When the owning cluster itself is being deleted, the propagated
//     deletion policy decides: "cascade" (default) lets the GC delete pod and
//     PVCs with the CR — whole-cluster teardown, decommission is pointless —
//     while "orphan" releases them.
func (r *BrokerReconciler) reconcileDelete(ctx context.Context, l logr.Logger, k8sClient client.Client, clusterName string, broker *redpandav1alpha2.Broker, podName string) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(broker, brokerFinalizerName) {
		return ctrl.Result{}, nil
	}

	var pod corev1.Pod
	err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, &pod)
	switch {
	case apierrors.IsNotFound(err):
		// The pod is already gone (e.g. deleted out of band), but the PVCs
		// may still exist and carry this CR's controller ownerRef. Apply the
		// same policy decision as the pod-present path — otherwise the CR
		// deletion cascades and the GC deletes the data regardless of
		// intent.
		if r.deletionPolicy(ctx, l, k8sClient, broker) == deletionPolicyCascade {
			l.Info("pod already gone, cascade policy, removing finalizer")
		} else {
			if err := r.releaseBrokerResources(ctx, l, k8sClient, broker, nil); err != nil {
				return ctrl.Result{}, err
			}
			l.Info("pod already gone, PVCs released, removing finalizer")
		}

	case err != nil:
		return ctrl.Result{}, err

	case !metav1.IsControlledBy(&pod, broker):
		l.Info("pod not owned by this Broker CR, removing finalizer (rollback case)", "owner", metav1.GetControllerOf(&pod))

	case broker.Spec.Decommission:
		// Deletion WITH explicit intent: decommission, then let the CR
		// deletion cascade pod and PVCs — UNLESS the owning cluster itself
		// is being torn down. During teardown the sibling Brokers are being
		// deleted concurrently, so the admin API dies under the decommission
		// (or the last-broker guard trips) and insisting on completion wedges
		// this CR's finalizer forever: pod and PVCs leak in Terminating and
		// namespace deletion hangs. Teardown also makes the decommission
		// pointless — the whole cluster is going away — so apply the
		// deletion policy directly, exactly like intent-less deletion.
		if r.ownerTearingDown(ctx, l, k8sClient, broker) {
			if r.deletionPolicy(ctx, l, k8sClient, broker) == deletionPolicyCascade {
				l.Info("cluster teardown, skipping decommission, removing finalizer (cascade policy)", "name", broker.Name)
			} else {
				l.Info("cluster teardown, skipping decommission, releasing resources (orphan policy)", "name", broker.Name)
				if err := r.releaseBrokerResources(ctx, l, k8sClient, broker, &pod); err != nil {
					return ctrl.Result{}, err
				}
			}
			break
		}

		// Status.BrokerID may be nil because a status update raced — resolve
		// it live rather than skipping the decommission and leaving a dead
		// membership entry behind.
		if broker.Status.BrokerID == nil {
			resolved, found, err := r.resolveBroker(ctx, clusterName, broker, &pod, podName)
			if err != nil {
				l.Info("could not resolve broker ID before decommission, will retry", "error", err)
				return ctrl.Result{RequeueAfter: requeueShort}, nil
			}
			if found && resolved.MembershipStatus == rpadmin.MembershipStatusActive {
				broker.Status.BrokerID = ptr.To(int32(resolved.NodeID))
			}
		}
		if broker.Status.BrokerID != nil {
			result, err := r.executeDecommission(ctx, clusterName, broker)
			if err != nil {
				return ctrl.Result{}, err
			}
			broker.Status.Phase = result.phase
			if err := k8sClient.Status().Update(ctx, broker); err != nil {
				return ctrl.Result{}, err
			}
			if result.phase == redpandav1alpha2.BrokerPhaseStuck {
				l.Info("decommission blocked, holding deletion", "name", broker.Name)
				return ctrl.Result{RequeueAfter: periodicRequeue}, nil
			}
			if result.requeue {
				return ctrl.Result{RequeueAfter: requeueDecommission}, nil
			}
		}
		l.Info("deleting pod after decommission", "name", podName)
		if err := k8sClient.Delete(ctx, &pod); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

	default:
		// Deletion WITHOUT intent: never decommission (RFC Q2).
		if r.deletionPolicy(ctx, l, k8sClient, broker) == deletionPolicyCascade {
			// Owning cluster is being torn down with the default cascade
			// policy: leave pod and PVCs to the GC (owned by this CR).
			l.Info("cluster teardown with cascade policy, removing finalizer", "name", broker.Name)
		} else {
			if err := r.releaseBrokerResources(ctx, l, k8sClient, broker, &pod); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	controllerutil.RemoveFinalizer(broker, brokerFinalizerName)
	if err := k8sClient.Update(ctx, broker); err != nil {
		return ctrl.Result{}, err
	}
	l.Info("removed finalizer, Broker CR will be deleted")
	return ctrl.Result{}, nil
}

type brokerDeletionPolicy string

const (
	deletionPolicyCascade brokerDeletionPolicy = "cascade"
	deletionPolicyOrphan  brokerDeletionPolicy = "orphan"
)

// ownerTearingDown reports whether the Broker's controller owner (its
// cluster) is being deleted or is already gone. Uncertainty reads as false:
// wrongly assuming teardown could destroy data (cascade policy) or skip a
// legitimate decommission.
func (r *BrokerReconciler) ownerTearingDown(ctx context.Context, l logr.Logger, k8sClient client.Client, broker *redpandav1alpha2.Broker) bool {
	owner := metav1.GetControllerOf(broker)
	if owner == nil {
		return false
	}

	var ownerObj client.Object
	switch {
	case owner.Kind == "Cluster" && strings.HasPrefix(owner.APIVersion, "redpanda.vectorized.io/"):
		ownerObj = &vectorizedv1alpha1.Cluster{}
	case owner.Kind == "Redpanda" && strings.HasPrefix(owner.APIVersion, "cluster.redpanda.com/"):
		ownerObj = &redpandav1alpha2.Redpanda{}
	default:
		return false
	}

	err := k8sClient.Get(ctx, client.ObjectKey{Name: owner.Name, Namespace: broker.Namespace}, ownerObj)
	switch {
	case err == nil:
		return !ownerObj.GetDeletionTimestamp().IsZero()
	case apierrors.IsNotFound(err):
		return true
	default:
		l.Info("could not determine owner state, assuming it is alive", "owner", owner.Name, "error", err)
		return false
	}
}

// deletionPolicy decides what happens to the pod and PVCs when a Broker CR is
// deleted. While the owning cluster is alive the answer is always orphan
// (release + self-heal); during cluster teardown the policy propagated onto
// the Broker (feature.BrokerDeletionPolicy, default cascade) decides. On any
// uncertainty the answer is orphan — releasing a pod is recoverable,
// destroying its data is not.
func (r *BrokerReconciler) deletionPolicy(ctx context.Context, l logr.Logger, k8sClient client.Client, broker *redpandav1alpha2.Broker) brokerDeletionPolicy {
	if !r.ownerTearingDown(ctx, l, k8sClient, broker) {
		return deletionPolicyOrphan
	}

	if feature.BrokerDeletionPolicy.Get(ctx, broker) == string(deletionPolicyOrphan) {
		return deletionPolicyOrphan
	}
	return deletionPolicyCascade
}

// releaseBrokerResources strips this Broker's ownerRefs from its pod and PVCs
// so they survive the CR deletion. pod may be nil when it is already gone —
// the PVCs are still released.
func (r *BrokerReconciler) releaseBrokerResources(ctx context.Context, l logr.Logger, k8sClient client.Client, broker *redpandav1alpha2.Broker, pod *corev1.Pod) error {
	if pod != nil && removeOwnerRefByUID(pod, broker.UID) {
		l.Info("releasing pod from deleted Broker CR", "pod", pod.Name)
		if err := k8sClient.Update(ctx, pod); err != nil {
			return fmt.Errorf("releasing pod %s: %w", pod.Name, err)
		}
	}

	var pvcNames []string
	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		pvcNames = append(pvcNames, fmt.Sprintf("%s-%s", vct.Name, broker.PodName()))
	}
	for _, ec := range broker.Spec.Storage.ExistingClaims {
		pvcNames = append(pvcNames, ec.Name)
	}
	for _, name := range pvcNames {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: broker.Namespace}, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if removeOwnerRefByUID(&pvc, broker.UID) {
			l.Info("releasing PVC from deleted Broker CR", "pvc", name)
			if err := k8sClient.Update(ctx, &pvc); err != nil {
				return fmt.Errorf("releasing PVC %s: %w", name, err)
			}
		}
	}
	return nil
}

func removeOwnerRefByUID(obj client.Object, uid types.UID) bool {
	refs := obj.GetOwnerReferences()
	for i, ref := range refs {
		if ref.UID == uid {
			obj.SetOwnerReferences(append(refs[:i], refs[i+1:]...))
			return true
		}
	}
	return false
}

// ensureDrained enables maintenance mode and returns true when leadership drain
// is complete. Callers should requeue until this returns true.
func (r *BrokerReconciler) ensureDrained(ctx context.Context, clusterName string, broker *redpandav1alpha2.Broker) (bool, error) {
	l := log.FromContext(ctx)
	brokerID := int(*broker.Status.BrokerID)

	admin, err := r.ClientFactory.RedpandaAdminClientForCluster(ctx, broker, clusterName)
	if err != nil {
		return false, err
	}
	defer admin.Close()

	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return false, err
	}
	for _, b := range brokers {
		if b.NodeID != brokerID {
			continue
		}
		if b.Maintenance != nil && b.Maintenance.Finished != nil && *b.Maintenance.Finished {
			l.Info("leadership drain complete", "brokerID", brokerID)
			return true, nil
		}
		if b.Maintenance != nil && b.Maintenance.Draining {
			l.Info("leadership drain in progress", "brokerID", brokerID,
				"partitions", b.Maintenance.Partitions, "transferring", b.Maintenance.Transferring)
			return false, nil
		}
		// Not yet in maintenance mode — enable it.
		if err := admin.EnableMaintenanceMode(ctx, brokerID); err != nil {
			return false, err
		}
		l.Info("enabled maintenance mode", "brokerID", brokerID)
		return false, nil
	}
	// Broker not found in admin API — skip drain, let rotation proceed.
	return true, nil
}

func (r *BrokerReconciler) disableMaintenanceMode(ctx context.Context, clusterName string, broker *redpandav1alpha2.Broker) error {
	admin, err := r.ClientFactory.RedpandaAdminClientForCluster(ctx, broker, clusterName)
	if err != nil {
		return err
	}
	defer admin.Close()
	return admin.DisableMaintenanceMode(ctx, int(*broker.Status.BrokerID), false)
}

// resolveBroker finds the cluster-membership entry backing this Broker's
// pod. found reports whether an authoritative entry exists — false is the
// COMMON case while a broker is still joining, so callers must branch on it
// explicitly rather than relying on error or nilness.
//
// Matching handles every advertised-address form seen in the wild: per-pod
// FQDN (matched by first DNS label), "host:port", bare host, and — when the
// pod is known — a bare pod IP. More than one distinct match is ambiguous
// and reported as not found rather than guessed at: right after a
// decommission the membership list can briefly retain the dead
// predecessor's entry under the SAME pod name (a replacement reuses it).
// Callers that ADOPT the resolved id must additionally check the entry is
// an active, alive member — see reconcileBrokerRegistration.
func (r *BrokerReconciler) resolveBroker(ctx context.Context, clusterName string, broker *redpandav1alpha2.Broker, pod *corev1.Pod, podName string) (resolved *rpadmin.Broker, found bool, err error) {
	admin, err := r.ClientFactory.RedpandaAdminClientForCluster(ctx, broker, clusterName)
	if err != nil {
		return nil, false, err
	}
	defer admin.Close()

	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return nil, false, err
	}

	var matches []rpadmin.Broker
	for _, b := range brokers {
		host := b.InternalRPCAddress
		if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = h
		}
		if host == "" {
			continue
		}
		if strings.SplitN(host, ".", 2)[0] == podName || host == podName ||
			(pod != nil && pod.Status.PodIP != "" && host == pod.Status.PodIP) {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return &matches[0], true, nil
	default:
		// A dead predecessor's entry (PV remediation, broker replacement)
		// lingers under the SAME reused pod name until it is ghost-
		// decommissioned, so plain ambiguity-refusal would wedge recovery
		// forever. At most one LIVE process can advertise the address at a
		// time — a unique active+alive match is authoritative.
		var live []rpadmin.Broker
		for _, b := range matches {
			if brokerActiveAndAlive(&b) {
				live = append(live, b)
			}
		}
		if len(live) == 1 {
			return &live[0], true, nil
		}
		log.FromContext(ctx).Info("ambiguous cluster-membership match for pod, refusing to guess",
			"pod", podName, "matches", len(matches), "alive", len(live))
		return nil, false, nil
	}
}

// brokerActiveAndAlive reports whether a membership entry is safe to adopt
// as this Broker's identity: an entry that is draining, removed, or not
// alive is either a leftover of a decommissioned predecessor or a node that
// has not finished joining.
func brokerActiveAndAlive(b *rpadmin.Broker) bool {
	return b != nil && b.MembershipStatus == rpadmin.MembershipStatusActive && b.IsAlive != nil && *b.IsAlive
}

// hasValidRollGrant returns true if the Broker CR carries a roll-grant
// annotation whose checksum portion matches the Broker's desired pod
// template hash (the rotation identity the cluster controller keys grants
// on) and whose deadline has not passed.
func hasValidRollGrant(ctx context.Context, broker *redpandav1alpha2.Broker) bool {
	grant := feature.RollGrant.Get(ctx, broker)
	if grant == "" {
		return false
	}
	grantChecksum, deadline, ok := feature.ParseRollGrant(grant)
	if !ok {
		return false
	}
	if grantChecksum != broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] {
		return false
	}
	return time.Now().Before(deadline)
}

// podStuckReason reports the container waiting reason when a pod is wedged
// in a state kubelet will not recover on its own (crash-looping, unable to
// pull its image, or failing container creation).
func podStuckReason(pod *corev1.Pod) string {
	statuses := make([]corev1.ContainerStatus, 0, len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull",
				"InvalidImageName", "CreateContainerConfigError", "CreateContainerError":
				return w.Reason
			}
		}
	}
	return ""
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
