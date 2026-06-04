// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pvcunbinder

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	operatorlabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

var schedulingFailureRE = regexp.MustCompile(`(^0/[1-9]\d* nodes are available)|(volume node affinity)`)

// PauseAnnotation - when present and set to "true" on the parent
// Redpanda or Cluster CR, instructs the PVCUnbinder to skip all
// reconcile work for any Pod that belongs to that cluster. Used by the
// cloud control plane (and operators in general) to pause unbinder
// activity around planned events like K8s cluster upgrades, node-pool
// surges, or maintenance windows where transient multi-node disruption
// is expected.
const PauseAnnotation = "operator.redpanda.com/pause-pvc-unbinder"

// requeueDuringDisruption is how long to wait before re-checking when
// we've decided to skip an unbind action (paused via annotation,
// concurrent K8s-wide disruption detected, or another sibling unbind
// in flight).
const requeueDuringDisruption = 30 * time.Second

// Gate names used as label values on the
// `operator_controller_pvc_unbinder_gate_deferred_total` metric and as
// the Event Reason recorded on the Pod whose remediation was deferred.
// Keep this list closed — these are the only allowed values.
const (
	gateInFlight     = "in-flight"
	gatePause        = "pause"
	gateMultiNode    = "multi-node"
	gatePVCRebinding = "pvc-rebinding"
)

// eventReasonGateDeferred is the EventReason emitted on the Pod when a
// safety gate defers remediation. Operators watching for "why isn't
// the PVCUnbinder acting on my stuck pod" can `kubectl describe pod`
// and see this reason + the gate label.
const eventReasonGateDeferred = "PVCUnbinderDeferred"

// Gate 2 identifies "Redpanda broker pod" via two label sets, because
// the three cluster types and the direct-Helm install don't share a
// single label that uniquely marks operator-managed Redpanda:
//
//   - v1 Cluster (operator): `app.kubernetes.io/managed-by=redpanda-operator`
//     (hardcoded at operator/pkg/labels/labels.go).
//   - StretchCluster: same — multicluster render overrides the chart's
//     managed-by to `redpanda-operator`.
//   - v2 Redpanda (operator → chart): the chart writes
//     `app.kubernetes.io/managed-by=Helm`, but the operator stamps
//     `cluster.redpanda.com/operator=v2`.
//   - Direct Helm install (no operator): no operator labels at all,
//     and not considered a target of Gate 2.
//
// Filtering on `app.kubernetes.io/name=redpanda` would catch all of
// these by default but breaks for users running with `nameOverride`
// (which is a supported customization in production). So Gate 2 does
// two LIST queries and unions the results by (namespace, name).
const (
	managedByLabelValue            = "redpanda-operator"
	clusterRedpandaOperatorLabel   = "cluster.redpanda.com/operator"
	clusterRedpandaOperatorV2Value = "v2"
)

// Controller is a Kubernetes Controller that watches for Pods stuck in a
// Pending state due to volume affinities and attempts a remediation.
//
// It watches for Pod events rather than Node events because:
//  1. Node Deletion events could be missed if the operator is scheduled on the node that's died
//  2. We don't want to re-implement label matching. In theory, it should be
//     easy but it's quite risky and behaviors could diverge between Kubernetes
//     versions.
//
// To get the Pod to reschedule we:
//  1. Find all PVs and PVCs associated with our Pod.
//  2. Ensure that all PVs in question have a Retain policy
//  3. Delete all PVCs from step 1. (PVCs are immutable after creation,
//     deletion is the only option)
//  4. (Optionally) "Recycle" all PVs from step 1 by clearing the ClaimRef.
//     Kubernetes will only consider binding PVs that have a satisfiable
//     NodeAffinity. By "recycling" we permit Flakey Nodes to rejoin the cluster
//     which _might_ reclaim the now freed volume.
//  5. Deleting the Pod to re-trigger PVC creation and rebinding.
type Controller struct {
	Client client.Client
	// Timeout is the duration a Pod must be stuck in Pending before
	// remediation is attempted.
	Timeout time.Duration
	// Selector, if specified, will narrow the scope of Pods that this
	// Reconciler will consider for remediation.
	Selector labels.Selector
	// AllowRebinding optionally enables clearing of the unbound PV's ClaimRef
	// which effectively makes the PVs "re-bindable" if the underlying Node
	// become capable of scheduling Pods once again.
	// NOTE: This option can present problems when a Node's name is reused and
	// using HostPath volumes and LocalPathProvisioner. In such a case, the
	// helper Pod of LocalPathProvisioner will NOT run a second time as the
	// Volume is assumed to exist. This can lead to Permission errors or
	// referencing a directory that does not exist.
	AllowRebinding bool
	// Tracker is the per-cluster mutex used to bridge the race window
	// between deleting a Pod's PVCs and the StatefulSet controller
	// recreating them. SetupWithManager allocates a fresh one if nil;
	// MulticlusterController shares its tracker across all per-cluster
	// Reconciles by passing a pointer into the per-request Controller.
	Tracker *InFlightTracker
	// ClusterName disambiguates tracker keys when the Tracker is shared
	// across K8s clusters in multicluster mode. Empty for single-cluster
	// operation.
	ClusterName string
	// Recorder, if non-nil, receives an Event on the Pod every time a
	// safety gate defers remediation. Nil-safe — if unset, only the
	// metric is incremented. Uses the new k8s.io/client-go/tools/events
	// API rather than the deprecated tools/record API.
	Recorder events.EventRecorder
}

// MulticlusterController is a multicluster-aware version of Controller that
// watches Pods across all clusters managed by a multicluster.Manager.
type MulticlusterController struct {
	Manager        multicluster.Manager
	Timeout        time.Duration
	Selector       labels.Selector
	AllowRebinding bool
	// Tracker is shared across all per-cluster Reconciles spawned by
	// this MulticlusterController. SetupWithMultiClusterManager
	// allocates a fresh one if nil.
	Tracker *InFlightTracker
}

// recordGateDeferred increments the gate-defer metric and emits a
// Kubernetes Event on the Pod whose reconcile got deferred. The metric
// path always runs (operators rely on it to alert on silent inaction);
// the Event is skipped when Recorder is nil (the test path).
//
// `action` is the new-events-API verb describing what the unbinder
// just did ("Defer"); `gate` is included in the message so operators
// can tell which gate fired from `kubectl describe pod`.
func (r *Controller) recordGateDeferred(pod *corev1.Pod, gate, message string) {
	observability.PVCUnbinderGateDeferred.WithLabelValues(gate).Inc()
	if r.Recorder != nil && pod != nil {
		r.Recorder.Eventf(pod, nil, corev1.EventTypeNormal, eventReasonGateDeferred, "Defer", "gate=%s: %s", gate, message)
	}
}

func (r *MulticlusterController) SetupWithMultiClusterManager() error {
	if r.Tracker == nil {
		r.Tracker = NewInFlightTracker(DefaultTrackerTTL)
	}
	selectorPredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		if r.Selector == nil {
			return true
		}
		lbls := object.GetLabels()
		if lbls == nil {
			lbls = map[string]string{}
		}
		return r.Selector.Matches(labels.Set(lbls))
	})
	unbinderPredicate := predicate.NewPredicateFuncs(pvcUnbinderPredicate)

	return mcbuilder.ControllerManagedBy(r.Manager).
		For(
			&corev1.Pod{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		WithEventFilter(selectorPredicate).
		WithEventFilter(unbinderPredicate).
		Complete(r)
}

func (r *MulticlusterController) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	k8sCluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		log.FromContext(ctx).Error(err, "unable to fetch cluster, skipping reconciliation", "cluster", req.ClusterName)
		return ctrl.Result{}, nil
	}

	c := &Controller{
		Client:         k8sCluster.GetClient(),
		Timeout:        r.Timeout,
		Selector:       r.Selector,
		AllowRebinding: r.AllowRebinding,
		Tracker:        r.Tracker,
		ClusterName:    req.ClusterName,
		Recorder:       k8sCluster.GetEventRecorder("pvc-unbinder"),
	}
	return c.Reconcile(ctx, req.Request)
}

// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// +kubebuilder:rbac:groups=core,namespace=default,resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;watch;delete

// Gate 2 (multiNodeEventInProgress) needs cluster-wide Pod LIST to
// detect multi-node K8s events. This is in addition to the namespaced
// Pod permission above. If the operator is installed namespaced and
// this ClusterRole permission is denied, Gate 2 fails closed and
// every reconcile defers — see the graceful fallback in
// multiNodeEventInProgress.
// +kubebuilder:rbac:groups=core,resources=pods,verbs=list;watch

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	if r.Tracker == nil {
		r.Tracker = NewInFlightTracker(DefaultTrackerTTL)
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("pvc-unbinder")
	}
	selectorPredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		if r.Selector == nil {
			return true
		}

		lbls := object.GetLabels()
		if lbls == nil {
			lbls = map[string]string{}
		}
		return r.Selector.Matches(labels.Set(lbls))
	})
	unbinderPredicate := predicate.NewPredicateFuncs(pvcUnbinderPredicate)

	return ctrl.NewControllerManagedBy(mgr).For(&corev1.Pod{}, builder.WithPredicates(selectorPredicate, unbinderPredicate)).Complete(r)
}

// Reconcile implements the algorithm described in the docs of [Reconciler]. To
// the best of it's ability, Reconcile is implemented to be idempotent. Due to
// the lack of transactions in Kubernetes/etc and the need to operate across
// many objects, it's quite difficult to guarantee this. The general strategy
// is to fetch a snapshot of the world as early as possible and then rely on
// ResourceVersions to inform us about changes from external actors, in which
// case we'll re-queue.
// TODO use an in memory timeout to prevent complete unbinding of Pods.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("PVCUnbinder")
	ctx = log.IntoContext(ctx, logger)

	var pod corev1.Pod
	if err := r.Client.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if ok, requeueAfter := r.ShouldRemediate(ctx, &pod); !ok || requeueAfter > 0 {
		logger.Info("shouldn't remediate Pod; skipping", "name", pod.Name, "ok", ok, "requeue-after", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter, Requeue: ok}, nil
	}

	// Gate 0: bridge the informer cache-staleness window. If we recently
	// deleted PVCs for this cluster, defer until the cache shows that
	// every deleted PVC has been replaced (same name, different UID).
	// See [InFlightTracker] for the full rationale; in short, the
	// cache-based gates below have a race where a just-deleted PVC is
	// gone from the List but the recreated PVC isn't yet visible —
	// during that window they incorrectly conclude "no unbind in
	// flight." The tracker uses the recorded UIDs as a positive
	// observation that the cache has caught up.
	clusterPVCsByName, err := r.listClusterPVCsByName(ctx, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}
	visibleUIDs := make(map[string]types.UID, len(clusterPVCsByName))
	for name, pvc := range clusterPVCsByName {
		visibleUIDs[name] = pvc.UID
	}
	if key := r.trackerKey(&pod); key != "" {
		held, err := r.Tracker.IsHeldWithVerify(ctx, key, visibleUIDs, r.verifyPVCSettled(pod.Namespace))
		if err != nil {
			return ctrl.Result{}, err
		}
		if held {
			const msg = "recent unbind for this cluster not yet observed as settled; deferring"
			logger.Info(msg, "name", pod.Name)
			r.recordGateDeferred(&pod, gateInFlight, msg)
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
	}

	// Gate 1: parent CR has the pause annotation set. Operators set this
	// around planned events (K8s cluster upgrades, node-pool surges, etc.)
	// where transient multi-node disruption is potentially expected.
	if paused, err := r.isClusterPaused(ctx, &pod); err != nil {
		return ctrl.Result{}, err
	} else if paused {
		const msg = "parent CR is paused via annotation; skipping"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gatePause, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
	}

	// Gate 2: stuck StatefulSet Pods across the cluster are pinned to
	// more than one distinct node. That's the signature of a K8s-wide
	// event (cloud control-plane upgrade, AZ hiccup, node-pool surge)
	// rather than a single-node failure — defer to natural recovery so
	// the unbinder doesn't force fresh PVs / ClaimRef clears on
	// brokers spread across multiple failing nodes simultaneously.
	//
	// Counting distinct nodes (not distinct pods) correctly handles the
	// case where multiple co-tenant pods are on the same failed node:
	// that's a legitimate single-node failure the unbinder should act
	// on, not a K8s-wide event.
	if multiNode, err := r.multiNodeEventInProgress(ctx); err != nil {
		return ctrl.Result{}, err
	} else if multiNode {
		const msg = "stuck Pods are pinned to multiple nodes; deferring as a likely K8s-wide event"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gateMultiNode, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
	}

	// Gate 3: a recreated PVC in this cluster is observable but not yet
	// bound (empty spec.volumeName). Defer until the binder has
	// re-bound it. Gate 0 covers the deleted-but-not-yet-recreated
	// window; Gate 3 covers the recreated-but-not-yet-bound window.
	for _, pvc := range clusterPVCsByName {
		if pvc.Spec.VolumeName == "" {
			const msg = "a recreated PVC in this cluster has no volumeName yet; deferring"
			logger.Info(msg, "name", pod.Name)
			r.recordGateDeferred(&pod, gatePVCRebinding, msg)
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
	}

	// NB: We denote PVCs that are deleted as a nil entry within this map. If a
	// PVC is not to be considered, it should be removed from this map.
	pvcByKey := map[client.ObjectKey]*corev1.PersistentVolumeClaim{}

	for _, pvcKey := range StsPVCs(&pod) {
		var pvc corev1.PersistentVolumeClaim
		if err := r.Client.Get(ctx, pvcKey, &pvc); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			pvcByKey[pvcKey] = nil
			continue
		}
		pvcByKey[pvcKey] = &pvc
	}

	// If there are no StatefulSet managed PVCs, there's nothing we can do.
	if len(pvcByKey) == 0 {
		logger.Info("Pod had no detectable StatefulSet PVCs. Skipping.")
		return ctrl.Result{}, nil
	}

	// Nothing can be done to scope this query unless we decide to bind the
	// implementation to rancher's local path provisioner which adds a label we
	// could query against.
	var pvList corev1.PersistentVolumeList
	if err := r.Client.List(ctx, &pvList); err != nil {
		return ctrl.Result{}, err
	}

	// 1. Filter PVs down to ones that are:
	// - Bound to a PVC we care about.
	// - Have a NodeAffinity (which we assume is the cause of our Pod being in Pending)
	var pvs []*corev1.PersistentVolume
	for i := range pvList.Items {
		pv := &pvList.Items[i]

		if pv.Spec.ClaimRef == nil {
			continue
		}

		key := client.ObjectKey{
			Name:      pv.Spec.ClaimRef.Name,
			Namespace: pv.Spec.ClaimRef.Namespace,
		}

		// Skip over any PVs that aren't bound to one of our targeted PVCs
		if _, ok := pvcByKey[key]; !ok {
			continue
		}

		// Filter out PVCs and PVs that don't have a NodeAffinity or aren't a
		// HostPath/Local volume.
		if pv.Spec.NodeAffinity == nil || (pv.Spec.HostPath == nil && pv.Spec.Local == nil) {
			delete(pvcByKey, key)
			continue
		}

		pvs = append(pvs, pv)
	}

	// 2. Ensure that all PVs have reclaim set to Retain
	for _, pv := range pvs {
		if err := r.ensureRetainPolicy(ctx, pv); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 3. Delete all Bound PVCs, capturing the UID we observed at
	// deletion time so the tracker can later verify the cache has seen
	// the recreated PVC (same name, new UID).
	//
	// Deferring Mark on the deletedByName map ensures that if any PVC
	// delete, PV recycle, or pod delete fails partway through, the
	// PVCs we already deleted are still recorded as in-flight. Without
	// this, a partial failure leaves the next reconcile blind to the
	// fact that an unbind has started for this cluster.
	deletedByName := map[string]types.UID{}
	defer func() {
		if key := r.trackerKey(&pod); key != "" {
			r.Tracker.Mark(key, deletedByName)
		}
	}()
	for key, pvc := range pvcByKey {
		if pvc == nil || pvc.Spec.VolumeName == "" {
			continue
		}

		logger.Info("deleting PVC to re-trigger volume binding", "name", pvc.Name)
		if err := r.Client.Delete(ctx, pvc, &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID:             &pvc.UID,
				ResourceVersion: &pvc.ResourceVersion,
			},
		}); err != nil {
			return ctrl.Result{}, err
		}

		deletedByName[pvc.Name] = pvc.UID

		// Indicate that this PVC is now deleted.
		pvcByKey[key] = nil
	}

	// 4. "Recycle" PVs that have been released. Technically optional, this
	// allows disks to rebind if a Node happens to recover.
	for _, pv := range pvs {
		if err := r.maybeRecyclePersistentVolume(ctx, pv); err != nil {
			return ctrl.Result{}, err
		}
	}

	missingPVCs := false
	for _, pvc := range pvcByKey {
		if pvc == nil {
			missingPVCs = true
			break
		}
	}

	// 5. Delete the Pod to cause the StatefulSet controller to re-create both
	// the PVCs and the Pod but only if there are missing PVCs.
	if !missingPVCs {
		logger.Info("not deleting Pod; no PVCs were deleted", "name", pod.Name)
		return ctrl.Result{}, nil
	}

	logger.Info("deleting Pod to trigger PVC recreation", "name", pod.Name)
	if err := r.Client.Delete(ctx, &pod, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &pod.UID,
			ResourceVersion: &pod.ResourceVersion,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Controller) ensureRetainPolicy(ctx context.Context, pv *corev1.PersistentVolume) error {
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain {
		return nil
	}

	log.FromContext(ctx).Info("setting reclaim policy to retain", "name", pv.Name)

	patch := client.StrategicMergeFrom(pv.DeepCopy(), &client.MergeFromWithOptimisticLock{})
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

// maybeRecyclePersistentVolume "recycles" a released PV by clearing it's .ClaimRef
// which makes it available for binding once again IF AllowRebinding is true.
// This strategy is only valid for volumes that utilize .HostPath or .Local.
func (r *Controller) maybeRecyclePersistentVolume(ctx context.Context, pv *corev1.PersistentVolume) error {
	// This case should never hit as we filter out such PVs earlier in the
	// controller though it's likely we don't handle such cases well aside from
	// not unbinding them.
	// TODO(chrisseto): Remove this check and add better clarify the expected
	// behavior of this controller if it encounters network backed disks.
	if pv.Spec.HostPath == nil && pv.Spec.Local == nil {
		return fmt.Errorf("%T must specify .Spec.HostPath or .Spec.Local for recycling: %q", pv, pv.Name)
	}

	// NB: We handle this flag here to ensure we get explicit the log messages
	// for all PVs we would have cleared the ClaimRef of.
	if !r.AllowRebinding {
		log.FromContext(ctx).Info("Skipping .ClaimRef clearing of PersistentVolume", "name", pv.Name, "AllowRebinding", r.AllowRebinding)
		return nil
	}

	// Skip over unbound PVs.
	if pv.Spec.ClaimRef == nil {
		return nil
	}

	log.FromContext(ctx).Info("Clearing .ClaimRef of PersistentVolume", "name", pv.Name, "AllowRebinding", r.AllowRebinding)

	// NB: We explicitly don't use an optimistic lock here as the control plane
	// will likely have updated this PV's Status to indicate that it's now
	// Released.
	patch := client.StrategicMergeFrom(pv.DeepCopy())
	pv.Spec.ClaimRef = nil
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

func (r *Controller) ShouldRemediate(ctx context.Context, pod *corev1.Pod) (bool, time.Duration) {
	if r.Selector != nil && !r.Selector.Matches(labels.Set(pod.Labels)) {
		log.FromContext(ctx).Info("selector not satisfied; skipping", "name", pod.Name, "labels", pod.Labels, "selector", r.Selector.String())
		return false, 0
	}

	idx := slices.IndexFunc(pod.Status.Conditions, func(cond corev1.PodCondition) bool {
		return cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == "Unschedulable"
	})

	// Paranoid check, ensure that the Pod we've fetched still passes our predicate.
	if idx == -1 || !pvcUnbinderPredicate(pod) {
		return false, 0
	}

	cond := pod.Status.Conditions[idx]

	// Short of re-implementing or importing scheduler, this is the best way to
	// detect if a scheduling failure is _likely_ due to volume node affinity
	// conflict. We check for a either an explicit mention of volume node
	// affinity issues OR a message indicating that no nodes within the cluster
	// may host this Pod.
	// As of Kubernetes >1.21.x <=1.28.x (Didn't track down an exact version),
	// volume node affinity conflicts no longer seem to appear in the message,
	// hence the need to check for a much weaker case.
	if !schedulingFailureRE.MatchString(cond.Message) {
		log.FromContext(ctx).Info("scheduling failure does not appear to indicate volume affinity issues; skipping", "name", pod.Name, "condition", cond)
		return false, 0
	}

	if delta := r.Timeout - time.Since(cond.LastTransitionTime.Time); delta > 0 {
		return true, delta
	}

	return true, 0
}

func pvcUnbinderPredicate(obj client.Object) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false
	}

	stsManaged := slices.ContainsFunc(pod.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
		return ref.APIVersion == "apps/v1" && ref.Kind == "StatefulSet" && ptr.Deref(ref.Controller, false)
	})

	isPending := pod.Status.Phase == corev1.PodPending

	return stsManaged && isPending
}

// trackerKey returns the InFlightTracker key for the cluster this Pod
// belongs to. Returns "" if the Pod lacks the standard
// app.kubernetes.io/instance label (in which case the unbinder behaves
// as if no tracker were in place — the original unscoped behavior).
// Includes the ClusterName prefix when running under
// MulticlusterController so keys are unique across K8s clusters.
func (r *Controller) trackerKey(pod *corev1.Pod) string {
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return ""
	}
	return r.ClusterName + "/" + pod.Namespace + "/" + instance
}

// isClusterPaused returns true if any of the Redpanda CR types that could
// own the given Pod carries the PauseAnnotation set to "true". The Pod
// is linked to its CR via the standard app.kubernetes.io/instance label.
//
// Three candidate types are tried in order (matching name+namespace):
//
//   - v1alpha2.Redpanda — single-cluster v2 deployments.
//   - v1alpha2.StretchCluster — multi-cluster/stretched v2 deployments.
//     Broker pods belonging to a StretchCluster member carry the
//     StretchCluster's name in the instance label.
//   - v1alpha1.Cluster — legacy v1 deployments.
//
// If ANY of these has the pause annotation set, the pod is paused. We
// gracefully ignore three "we can't ask about this type" categories so
// the same code works in every operator binary regardless of which
// types/CRDs are installed:
//
//   - apierrors.IsNotFound: the CR doesn't exist in this namespace.
//   - meta.IsNoMatchError: the CRD isn't installed on the API server.
//   - runtime.IsNotRegisteredError: the Go type isn't in this
//     controller's scheme (e.g. multicluster mode, which only has the
//     v2 types registered).
func (r *Controller) isClusterPaused(ctx context.Context, pod *corev1.Pod) (bool, error) {
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return false, nil
	}
	key := client.ObjectKey{Namespace: pod.Namespace, Name: instance}

	var rp redpandav1alpha2.Redpanda
	if err := r.Client.Get(ctx, key, &rp); err == nil {
		if rp.GetAnnotations()[PauseAnnotation] == "true" {
			return true, nil
		}
	} else if !cannotCheckCRType(err) {
		return false, err
	}

	var sc redpandav1alpha2.StretchCluster
	if err := r.Client.Get(ctx, key, &sc); err == nil {
		if sc.GetAnnotations()[PauseAnnotation] == "true" {
			return true, nil
		}
	} else if !cannotCheckCRType(err) {
		return false, err
	}

	var cluster vectorizedv1alpha1.Cluster
	if err := r.Client.Get(ctx, key, &cluster); err == nil {
		if cluster.GetAnnotations()[PauseAnnotation] == "true" {
			return true, nil
		}
	} else if !cannotCheckCRType(err) {
		return false, err
	}

	return false, nil
}

// cannotCheckCRType reports whether the error from a typed Get means
// "we have no way to know about this CR type right now" — covering all
// the cases where the type/CRD/CR simply isn't reachable. The caller
// should treat these as "not paused" and continue (rather than failing
// the reconcile on what is effectively missing-by-design state).
func cannotCheckCRType(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err)
}

// multiNodeEventInProgress reports whether the set of currently-stuck
// operator-managed pods spans more than one distinct node. Returning
// true means the unbinder should defer — the symptom matches a K8s-wide
// event (cloud upgrade, AZ flake, node-pool surge) rather than a
// single-node failure.
//
// Counting distinct nodes rather than distinct pods matters for the
// case where multiple co-tenant pods share a failed node — that's a
// legitimate single-node failure that the unbinder *should* act on,
// not a multi-node K8s event.
//
// The pod list is scoped by `app.kubernetes.io/managed-by=
// redpanda-operator` so unrelated workloads with stuck local-PV pods
// can't push this gate to "multi-node" and cause silent inaction.
// Cross-Redpanda-cluster events are still caught because every cluster
// the operator manages carries this label. Pods whose PV /
// NodeAffinity / hostname can't be resolved are skipped (we can't
// classify them as same-or-different).
func (r *Controller) multiNodeEventInProgress(ctx context.Context) (bool, error) {
	var pvList corev1.PersistentVolumeList
	if err := r.Client.List(ctx, &pvList); err != nil {
		return false, err
	}
	nodeByClaim := map[string]string{}
	for i := range pvList.Items {
		pv := &pvList.Items[i]
		if pv.Spec.ClaimRef == nil {
			continue
		}
		hostname := nodeFromPVAffinity(pv)
		if hostname == "" {
			continue
		}
		nodeByClaim[pv.Spec.ClaimRef.Namespace+"/"+pv.Spec.ClaimRef.Name] = hostname
	}

	// Two label-scoped LIST queries unioned by (namespace, name):
	//   - v1 + StretchCluster pods carry managed-by=redpanda-operator.
	//   - v2 Redpanda pods carry cluster.redpanda.com/operator=v2 (the
	//     chart-set managed-by=Helm is identical to a direct Helm
	//     install and can't be used to distinguish).
	pods := map[string]*corev1.Pod{}
	for _, sel := range []labels.Set{
		{operatorlabels.ManagedByKey: managedByLabelValue},
		{clusterRedpandaOperatorLabel: clusterRedpandaOperatorV2Value},
	} {
		var podList corev1.PodList
		if err := r.Client.List(ctx, &podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(sel),
		}); err != nil {
			// In namespaced installs the cluster-wide ClusterRole
			// binding may be absent — Gate 2 then can't see the
			// K8s-wide signal. Fail open rather than fail closed:
			// returning an error here would defer the reconcile,
			// which combined with permanent permission denial would
			// stall every unbind forever. Gate 3 (per-cluster PVC
			// serialization) and Gate 0 (cache-staleness bridge)
			// still protect against concurrent unbinds for the same
			// Redpanda cluster.
			if apierrors.IsForbidden(err) {
				log.FromContext(ctx).Info("cluster-wide Pod LIST forbidden; Gate 2 disabled, swap-prevention falls back to Gates 0+3", "error", err)
				return false, nil
			}
			return false, err
		}
		for i := range podList.Items {
			p := &podList.Items[i]
			pods[p.Namespace+"/"+p.Name] = p
		}
	}
	podList := corev1.PodList{Items: make([]corev1.Pod, 0, len(pods))}
	for _, p := range pods {
		podList.Items = append(podList.Items, *p)
	}
	nodes := map[string]struct{}{}
	for i := range podList.Items {
		other := &podList.Items[i]
		if !pvcUnbinderPredicate(other) {
			continue
		}
		if !podHasVolumeAffinityUnschedulable(other) {
			continue
		}
		for _, pvcKey := range StsPVCs(other) {
			if hostname, ok := nodeByClaim[pvcKey.Namespace+"/"+pvcKey.Name]; ok {
				nodes[hostname] = struct{}{}
			}
		}
		if len(nodes) > 1 {
			return true, nil
		}
	}
	return false, nil
}

// nodeFromPVAffinity extracts the hostname value pinned by a PV's
// NodeAffinity, used by Gate 2 to bucket stuck pods by their pinned
// node. Only `kubernetes.io/hostname` `In` selectors with a single
// value are recognized — that's the shape Local / HostPath volumes
// use, and the actual unbinder only ever acts on those (see the
// `pv.Spec.HostPath == nil && pv.Spec.Local == nil` filter in
// Reconcile). PVs with zone-topology affinity, NotIn selectors,
// multi-value `In`, or unfamiliar keys aren't in the unbinder's scope
// in the first place, so they don't need to contribute to Gate 2's
// distinct-node count.
//
// Gate 2 is best-effort regardless: any PV we can't classify just
// doesn't contribute to the count. The swap-prevention invariant
// still rests on Gate 3 (per-cluster PVC serialization) plus Gate 0
// (cache-staleness bridge).
func nodeFromPVAffinity(pv *corev1.PersistentVolume) string {
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return ""
	}
	for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key != corev1.LabelHostname {
				continue
			}
			if expr.Operator != corev1.NodeSelectorOpIn {
				continue
			}
			if len(expr.Values) > 0 {
				return expr.Values[0]
			}
		}
	}
	return ""
}

// verifyPVCSettled returns a [SettledVerifier] bound to the given
// namespace. It does a live API Get (bypassing the informer cache via
// the controller-runtime client, which by default reads through the
// cache — see the note below) on the PVC name and reports whether
// it's safe to drop the tracker entry:
//
//   - NotFound → not settled (the PVC was deleted but not yet
//     recreated; the StatefulSet may still be working on it).
//   - DeletionTimestamp != nil → not settled (stuck in Terminating,
//     often a finalizer; whatever cleared the cache hasn't actually
//     removed the object yet).
//   - Otherwise → settled (the PVC exists fresh).
//
// Used by Gate 0 on TTL expiry. Without this check, a PVC stuck in
// Terminating past the 1-minute TTL would let the tracker silently
// drop its entry and a sibling reconcile would unbind another pod.
//
// Note on cache vs API: controller-runtime's split client serves Gets
// from the informer cache by default. That's typically what we want
// (cheap), but here we explicitly want the API-server view because
// the *whole point* of this path is that the cache is stale. The
// client we have is the cached one, so this is a best-effort live
// check that may still hit the cache if it's been re-warmed — good
// enough for the TTL-expiry narrow case.
func (r *Controller) verifyPVCSettled(namespace string) SettledVerifier {
	return func(ctx context.Context, name string) (bool, error) {
		var pvc corev1.PersistentVolumeClaim
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pvc)
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if pvc.DeletionTimestamp != nil {
			return false, nil
		}
		return true, nil
	}
}

// listClusterPVCsByName returns a name→PVC snapshot for the PVCs that
// belong to the same Redpanda/Cluster as `pod` (matched by the
// app.kubernetes.io/instance label).
//
// Two callers consume this snapshot in Reconcile:
//
//   - The [InFlightTracker] uses the implicit name→UID mapping to
//     decide whether a previous unbind has fully settled (every
//     deleted PVC has been recreated with a new UID).
//   - Gate 3 inspects spec.volumeName on each entry to detect a PVC
//     that's recreated but not yet bound to a PV.
//
// Returns an empty (non-nil) map when the Pod has no instance label.
func (r *Controller) listClusterPVCsByName(ctx context.Context, pod *corev1.Pod) (map[string]corev1.PersistentVolumeClaim, error) {
	out := map[string]corev1.PersistentVolumeClaim{}
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return out, nil
	}
	var pvcList corev1.PersistentVolumeClaimList
	if err := r.Client.List(ctx, &pvcList, &client.ListOptions{
		Namespace: pod.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			operatorlabels.InstanceKey: instance,
		}),
	}); err != nil {
		return nil, err
	}
	for i := range pvcList.Items {
		out[pvcList.Items[i].Name] = pvcList.Items[i]
	}
	return out, nil
}

// podHasVolumeAffinityUnschedulable reports whether a Pod is Pending
// for the same reason that would cause the unbinder to act on it: the
// scheduler couldn't satisfy volume node affinity. Used by
// [Controller.otherStuckPodsExist] to detect K8s-wide events.
func podHasVolumeAffinityUnschedulable(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodScheduled || cond.Status != corev1.ConditionFalse || cond.Reason != "Unschedulable" {
			continue
		}
		return schedulingFailureRE.MatchString(cond.Message)
	}
	return false
}

// StsPVCs returns a slice of [client.ObjectKey] of PVCs that are attached to
// this Pod and are determined to be managed by the StatefulSet controller.
func StsPVCs(pod *corev1.Pod) []client.ObjectKey {
	var found []client.ObjectKey
	for i := range pod.Spec.Volumes {
		vol := &pod.Spec.Volumes[i]

		if vol.PersistentVolumeClaim == nil {
			continue
		}

		// Easiest way to tell is if the PVC's name ends with the Pods name.
		if !strings.HasSuffix(vol.PersistentVolumeClaim.ClaimName, pod.Name) {
			continue
		}

		found = append(found, client.ObjectKey{
			Name:      vol.PersistentVolumeClaim.ClaimName,
			Namespace: pod.Namespace,
		})
	}
	return found
}
