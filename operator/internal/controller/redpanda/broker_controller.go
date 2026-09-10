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

	"github.com/cockroachdb/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
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

const defaultMarkDiskLostAfter = 5 * time.Minute

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
	Manager           multicluster.Manager
	ClientFactory     internalclient.ClientFactory
	MarkDiskLostAfter time.Duration
}

func SetupBrokerController(ctx context.Context, mgr multicluster.Manager, clientFactory internalclient.ClientFactory, namespace string, markDiskLostAfter time.Duration) error {
	if markDiskLostAfter <= 0 {
		markDiskLostAfter = defaultMarkDiskLostAfter
	}
	for _, clusterName := range mgr.GetClusterNames() {
		cl, err := mgr.GetCluster(ctx, clusterName)
		if err != nil {
			return err
		}
		if err := cl.GetFieldIndexer().IndexField(ctx, &redpandav1alpha2.Broker{}, brokerPodNameIndex, indexBrokerByPodName); err != nil {
			return err
		}
	}
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		SkipNameValidation: ptr.To(true),
	}).For(
		&redpandav1alpha2.Broker{},
		mcbuilder.WithEngageWithLocalCluster(true),
		mcbuilder.WithEngageWithProviderClusters(true),
	).
		Owns(&corev1.Pod{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Watches(&corev1.Pod{}, enqueueBrokerForAdoptablePod,
			mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Complete(
			controller.FilterNamespaceReconciler(
				namespace,
				observability.Wrap[mcreconcile.Request](&BrokerReconciler{
					Manager:           mgr,
					ClientFactory:     clientFactory,
					MarkDiskLostAfter: markDiskLostAfter,
				}, "Broker", periodicRequeue)))
}

// brokerPodNameIndex indexes Broker CRs by their deterministic pod name.
const brokerPodNameIndex = "__broker_pod_name"

func indexBrokerByPodName(o client.Object) []string {
	b := o.(*redpandav1alpha2.Broker)
	if b.Spec.NetworkIndex == nil {
		return nil
	}
	return []string{b.PodName()}
}

// enqueueBrokerForAdoptablePod enqueues pods with no owner and a name that matches the Broker's pod name format.
func enqueueBrokerForAdoptablePod(clusterName string, cl cluster.Cluster) mchandler.EventHandler {
	return handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []mcreconcile.Request {
		if metav1.GetControllerOf(o) != nil {
			return nil
		}
		var brokers redpandav1alpha2.BrokerList
		if err := cl.GetClient().List(ctx, &brokers,
			client.InNamespace(o.GetNamespace()),
			client.MatchingFields{brokerPodNameIndex: o.GetName()},
		); err != nil {
			return nil
		}
		requests := make([]mcreconcile.Request, 0, len(brokers.Items))
		for i := range brokers.Items {
			requests = append(requests, mcreconcile.Request{
				Request:     reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&brokers.Items[i])},
				ClusterName: clusterName,
			})
		}
		return requests
	})
}

type brokerReconciliationState struct {
	broker *redpandav1alpha2.Broker
	pod    *corev1.Pod // nil when pod does not exist yet
	// pass-start snapshot; nil = did not exist when fetched.
	// brokerPVCs[i] corresponds to Spec.Storage.VolumeClaimTemplates[i],
	// brokerExistingPVCs[i] to Spec.Storage.ExistingClaims[i].
	brokerPVCs         []*corev1.PersistentVolumeClaim
	brokerExistingPVCs []*corev1.PersistentVolumeClaim

	phase       redpandav1alpha2.BrokerPhase // empty = compute from pod status
	granted     bool
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

// brokerPVCs returns all PVCs for given broker (.brokerPVCs + .brokerExistingPVCs combined)
func (s *brokerReconciliationState) allBrokerPVCs() []*corev1.PersistentVolumeClaim {
	return slices.Concat(s.brokerPVCs, s.brokerExistingPVCs)
}

type brokerReconcilerFn func(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error)

func (r *BrokerReconciler) fetchState(ctx context.Context, req mcreconcile.Request, k8sClient client.Client, broker *redpandav1alpha2.Broker) (*brokerReconciliationState, error) {
	state := &brokerReconciliationState{
		broker:        broker,
		granted:       broker.HasValidRollGrant(),
		initialStatus: broker.Status.DeepCopy(),
		clusterName:   req.ClusterName,
	}

	// fetch PVCs
	podName := broker.PodName()

	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		pvcName := fmt.Sprintf("%s-%s", vct.Name, podName)
		var pvc corev1.PersistentVolumeClaim
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: broker.Namespace, Name: pvcName}, &pvc)
		if client.IgnoreNotFound(err) != nil {
			return nil, errors.Wrapf(err, "cannot fetch PVC %s for broker %s", pvcName, broker.Name)
		}
		if apierrors.IsNotFound(err) {
			// we need to explicitly append nil, because in the IsNotFound case
			// pvc becomes an empty corev1.PersistentVolumeClaim
			state.brokerPVCs = append(state.brokerPVCs, nil)
			continue
		}
		state.brokerPVCs = append(state.brokerPVCs, &pvc)
	}

	for _, vct := range broker.Spec.Storage.ExistingClaims {
		var pvc corev1.PersistentVolumeClaim
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: broker.Namespace, Name: vct.Name}, &pvc)
		if client.IgnoreNotFound(err) != nil {
			return nil, errors.Wrapf(err, "cannot fetch PVC %s for broker %s", vct.Name, broker.Name)
		}
		if apierrors.IsNotFound(err) {
			// we need to explicitly append nil, because in the IsNotFound case
			// pvc becomes an empty corev1.PersistentVolumeClaim
			state.brokerExistingPVCs = append(state.brokerExistingPVCs, nil)
			continue
		}
		state.brokerExistingPVCs = append(state.brokerExistingPVCs, &pvc)
	}

	var pod corev1.Pod
	err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, &pod)
	switch {
	case apierrors.IsNotFound(err):
		return state, nil
	case err != nil:
		return nil, errors.Wrapf(err, "cannot get pod for broker %s", broker.Name)
	default:
		state.pod = &pod
		if isOwnedByDifferentBroker(state.pod, broker) {
			// this can happen only when we're reconciling Broker TombStone.
			// then, there's a chance the pod is already a new pod
			// for a new Broker which was provisioned to replace dead Broker.
			state.pod = nil
		}
	}

	return state, nil
}

func isOwnedByDifferentBroker(pod *corev1.Pod, broker *redpandav1alpha2.Broker) bool {
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return false
	}
	return owner.Kind == redpandav1alpha2.BrokerKind &&
		owner.APIVersion == redpandav1alpha2.GroupVersion.String() &&
		owner.UID != broker.UID
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

	state, err := r.fetchState(ctx, req, k8sClient, &broker)
	if err != nil {
		return ctrl.Result{}, err
	}

	reconcilers := []brokerReconcilerFn{
		r.reconcileDiskLost,
		r.reconcilePVCs,
		r.reconcilePod,
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
	podName := broker.PodName()

	for i, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		// check if it exists
		pvc := state.brokerPVCs[i]
		if pvc != nil && pvc.DeletionTimestamp.IsZero() {
			continue
		}
		if pvc != nil && !pvc.DeletionTimestamp.IsZero() {
			// A PVC mid-deletion (e.g. PV-affinity remediation) must be fully
			// gone before recreating it or the pod: pvc-protection releases a
			// Terminating PVC only once no pod references it, so recreating
			// the pod first pins the old PVC forever and the pod never
			// schedules.
			l.Info("waiting for PVC deletion to complete", "pvc", pvc.Name)
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		// does not exist, create
		pvcName := fmt.Sprintf("%s-%s", vct.Name, podName)
		pvc = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: broker.Namespace,
				Labels:    broker.Spec.PodTemplate.Labels,
				// thing to consider: maybe we should add a set of labels that will identify the Broker
				// this way we can then simplify querying for PVC "belonging" to given broker.
			},
			Spec: vct.Spec,
		}
		l.Info("creating PVC", "name", pvcName)
		if err := k8sClient.Create(ctx, pvc); err != nil {
			return ctrl.Result{}, err
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
		if adoptionBarredByRollback(ctx, k8sClient, broker) {
			l.Info("owning cluster left broker mode; not creating a pod", "name", podName)
			state.phase = redpandav1alpha2.BrokerPhasePending
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		l.Info("creating pod (no existing pod found)", "name", podName)
		newPod := broker.BuildPod(podName)
		if err := controllerutil.SetControllerReference(broker, newPod, scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := k8sClient.Create(ctx, newPod); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}

	pod := state.pod
	ownerRef := metav1.GetControllerOf(pod)
	if ownerRef != nil && !metav1.IsControlledBy(pod, broker) {
		l.Info("pod owned by another controller (shadow mode)", "owner", ownerRef.Kind+"/"+ownerRef.Name)
		state.phase = redpandav1alpha2.BrokerPhasePending
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}
	if ownerRef == nil {
		if adoptionBarredByRollback(ctx, k8sClient, broker) {
			l.Info("owning cluster left broker mode; leaving orphaned pod for the StatefulSet to adopt", "name", podName)
			state.phase = redpandav1alpha2.BrokerPhasePending
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		l.Info("adopting orphaned pod", "name", podName)
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
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}

	// Owned pods may lack rotation keys that post-date their creation: the
	// pod-template hash on pods from before hash tracking existed (operator
	// upgrade), or the cluster-config version on pods born before the first
	// MarkForRestart stamp (bootstrap, or a pod created off a cache-lagged
	// unstamped template). Treating a MISSING key as outdated would roll the
	// whole fleet for no actual change — backfill the desired value instead:
	// drift predating the key is undetectable either way, and every future
	// change rotates through the freshly stamped value. A pod carrying a
	// DIFFERENT value is a genuine pending rotation and is never touched.
	if backfillRotationKeys(broker, pod) {
		l.Info("backfilling missing rotation keys on pod", "name", podName)
		if err := k8sClient.Update(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}

	return ctrl.Result{}, nil
}

func adoptionBarredByRollback(ctx context.Context, k8sClient client.Client, broker *redpandav1alpha2.Broker) bool {
	owner, found, err := getBrokerOwner(ctx, k8sClient, broker)
	if err != nil {
		return true
	}
	if !found {
		// owner not set / set to a different kind than Redpanda / Cluster
		return false
	}
	switch o := owner.(type) {
	case *redpandav1alpha2.Redpanda:
		return !feature.V2UseBrokerCR.Get(ctx, o)
	case *vectorizedv1alpha1.Cluster:
		return !feature.V1UseBrokerCR.Get(ctx, o)
	}
	return false
}

func backfillRotationKeys(broker *redpandav1alpha2.Broker, pod *corev1.Pod) bool {
	stamped := false
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	for _, key := range redpandav1alpha2.RotationAnnotations {
		if _, ok := pod.Annotations[key]; ok {
			continue
		}
		if desired := broker.Spec.PodTemplate.Annotations[key]; desired != "" {
			pod.Annotations[key] = desired
			stamped = true
		}
	}
	return stamped
}

func (r *BrokerReconciler) reconcileDiskLost(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	broker := state.broker

	if !broker.IsDiskLost() {
		return r.detectDiskLost(ctx, l, state, cluster)
	}

	if broker.Spec.Decommission {
		if broker.Status.BrokerID == nil {
			l.Info("DiskLost tombstone has no recorded node_id; nothing to decommission")
			state.phase = redpandav1alpha2.BrokerPhaseDecommissioned
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		return ctrl.Result{}, nil
	}

	return r.dismantleDiskLost(ctx, l, state, cluster)
}

func (r *BrokerReconciler) detectDiskLost(ctx context.Context, l logr.Logger, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker

	if broker.Spec.Decommission ||
		state.pod == nil ||
		!metav1.IsControlledBy(state.pod, broker) ||
		!pvcunbinder.PodHasVolumeAffinityUnschedulable(state.pod) {
		return ctrl.Result{}, nil
	}

	// Timeout: only accept the proof after the pod has been stuck long
	// enough for a rebooting node to have come back.
	for _, cond := range state.pod.Status.Conditions {
		if cond.Type != corev1.PodScheduled || cond.Status != corev1.ConditionFalse {
			continue
		}
		if delta := r.MarkDiskLostAfter - time.Since(cond.LastTransitionTime.Time); delta > 0 {
			l.Info("pod stuck on PV node affinity but disk-loss timeout not reached", "remaining", delta.Round(time.Second))
			return ctrl.Result{RequeueAfter: delta}, nil
		}
	}

	lost, err := pvcunbinder.LostDiskClaims(ctx, cluster.GetAPIReader(), state.pod)
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

func (r *BrokerReconciler) dismantleDiskLost(ctx context.Context, l logr.Logger, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	state.phase = redpandav1alpha2.BrokerPhaseDiskLost

	if broker.DiskLostReleased() {
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

	k8sClient := cluster.GetClient()
	podName := broker.PodName()
	if state.pod != nil {
		podName = state.pod.Name
		l.Info("disk-lost dismantle: deleting pod", "pod", state.pod.Name)
		if err := k8sClient.Delete(ctx, state.pod); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	err := r.deleteBrokerPVCs(ctx, l, k8sClient, broker)
	if err != nil {
		return ctrl.Result{}, err
	}

	// make sure pod and pvcs are released, otherwise requeue
	var pod corev1.Pod
	err = k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, &pod)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if apierrors.IsNotFound(err) {
		// check PVCs
		for _, name := range broker.ClaimNames() {
			var pvc corev1.PersistentVolumeClaim
			err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: broker.Namespace}, &pvc)
			switch {
			case err == nil:
				if owner := metav1.GetControllerOf(&pvc); owner == nil || metav1.IsControlledBy(&pvc, broker) {
					l.Info("pvc still terminating", "pvc", name)
					return ctrl.Result{RequeueAfter: requeueShort}, nil // still terminating, requeue
				}
			case !apierrors.IsNotFound(err):
				return ctrl.Result{}, err
			}
		}
		// we're done
		l.Info("disk-lost dismantle complete: pod and PVCs gone, network index released")
		broker.Status.DiskLost.ResourcesReleased = true
		return ctrl.Result{RequeueAfter: requeueShort}, nil

	}
	l.Info("pod still not terminated", "pod", pod.Name)
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
	if !broker.PodOutdated(state.pod) {
		return ctrl.Result{}, nil
	}

	l := log.FromContext(ctx)
	if !state.granted {
		l.Info("pod needs rotation but no roll-grant", "name", state.pod.Name)
		if broker.Status.BrokerID != nil {
			if err := r.disableMaintenanceMode(ctx, state.clusterName, broker); err != nil {
				l.V(1).Info("could not disable maintenance mode while parked without a grant", "error", err)
			}
		}
		state.phase = redpandav1alpha2.BrokerPhaseRunning
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

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
	return ctrl.Result{RequeueAfter: requeueShort}, nil
}

func (r *BrokerReconciler) reconcilePodMetadata(ctx context.Context, state *brokerReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pod == nil || state.broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
	pod := state.pod
	tpl := state.broker.Spec.PodTemplate

	patch := client.MergeFrom(pod.DeepCopy())
	changed := false
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	for k, v := range tpl.Annotations {
		if slices.Contains(redpandav1alpha2.RotationAnnotations, k) {
			continue
		}
		if pod.Annotations[k] != v {
			pod.Annotations[k] = v
			changed = true
		}
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	for k, v := range tpl.Labels {
		if pod.Labels[k] != v {
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

func (r *BrokerReconciler) reconcileBrokerRegistration(ctx context.Context, state *brokerReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	broker := state.broker
	if broker.Spec.Decommission {
		return ctrl.Result{}, nil
	}
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
		if !brokerActiveAndAlive(resolved) {
			l.Info("matched membership entry not active/alive yet, deferring identity adoption",
				"nodeID", resolved.NodeID, "membership", resolved.MembershipStatus)
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}
		broker.Status.BrokerID = currentID
	}
	if *currentID != *broker.Status.BrokerID {
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
		broker.Status.BrokerID = nil
		l := log.FromContext(ctx)
		k8sClient := cluster.GetClient()
		podName := broker.PodName()

		if state.pod != nil && metav1.IsControlledBy(state.pod, broker) {
			l.Info("deleting pod after decommission", "name", podName)
			if err := k8sClient.Delete(ctx, state.pod); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		if broker.IsDiskLost() {
			return ctrl.Result{}, nil
		}
		if err := r.deleteBrokerPVCs(ctx, l, k8sClient, broker); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

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
	for _, pvc := range state.allBrokerPVCs() {
		if pvc == nil || pvc.Status.Phase != corev1.ClaimBound {
			allBound = false
			break
		}
	}
	if allBound {
		status.SetStorageBound(statuses.BrokerStorageBoundReasonBound)
	} else {
		status.SetStorageBound(statuses.BrokerStorageBoundReasonPending, "One or more PVCs are not bound")
	}

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

	// Last-broker guard.
	if len(brokers) <= 1 {
		l.Info("blocking decommission: last broker in cluster", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseStuck}, nil
	}

	status, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		// HITL: This is a potential footgun. Any time Redpanda error message changes, this no longer works
		// and we don't know about it. How did you come up with this "is not decommissioning"?
		// isn't there a better way to check it?
		// Isn't it better to just throw an error, if admin.DecommissionBrokerStatus returns an error?
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

func (r *BrokerReconciler) reconcileDelete(ctx context.Context, l logr.Logger, k8sClient client.Client, clusterName string, broker *redpandav1alpha2.Broker, podName string) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(broker, brokerFinalizerName) {
		return ctrl.Result{}, nil
	}

	ownerTearingDown := r.ownerTearingDown(ctx, l, k8sClient, broker)
	switch {
	case ownerTearingDown:
		if broker.GetBrokerDeletionPolicy() == redpandav1alpha2.BrokerDeletionPolicyOrphan {
			l.Info("cluster teardown with orphan policy: keeping broker's PVCs", "name", broker.Name)
		} else {
			l.Info("cluster teardown with cascade policy: deleting PVCs, pod is left to the GC", "name", broker.Name)
			if !broker.IsDiskLost() {
				err := r.deleteBrokerPVCs(ctx, l, k8sClient, broker)
				if err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	case broker.Spec.Decommission:
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, pod)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if apierrors.IsNotFound(err) {
			pod = nil
		}

		if broker.Status.BrokerID == nil {
			resolved, found, err := r.resolveBroker(ctx, clusterName, broker, pod, podName)
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

		if pod != nil && metav1.IsControlledBy(pod, broker) {
			l.Info("deleting pod after decommission", "name", podName)
			if err := k8sClient.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}

		if !broker.IsDiskLost() {
			if err := r.deleteBrokerPVCs(ctx, l, k8sClient, broker); err != nil {
				return ctrl.Result{}, err
			}
		}
	default:
		l.Info("releasing pod from deleted Broker CR", "podName", podName, "brokerName", broker.Name)
		if err := stripPodOwnerRef(ctx, k8sClient, broker, podName); err != nil {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(broker, brokerFinalizerName)
	if err := k8sClient.Update(ctx, broker); err != nil {
		return ctrl.Result{}, err
	}
	l.Info("removed finalizer, Broker CR will be deleted")
	return ctrl.Result{}, nil
}

func (r *BrokerReconciler) deleteBrokerPVCs(ctx context.Context, l logr.Logger, k8sClient client.Client, broker *redpandav1alpha2.Broker) error {
	for _, name := range broker.ClaimNames() {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: broker.Namespace}, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if metav1.GetControllerOf(&pvc) != nil {
			l.Info("skipping PVC controlled by another object", "pvc", name, "owner", metav1.GetControllerOf(&pvc).String())
			continue
		}
		l.Info("deleting PVC", "name", name)
		if err := k8sClient.Delete(ctx, &pvc); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func stripPodOwnerRef(ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker, podName string) error {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: broker.Namespace}}
	patch := []byte(fmt.Sprintf(`{"metadata":{"ownerReferences":[{"$patch":"delete","uid":"%s"}]}}`, broker.UID))
	if err := c.Patch(ctx, pod, client.RawPatch(types.StrategicMergePatchType, patch)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func getBrokerOwner(ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker) (owner client.Object, found bool, err error) {
	ref := metav1.GetControllerOf(broker)
	if ref == nil {
		return nil, false, nil
	}
	switch {
	case ref.Kind == vectorizedv1alpha1.ClusterKind && strings.HasPrefix(ref.APIVersion, vectorizedv1alpha1.GroupVersion.Group):
		owner = &vectorizedv1alpha1.Cluster{}
	case ref.Kind == redpandav1alpha2.RedpandaKind && strings.HasPrefix(ref.APIVersion, redpandav1alpha2.GroupVersion.Group):
		owner = &redpandav1alpha2.Redpanda{}
	default:
		return nil, false, nil
	}
	if err := c.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: broker.Namespace}, owner); err != nil {
		return nil, false, err
	}
	return owner, true, nil
}

func (r *BrokerReconciler) ownerTearingDown(ctx context.Context, l logr.Logger, k8sClient client.Client, broker *redpandav1alpha2.Broker) bool {
	owner, found, err := getBrokerOwner(ctx, k8sClient, broker)
	switch {
	case apierrors.IsNotFound(err):
		// owner already deleted
		return true
	case err != nil:
		l.Info("could not determine owner state, assuming it is alive", "error", err)
		return false
	case !found:
		// owner not set in OwnerReferences / set to other kind than Redpanda / Cluster.
		return false
	default:
		return !owner.GetDeletionTimestamp().IsZero()
	}
}

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

func brokerActiveAndAlive(b *rpadmin.Broker) bool {
	return b != nil && b.MembershipStatus == rpadmin.MembershipStatusActive && b.IsAlive != nil && *b.IsAlive
}

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
