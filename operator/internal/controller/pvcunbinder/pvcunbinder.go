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

	"github.com/go-logr/logr"
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
	gateFreedPV      = "freed-pv"
)

// The unbinder records its in-flight state as annotations on the
// PersistentVolumes it operates on, NOT in process memory. PVs are
// forced to a Retain policy before any destructive action and survive
// operator restarts, leader handoffs, and the pod/PVC deletions that
// make up an unbind — so every gate that reads these annotations is
// crash-safe by construction. All reads of these annotations go
// through the uncached Reader: the annotations are written by this
// controller moments before they're needed, which is exactly the
// window where the informer cache lags.
const (
	// InFlightAnnotation marks a PV whose bound PVC this controller is
	// about to delete (written in the same patch that forces the
	// Retain policy, BEFORE the PVC delete). The value is the cluster
	// key of the Redpanda cluster the PV belongs to.
	//
	// While any PV carries this annotation for a cluster, Gate 0
	// defers all further unbinds in that cluster. The annotation is
	// cleared once the deleted claim is observed recreated (same
	// name, different UID) and bound — i.e., the previous unbind has
	// fully settled.
	InFlightAnnotation = "operator.redpanda.com/pvc-unbinder-in-flight"

	// InFlightClaimAnnotation records the claim the PV served at
	// unbind time as "namespace/name/uid". Written and cleared
	// together with InFlightAnnotation. The UID lets the settle check
	// distinguish the recreated claim from the not-yet-deleted old
	// one; the namespace/name survive the rebinding path nil-ing out
	// pv.Spec.ClaimRef.
	InFlightClaimAnnotation = "operator.redpanda.com/pvc-unbinder-claim"

	// FreedPVAnnotation marks a PV whose ClaimRef this controller
	// cleared (the `--allow-pv-rebinding` path). The value is the
	// cluster key of the Redpanda cluster the PV belonged to.
	//
	// While a PV carrying this annotation is in Available phase AND
	// its pinned node still exists, the unbinder refuses to unbind
	// anything else in the same cluster (Gate 4). Rationale: a freed
	// Available PV is a first-class binding candidate for ANY new PVC
	// — if we unbind a second broker while the first broker's freed
	// disk is still floating, the scheduler can pair the second
	// broker's fresh claim with the first broker's old disk (the
	// cross-broker swap from INC-2818, reproduced sequentially even
	// with serialized unbinds). The annotation is cleared once the PV
	// is observed Bound again.
	FreedPVAnnotation = "operator.redpanda.com/pvc-unbinder-freed"
)

// eventReasonGateDeferred is the EventReason emitted on the Pod when a
// safety gate defers remediation. Operators watching for "why isn't
// the PVCUnbinder acting on my stuck pod" can `kubectl describe pod`
// and see this reason + the gate label.
const eventReasonGateDeferred = "PVCUnbinderDeferred"

// Gate 2 identifies "Redpanda broker pod" via two label sets, because
// the cluster types don't share a single pod label that uniquely marks
// Redpanda brokers:
//
//   - v1 Cluster (operator): `app.kubernetes.io/managed-by=redpanda-operator`
//     (hardcoded at operator/pkg/labels/labels.go). v1 pods do NOT
//     carry `cluster.redpanda.com/broker`.
//   - v2 Redpanda, StretchCluster, and direct Helm installs all render
//     broker pods through the redpanda chart, whose pod template sets
//     `cluster.redpanda.com/broker=true` (charts/redpanda/statefulset.go,
//     StatefulSetPodLabels). NB: the operator's
//     `cluster.redpanda.com/operator=v2` ownership label lands on the
//     StatefulSet OBJECT only — it is never propagated to the pod
//     template, so it cannot be used to select pods.
//
// Filtering on `app.kubernetes.io/name=redpanda` would catch all of
// these by default but breaks for users running with `nameOverride`
// (which is a supported customization in production). So Gate 2 does
// two LIST queries and unions the results by (namespace, name).
const (
	managedByLabelValue = "redpanda-operator"
	brokerLabelKey      = "cluster.redpanda.com/broker"
	brokerLabelValue    = "true"
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
	// ClusterName disambiguates cluster keys in multicluster mode.
	// Empty for single-cluster operation.
	ClusterName string
	// Recorder, if non-nil, receives an Event on the Pod every time a
	// safety gate defers remediation. Nil-safe — if unset, only the
	// metric is incremented. Uses the new k8s.io/client-go/tools/events
	// API rather than the deprecated tools/record API.
	Recorder events.EventRecorder
	// Reader is an uncached client.Reader (the manager's APIReader)
	// used where reading through the informer cache would defeat the
	// purpose of the check:
	//
	//   - the Gate 0/4 PV-annotation scans and the Gate 0 claim settle
	//     check, which read back state this controller wrote moments
	//     earlier — exactly the window where the informer cache lags —
	//     and whose durability guarantee depends on seeing true
	//     API-server state, and
	//   - Node existence checks in the freed-PV gate, where a cached
	//     Get would force the informer to watch every Node in the
	//     cluster for a check that only runs during disruptions.
	//
	// Falls back to Client when nil (tests).
	Reader client.Reader
	// Logger, when non-nil, overrides the context logger so this controller
	// can run at an independent verbosity (see --pvcunbinder-log-level). When
	// nil, the manager's logger is used.
	Logger *logr.Logger
}

// reader returns the uncached Reader if configured, otherwise the
// (cached) Client. Test code typically leaves Reader nil and relies on
// the fake client serving both roles.
func (r *Controller) reader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Client
}

// MulticlusterController is a multicluster-aware version of Controller that
// watches Pods across all clusters managed by a multicluster.Manager.
type MulticlusterController struct {
	Manager        multicluster.Manager
	Timeout        time.Duration
	Selector       labels.Selector
	AllowRebinding bool
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
		ClusterName:    req.ClusterName,
		Recorder:       k8sCluster.GetEventRecorder("pvc-unbinder"),
		Reader:         k8sCluster.GetAPIReader(),
	}
	return c.Reconcile(ctx, req.Request)
}

// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=redpanda.vectorized.io,resources=clusters,verbs=get;list;watch

// The gate-defer Events are written through the new events API
// (k8s.io/client-go/tools/events), which creates events.k8s.io/v1
// objects — not core/v1 Events.
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// +kubebuilder:rbac:groups=core,namespace=default,resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;watch;delete

// Gate 2 (multiNodeEventInProgress) needs cluster-wide Pod LIST to
// detect multi-node K8s events. This is in addition to the namespaced
// Pod permission above. If the operator is installed namespaced and
// this ClusterRole permission is denied, Gate 2 fails OPEN (logs and
// skips, rather than deferring every reconcile forever) — see the
// Forbidden fallback in multiNodeEventInProgress.
// +kubebuilder:rbac:groups=core,resources=pods,verbs=list;watch

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("pvc-unbinder")
	}
	if r.Reader == nil {
		r.Reader = mgr.GetAPIReader()
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

// Reconcile implements the algorithm described in the docs of [Controller]. To
// the best of it's ability, Reconcile is implemented to be idempotent. Due to
// the lack of transactions in Kubernetes/etc and the need to operate across
// many objects, it's quite difficult to guarantee this. The general strategy
// is to fetch a snapshot of the world as early as possible and then rely on
// ResourceVersions to inform us about changes from external actors, in which
// case we'll re-queue. Recovery from partial failures relies on the durable
// in-flight PV annotations: any successful prefix of the action steps leaves
// state that Gate 0 either holds on (siblings) or resumes from (the same pod
// retrying — see checkPVGates' own-claim exemption).
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// If a dedicated logger was configured (--pvcunbinder-log-level), inject it
	// so downstream LoggerFrom(ctx) calls run at that verbosity.
	if r.Logger != nil {
		ctx = ctrl.LoggerInto(ctx, *r.Logger)
	}
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

	// Gates 0 and 4 share one uncached scan over the cluster's
	// annotated PVs. Uncached because the annotations are written by
	// this controller moments before they're needed — exactly the
	// window where the informer cache lags — and because durable
	// API-server state (not process memory) is what makes these gates
	// survive operator restarts and leader handoffs mid-unbind.
	pvGates, err := r.checkPVGates(ctx, r.clusterKey(&pod), &pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Gate 0: a previous unbind in this cluster has not settled — a PV
	// carries the in-flight annotation and its recorded claim has not
	// yet been observed recreated (same name, NEW uid) and bound.
	// Covers both the deleted-but-not-yet-recreated window and any
	// partial failure of a previous reconcile (the annotation is
	// written before the first destructive action).
	if pvGates.unbindInFlight {
		const msg = "a previous unbind for this cluster has not settled; deferring"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gateInFlight, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
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

	// Gate 3: a PVC in this cluster is observable but not yet bound
	// (empty spec.volumeName). Defer until the binder has re-bound it.
	// Gate 0 already covers unbinds *we* performed end-to-end (the
	// in-flight annotation isn't cleared until the recreated claim is
	// bound); Gate 3 additionally catches unbound claims from external
	// actors — e.g. an admin manually deleting a PVC — which carry no
	// annotation. Cached read: a false pass here is still backstopped
	// by Gate 0 for our own actions, and a false defer is harmless.
	clusterPVCsByName, err := r.listClusterPVCsByName(ctx, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}
	for _, pvc := range clusterPVCsByName {
		if pvc.Spec.VolumeName == "" {
			const msg = "a PVC in this cluster has no volumeName yet; deferring"
			logger.Info(msg, "name", pod.Name)
			r.recordGateDeferred(&pod, gatePVCRebinding, msg)
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
	}

	// Gate 4: a PV we previously freed (ClaimRef cleared under
	// --allow-pv-rebinding) is still Available and its node still
	// exists — meaning it's a live binding candidate that a NEW PVC
	// from a subsequent unbind could mis-pair with (the sequential
	// cross-broker swap). Defer all further unbinds for this cluster
	// until the freed disk is re-bound or its node is permanently
	// gone. See [FreedPVAnnotation].
	if pvGates.freedPVUnresolved {
		const msg = "a previously freed PV is still Available with a live node; deferring to avoid cross-broker rebinding"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gateFreedPV, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
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

	// 2. Prepare every PV for unbinding: force the Retain policy and
	// record the in-flight annotations (cluster key + claim
	// namespace/name/uid) in a single patch. This happens BEFORE any
	// destructive action, so a crash, restart, or partial failure at
	// any later point leaves durable evidence that an unbind started —
	// Gate 0 reads it back (uncached) on the next reconcile, from any
	// process.
	for _, pv := range pvs {
		if err := r.prepareForUnbind(ctx, pv, r.clusterKey(&pod)); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 3. Delete all Bound PVCs.
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

		// Indicate that this PVC is now deleted.
		pvcByKey[key] = nil
	}

	// 4. "Recycle" PVs that have been released. Technically optional, this
	// allows disks to rebind if a Node happens to recover. Each recycled
	// PV is annotated with the cluster key so Gate 4 holds further
	// unbinds until the freed disk is re-bound (or its node is
	// permanently gone).
	for _, pv := range pvs {
		if err := r.maybeRecyclePersistentVolume(ctx, pv, r.clusterKey(&pod)); err != nil {
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

// prepareForUnbind readies a PV for the destructive part of an unbind
// in a single patch: it forces the Retain reclaim policy AND records
// the in-flight annotations ([InFlightAnnotation] = clusterKey,
// [InFlightClaimAnnotation] = "namespace/name/uid" of the currently
// bound claim). Runs BEFORE the PVC delete so the durable marker
// exists no matter what fails afterwards.
//
// When clusterKey is "" (pod without the standard instance label) only
// the Retain policy is applied — such pods were never covered by the
// per-cluster gates.
func (r *Controller) prepareForUnbind(ctx context.Context, pv *corev1.PersistentVolume, clusterKey string) error {
	annotate := clusterKey != "" && pv.Spec.ClaimRef != nil && pv.Annotations[InFlightAnnotation] != clusterKey
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain && !annotate {
		return nil
	}

	log.FromContext(ctx).Info("preparing PV for unbind (retain policy + in-flight annotations)", "name", pv.Name)

	patch := client.StrategicMergeFrom(pv.DeepCopy(), &client.MergeFromWithOptimisticLock{})
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if annotate {
		if pv.Annotations == nil {
			pv.Annotations = map[string]string{}
		}
		pv.Annotations[InFlightAnnotation] = clusterKey
		pv.Annotations[InFlightClaimAnnotation] = fmt.Sprintf("%s/%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name, pv.Spec.ClaimRef.UID)
	}
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

// maybeRecyclePersistentVolume "recycles" a released PV by clearing it's .ClaimRef
// which makes it available for binding once again IF AllowRebinding is true.
// This strategy is only valid for volumes that utilize .HostPath or .Local.
//
// When clearing the ClaimRef, the PV is annotated (in the same patch)
// with [FreedPVAnnotation] = clusterKey so that Gate 4 can refuse
// further unbinds in the same cluster until this PV is observed Bound
// again (or its node is permanently gone). See [FreedPVAnnotation].
func (r *Controller) maybeRecyclePersistentVolume(ctx context.Context, pv *corev1.PersistentVolume, clusterKey string) error {
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
	if clusterKey != "" {
		if pv.Annotations == nil {
			pv.Annotations = map[string]string{}
		}
		pv.Annotations[FreedPVAnnotation] = clusterKey
	}
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

// pvGateState is the result of one uncached scan over the cluster's
// annotated PVs, feeding Gate 0 (unbindInFlight) and Gate 4
// (freedPVUnresolved).
type pvGateState struct {
	unbindInFlight    bool
	freedPVUnresolved bool
}

// checkPVGates evaluates Gates 0 and 4 in a single uncached pass over
// the PV list. All reads go through the uncached Reader: these
// annotations are written by this controller moments before they're
// needed (exactly where the informer cache lags), and being durable
// API-server state is what makes the gates survive restarts and
// leader handoffs.
//
// Per PV carrying [InFlightAnnotation] == clusterKey (Gate 0): the
// recorded claim is fetched (uncached). If it's missing, still shows
// the recorded (old) UID, is Terminating, or is unbound — the
// previous unbind hasn't settled and unbindInFlight is set. Once the
// claim is observed recreated (new UID) and bound, the in-flight
// annotations are cleared.
//
// Per PV carrying [FreedPVAnnotation] == clusterKey (Gate 4):
//
//   - Bound again → the freed disk found a claim (ideally its original
//     broker's recreated PVC). Clear the annotation.
//   - Available + pinned node EXISTS → live rebinding candidate; a new
//     PVC from another unbind could mis-pair with it.
//     freedPVUnresolved is set.
//   - Available + pinned node GONE (Node object deleted) → inert: with
//     WaitForFirstConsumer, no pod can ever schedule onto a
//     nonexistent node, so the binder will never match this PV. Not
//     blocking, but the annotation is KEPT — if a node with the same
//     name rejoins (name reuse is real with LocalPathProvisioner), the
//     PV becomes a live candidate again and the gate re-engages.
//
// In-flight entries whose recorded claim belongs to the Pod being
// reconciled are NOT counted as blocking. The reconcile for that pod
// is exactly the retry that completes a stuck unbind — most
// importantly the pod-delete step, which is what releases a claim
// held in Terminating by the pvc-protection finalizer. Counting the
// pod's own claims would deadlock: the claim can't finish deleting
// until the pod is deleted, and the pod would never be deleted
// because the gate defers on the claim. Sibling pods still defer.
//
// If a freed PV never re-binds and its node persists — or an
// in-flight claim is never recreated (e.g. the cluster was scaled
// down mid-unbind) — these gates hold the cluster's unbinds
// indefinitely. That's deliberate: the failure mode is an alertable
// halt (gate metric + Event) instead of a silent cross-broker disk
// swap. Operators resolve it by removing the orphaned PV or, if the
// state is known-good, the annotation itself.
//
// clusterKey == "" (pod without the instance label) short-circuits to
// "no gates engaged" — such pods were never covered by per-cluster
// serialization.
func (r *Controller) checkPVGates(ctx context.Context, clusterKey string, pod *corev1.Pod) (pvGateState, error) {
	var state pvGateState
	if clusterKey == "" {
		return state, nil
	}

	ownClaims := map[string]struct{}{}
	for _, key := range StsPVCs(pod) {
		ownClaims[key.Namespace+"/"+key.Name] = struct{}{}
	}

	var pvList corev1.PersistentVolumeList
	if err := r.reader().List(ctx, &pvList); err != nil {
		return state, err
	}

	for i := range pvList.Items {
		pv := &pvList.Items[i]

		if pv.Annotations[InFlightAnnotation] == clusterKey {
			settled, err := r.inFlightClaimSettled(ctx, pv)
			if err != nil {
				return state, err
			}
			switch {
			case settled:
				// Clear both in-flight annotations. Failure to clear is
				// non-fatal — we'd just re-verify next reconcile.
				patch := client.StrategicMergeFrom(pv.DeepCopy())
				delete(pv.Annotations, InFlightAnnotation)
				delete(pv.Annotations, InFlightClaimAnnotation)
				if err := r.Client.Patch(ctx, pv, patch); err != nil {
					log.FromContext(ctx).Error(err, "failed to clear in-flight annotations; will retry next reconcile", "name", pv.Name)
				}
			case r.inFlightClaimOwnedBy(pv, ownClaims):
				// This pod's own unfinished unbind — let the reconcile
				// proceed so the idempotent pipeline can complete it.
			default:
				state.unbindInFlight = true
			}
		}

		if pv.Annotations[FreedPVAnnotation] == clusterKey {
			blocking, err := r.freedPVBlocking(ctx, pv)
			if err != nil {
				return state, err
			}
			if blocking {
				state.freedPVUnresolved = true
			}
		}

		if state.unbindInFlight && state.freedPVUnresolved {
			break
		}
	}
	return state, nil
}

// inFlightClaimOwnedBy reports whether the claim recorded in the PV's
// [InFlightClaimAnnotation] is one of the given pod-owned claim keys
// (formatted "namespace/name"). Used by Gate 0 to let a pod's own
// unfinished unbind proceed instead of deadlocking on itself. A
// malformed annotation is never "owned" — it stays blocking.
func (r *Controller) inFlightClaimOwnedBy(pv *corev1.PersistentVolume, ownClaims map[string]struct{}) bool {
	parts := strings.SplitN(pv.Annotations[InFlightClaimAnnotation], "/", 3)
	if len(parts) != 3 {
		return false
	}
	_, ok := ownClaims[parts[0]+"/"+parts[1]]
	return ok
}

// inFlightClaimSettled reports whether the claim recorded in a PV's
// [InFlightClaimAnnotation] no longer represents an unbind in
// progress, observed through the uncached Reader. Two states settle:
//
//   - The claim was recreated with a NEW UID and is bound — the
//     unbind completed end-to-end.
//   - The claim still has the OLD UID and is NOT Terminating — the
//     previous reconcile failed between annotating and deleting, so
//     no destructive action ever happened. The world is in its
//     pre-unbind state, which is safe to proceed (and retry) from.
//     Without this case, a failed PVC delete after a successful
//     annotation write would deadlock the cluster's unbinder: Gate 0
//     would defer every reconcile, including the retry that would
//     re-attempt the delete.
//
// Everything else — claim missing (deleted, awaiting StatefulSet
// recreation), Terminating (deletion held by the pvc-protection
// finalizer until the pod is deleted), or recreated-but-unbound — is
// an unbind in progress. A malformed annotation is treated as
// not-settled (conservative) and logged.
func (r *Controller) inFlightClaimSettled(ctx context.Context, pv *corev1.PersistentVolume) (bool, error) {
	parts := strings.SplitN(pv.Annotations[InFlightClaimAnnotation], "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		log.FromContext(ctx).Info("malformed in-flight claim annotation; treating the unbind as unsettled", "name", pv.Name, "value", pv.Annotations[InFlightClaimAnnotation])
		return false, nil
	}
	namespace, name, oldUID := parts[0], parts[1], types.UID(parts[2])

	var pvc corev1.PersistentVolumeClaim
	err := r.reader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pvc)
	switch {
	case apierrors.IsNotFound(err):
		// Deleted but not yet recreated.
		return false, nil
	case err != nil:
		return false, err
	case pvc.DeletionTimestamp != nil:
		// Terminating — old claim held by pvc-protection until its pod
		// is deleted, or a recreated claim being deleted externally.
		return false, nil
	case pvc.UID == oldUID:
		// The old claim exists untouched: the delete never happened
		// (previous reconcile failed between annotating and deleting).
		// Pre-unbind state — settled, safe to retry from scratch.
		return true, nil
	case pvc.Spec.VolumeName == "":
		// Recreated but the binder hasn't placed a volume yet.
		return false, nil
	default:
		return true, nil
	}
}

// freedPVBlocking reports whether a single freed PV is currently a
// live rebinding candidate (Gate 4). Clears the annotation when the
// PV is observed Bound again. See [checkPVGates] for the decision
// table.
func (r *Controller) freedPVBlocking(ctx context.Context, pv *corev1.PersistentVolume) (bool, error) {
	// Re-bound: the freed disk has been claimed again. Clear the
	// annotation so future reconciles don't keep paying for the scan.
	// Failure to clear is non-fatal — we'd just re-observe Bound next
	// time.
	if pv.Status.Phase == corev1.VolumeBound {
		patch := client.StrategicMergeFrom(pv.DeepCopy())
		delete(pv.Annotations, FreedPVAnnotation)
		if err := r.Client.Patch(ctx, pv, patch); err != nil {
			log.FromContext(ctx).Error(err, "failed to clear freed-PV annotation; will retry next reconcile", "name", pv.Name)
		}
		return false, nil
	}

	if pv.Status.Phase != corev1.VolumeAvailable {
		// Released/Failed/Pending — not a binding candidate right now.
		// Keep the annotation; if it transitions to Available later
		// the gate picks it up.
		return false, nil
	}

	hostname := nodeFromPVAffinity(pv)
	if hostname == "" {
		// Can't resolve the pinned node; be conservative — an
		// Available freed PV we can't classify is treated as a live
		// candidate.
		return true, nil
	}

	var node corev1.Node
	err := r.reader().Get(ctx, client.ObjectKey{Name: hostname}, &node)
	switch {
	case err == nil:
		// Node exists (Ready or not — cordoned/NotReady nodes can
		// recover and bind). Live candidate; defer.
		return true, nil
	case apierrors.IsNotFound(err):
		// Node permanently gone; PV is inert. Keep annotation in case
		// of node-name reuse, but don't defer on it.
		return false, nil
	default:
		return false, err
	}
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

// clusterKey identifies the Redpanda cluster this Pod belongs to; it's
// the value written into the PV gate annotations ([InFlightAnnotation],
// [FreedPVAnnotation]) to scope Gates 0 and 4 per cluster. Returns ""
// if the Pod lacks the standard app.kubernetes.io/instance label (in
// which case the per-cluster gates are skipped — the original unscoped
// behavior). Includes the ClusterName prefix when running under
// MulticlusterController so keys are unique across K8s clusters.
func (r *Controller) clusterKey(pod *corev1.Pod) string {
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
// Redpanda broker pods spans more than one distinct node. Returning
// true means the unbinder should defer — the symptom matches a K8s-wide
// event (cloud upgrade, AZ flake, node-pool surge) rather than a
// single-node failure.
//
// Counting distinct nodes rather than distinct pods matters for the
// case where multiple co-tenant pods share a failed node — that's a
// legitimate single-node failure that the unbinder *should* act on,
// not a multi-node K8s event.
//
// The pod list is scoped to Redpanda broker pods (see the
// managedByLabelValue / brokerLabelKey constants for the two-selector
// union and why each is needed) so unrelated workloads with stuck
// local-PV pods can't push this gate to "multi-node" and cause silent
// inaction. Cross-Redpanda-cluster events are still caught because
// every broker pod carries at least one of the two labels. Pods whose
// PV / NodeAffinity / hostname can't be resolved are skipped (we
// can't classify them as same-or-different).
//
// This gate is best-effort by design: it reads cached lists, so a
// fast-moving multi-node event may be under-counted. The binding
// safety invariant rests on Gates 0/3/4, not on this gate.
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
	//   - v1 Cluster pods carry managed-by=redpanda-operator.
	//   - v2 Redpanda / StretchCluster / direct-Helm broker pods all
	//     render through the redpanda chart, whose pod template sets
	//     cluster.redpanda.com/broker=true. (The operator's
	//     cluster.redpanda.com/operator=v2 ownership label is on the
	//     StatefulSet object only, never on pods — selecting on it
	//     here would match nothing.)
	pods := map[string]*corev1.Pod{}
	for _, sel := range []labels.Set{
		{operatorlabels.ManagedByKey: managedByLabelValue},
		{brokerLabelKey: brokerLabelValue},
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
			// stall every unbind forever. Gates 0/3/4 (the durable
			// PV-annotation gates and unbound-PVC serialization)
			// still protect against concurrent and sequential
			// mis-binding for the same Redpanda cluster.
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
// doesn't contribute to the count. The binding safety invariant rests
// on Gates 0/3/4, not on this function's coverage.
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

// listClusterPVCsByName returns a name→PVC snapshot for the PVCs that
// belong to the same Redpanda/Cluster as `pod` (matched by the
// app.kubernetes.io/instance label). Gate 3 inspects spec.volumeName
// on each entry to detect a PVC that's not yet bound to a PV.
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
// [Controller.multiNodeEventInProgress] to detect K8s-wide events.
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
