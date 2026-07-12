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
	storagev1 "k8s.io/api/storage/v1"
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

// SchedulingFailureRE matches scheduler messages that indicate a Pod
// cannot be placed because of volume node-affinity constraints. Used by
// both the PVCUnbinder and the Broker controller to detect dead-node
// scenarios.
var SchedulingFailureRE = regexp.MustCompile(`(^0/[1-9]\d* nodes are available)|(volume node affinity)`)

// PauseAnnotation pauses the PVCUnbinder for a whole cluster. Set it
// to "true" on the parent Redpanda or Cluster CR. Use it during
// planned events (cluster upgrades, node-pool surges, maintenance)
// when many nodes are disrupted at once.
const PauseAnnotation = "operator.redpanda.com/pause-pvc-unbinder"

// requeueDuringDisruption is the wait between re-checks after any
// gate defers an unbind.
const requeueDuringDisruption = 30 * time.Second

// The unbinder runs five safety gates before it deletes anything.
// Each gate can defer (postpone) the unbind. In order:
//
//   - Gate 0 "in-flight": a previous unbind in this cluster has not
//     finished yet (its recreated claim is not bound). Wait for it.
//   - Gate 1 "pause": the cluster CR carries [PauseAnnotation]. Wait.
//   - Gate 2 "multi-node": stuck pods are pinned to several different
//     nodes, which looks like a cluster-wide event, not a single node
//     failure. Wait.
//   - Gate 3 "pvc-rebinding": some claim in the cluster is not bound
//     yet. It is probably re-binding right now, so wait — unless the
//     claim is exempt because its pod is provably deadlocked (see
//     [Controller.stuckClaimNames]).
//   - Gate 4 "freed-pv": a PV freed by --allow-pv-rebinding is still
//     floating and could pair with the wrong claim. Wait.
//
// These names are the label values on the
// `..._pvc_unbinder_gate_deferred_total` metric and appear in the
// deferral Event on the Pod. Keep this list closed.
const (
	gateInFlight     = "in-flight"
	gatePause        = "pause"
	gateMultiNode    = "multi-node"
	gatePVCRebinding = "pvc-rebinding"
	gateFreedPV      = "freed-pv"
)

// The unbinder stores its progress as annotations on the PVs it works
// on, not in memory. PVs survive operator restarts and the deletions
// that make up an unbind, so the gates that read these annotations
// are crash-safe. All reads go through the uncached Reader, because
// the annotations are written moments before they are read — exactly
// when the informer cache lags.
const (
	// InFlightAnnotation marks a PV whose bound PVC this controller is
	// about to delete. It is written together with the Retain policy,
	// before the delete. The value is the cluster key.
	//
	// While any PV in a cluster carries this annotation, Gate 0 defers
	// all further unbinds there. It is cleared once the deleted claim
	// is seen recreated (same name, new UID) and bound.
	InFlightAnnotation = "operator.redpanda.com/pvc-unbinder-in-flight"

	// InFlightClaimAnnotation records the claim the PV served at
	// unbind time, as "namespace/name/uid". Written and cleared with
	// InFlightAnnotation. The UID tells the recreated claim apart from
	// the old one that is still being deleted.
	InFlightClaimAnnotation = "operator.redpanda.com/pvc-unbinder-claim"

	// FreedPVAnnotation marks a PV whose ClaimRef this controller
	// cleared (the --allow-pv-rebinding path). The value is the
	// cluster key.
	//
	// While such a PV is Available and its pinned node still exists,
	// Gate 4 blocks further unbinds in the same cluster. Reason: an
	// Available PV can bind to ANY new claim, so unbinding a second
	// broker while the first broker's freed disk still floats can give
	// the second broker the first broker's disk (the INC-2818
	// cross-broker swap). Cleared once the PV is Bound again.
	FreedPVAnnotation = "operator.redpanda.com/pvc-unbinder-freed"
)

// eventReasonGateDeferred is the Event reason written on the Pod when
// a gate defers remediation. `kubectl describe pod` shows it together
// with the gate name.
const eventReasonGateDeferred = "PVCUnbinderDeferred"

// eventReasonGateExempted is the Event reason written on the Pod when
// Gate 3 is passed because every unbound claim was exempted. A gate
// override gets the same paper trail as a deferral (metric, Warning
// Event, log) so incidents stay easy to attribute.
const eventReasonGateExempted = "PVCUnbinderGateExempted"

// Gate 2 finds Redpanda broker pods with two label queries, because
// no single pod label covers all cluster types:
//
//   - v1 Cluster pods carry app.kubernetes.io/managed-by=redpanda-operator.
//   - v2 Redpanda, StretchCluster, and Helm installs render pods
//     through the redpanda chart, which sets
//     cluster.redpanda.com/broker=true. (The operator=v2 label exists
//     only on the StatefulSet object, never on pods.)
//
// app.kubernetes.io/name=redpanda would cover both but breaks under
// nameOverride, so Gate 2 runs both queries and unions the results.
const (
	managedByLabelValue = "redpanda-operator"
	brokerLabelKey      = "cluster.redpanda.com/broker"
	brokerLabelValue    = "true"
)

// Controller watches for Pods stuck in Pending because their local
// volume is pinned to a node they can no longer run on, and frees
// them.
//
// It watches Pod events, not Node events: a Node-deletion event can
// be missed when the operator itself ran on the dead node, and
// re-implementing the scheduler's label matching would be risky.
//
// To let a stuck Pod reschedule it:
//  1. finds the Pod's PVs and PVCs,
//  2. sets a Retain policy on those PVs,
//  3. deletes the PVCs (PVCs are immutable; delete is the only way),
//  4. optionally clears the PVs' ClaimRef (--allow-pv-rebinding) so a
//     returning node might reclaim its old volume,
//  5. deletes the Pod, which makes the StatefulSet recreate Pod and
//     PVCs and bind them somewhere schedulable.
type Controller struct {
	Client client.Client
	// Timeout is the duration a Pod must be stuck in Pending before
	// remediation is attempted.
	Timeout time.Duration
	// Selector, if specified, will narrow the scope of Pods that this
	// Reconciler will consider for remediation.
	Selector labels.Selector
	// AllowRebinding also clears the freed PV's ClaimRef so the disk
	// can bind again if its node returns. Deprecated and risky: with
	// HostPath volumes and node-name reuse it can produce permission
	// errors or point at missing directories (LocalPathProvisioner's
	// helper Pod does not run again for a volume it believes already
	// exists), and it disables the Gate 3 exemption entirely.
	AllowRebinding bool
	// DisableStuckClaimExemption turns off Gate 3's stuck-claim
	// exemption and restores the old behavior: defer on every unbound
	// claim. It is an escape hatch for environments where the
	// exemption's proof chain misfires (for example, unusual local-PV
	// node-affinity shapes). Unlike the pause annotation, it keeps the
	// rest of the unbinder running.
	DisableStuckClaimExemption bool
	// ClusterName disambiguates cluster keys in multicluster mode.
	// Empty for single-cluster operation.
	ClusterName string
	// Recorder, if non-nil, receives an Event on the Pod every time a
	// safety gate defers remediation, and a Warning Event when Gate 3
	// is passed via the stuck-claim exemption. Nil-safe — if unset,
	// only the metrics are incremented. Uses the new k8s.io/client-go/tools/events
	// API rather than the deprecated tools/record API.
	Recorder events.EventRecorder
	// Reader is an uncached client.Reader (the manager's APIReader).
	// It is used wherever a stale cache would defeat the check:
	//
	//   - Gate 0/4 annotation scans, which read back state this
	//     controller wrote moments earlier;
	//   - Node lookups, so the cache does not have to watch every
	//     Node for checks that only run during incidents;
	//   - all Gate 3 exemption evidence (pod re-check, sibling and
	//     occupant pod lists, PVC and StorageClass reads), because a
	//     stale read there could wrongly unlock deletion.
	//
	// Falls back to Client when nil (tests).
	Reader client.Reader
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
	Manager                    multicluster.Manager
	Timeout                    time.Duration
	Selector                   labels.Selector
	AllowRebinding             bool
	DisableStuckClaimExemption bool
}

// claimListForEvent renders a claim-name list for an Event note,
// capped by BOTH name count and total rendered length (claim names
// can legally reach 253 characters, so a count cap alone does not
// bound the note). events.k8s.io/v1 rejects notes longer than 1024
// characters and the events broadcaster silently DROPS the rejected
// Event, so an unbounded list would erase the paper trail in exactly
// the largest incidents. Logs carry the full list.
func claimListForEvent(names []string) string {
	const maxNames = 8
	const maxChars = 700
	n, chars := 0, 0
	for _, name := range names {
		if n == maxNames || chars+len(name) > maxChars {
			break
		}
		n++
		chars += len(name) + 1
	}
	if n == len(names) {
		return fmt.Sprintf("%v", names)
	}
	return fmt.Sprintf("%v (+%d more)", names[:n], len(names)-n)
}

// recordGateDeferred increments the gate-defer metric and, when a
// Recorder is set, writes an Event on the Pod. The metric always
// runs; operators alert on it to notice silent inaction.
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
		Client:                     k8sCluster.GetClient(),
		Timeout:                    r.Timeout,
		Selector:                   r.Selector,
		AllowRebinding:             r.AllowRebinding,
		DisableStuckClaimExemption: r.DisableStuckClaimExemption,
		ClusterName:                req.ClusterName,
		Recorder:                   k8sCluster.GetEventRecorder("pvc-unbinder"),
		Reader:                     k8sCluster.GetAPIReader(),
	}
	return c.Reconcile(ctx, req.Request)
}

// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get
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

// Reconcile runs the algorithm described on [Controller]: it checks
// the five safety gates in order (see the gate constants) and, if all
// pass, performs the unbind steps. It aims to be idempotent. Because
// Kubernetes has no transactions, it takes an early snapshot, guards
// every delete with UID/ResourceVersion preconditions, and re-queues
// on conflicts. If it crashes half-way, the durable in-flight PV
// annotations let Gate 0 hold siblings back and let the same pod
// resume its own unbind.
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

	// The cached read above is only a cheap pre-filter. Everything
	// after this point must be justified by true API-server state. A
	// stale cache copy of a Pod that was already recreated or
	// scheduled could still look stuck, grant its own exemption, and
	// reach the PVC deletes — and the delete preconditions guard the
	// claims, not the Pod evidence. So: re-read the Pod uncached,
	// qualify it again, and let the fresh object drive the rest.
	//
	// The re-read decodes into a FRESH object. Decoding into the
	// cache-populated one would merge (JSON decode semantics): fields
	// the fresh response omits — say, a just-recreated pod's still
	// empty status.conditions — would keep their stale cached values
	// and defeat the re-qualification below.
	var freshPod corev1.Pod
	if err := r.reader().Get(ctx, req.NamespacedName, &freshPod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	pod = freshPod
	if ok, requeueAfter := r.ShouldRemediate(ctx, &pod); !ok || requeueAfter > 0 {
		logger.Info("Pod no longer qualifies on the uncached re-read; skipping", "name", pod.Name, "ok", ok, "requeue-after", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter, Requeue: ok}, nil
	}

	// Gates 0 and 4 share one uncached scan over the cluster's
	// annotated PVs. Uncached, because the annotations are written
	// moments before they are read; durable, so the gates survive
	// restarts and leader handoffs mid-unbind.
	pvGates, err := r.checkPVGates(ctx, r.clusterKey(&pod), &pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Gate 0 "in-flight": a previous unbind in this cluster has not
	// finished — some PV still carries the in-flight annotation and
	// its recorded claim is not yet recreated and bound. This also
	// covers partial failures, because the annotation is written
	// before the first destructive step.
	if pvGates.unbindInFlight {
		const msg = "a previous unbind for this cluster has not settled; deferring"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gateInFlight, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
	}

	// Gate 1 "pause": the parent CR carries [PauseAnnotation].
	// Operators set it around planned events like cluster upgrades.
	if paused, err := r.isClusterPaused(ctx, &pod); err != nil {
		return ctrl.Result{}, err
	} else if paused {
		const msg = "parent CR is paused via annotation; skipping"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gatePause, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
	}

	// Gate 2 "multi-node": stuck pods are pinned to more than one
	// distinct node. That looks like a cluster-wide event (control
	// plane upgrade, AZ problem), not a single node failure, so wait
	// for natural recovery. Distinct NODES are counted, not pods:
	// several pods on one dead node is still a single-node failure
	// and the unbinder should act on it. Known gap: two deadlocked
	// victims pinned to two different occupied nodes also defer here
	// and need manual PVC deletion.
	if multiNode, err := r.multiNodeEventInProgress(ctx); err != nil {
		return ctrl.Result{}, err
	} else if multiNode {
		const msg = "stuck Pods are pinned to multiple nodes; deferring as a likely K8s-wide event"
		logger.Info(msg, "name", pod.Name)
		r.recordGateDeferred(&pod, gateMultiNode, msg)
		return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
	}

	// Gate 3 "pvc-rebinding": some PVC in this cluster is not bound
	// yet (empty spec.volumeName). Usually that means a re-bind is in
	// progress, so wait. Gate 0 already covers unbinds WE performed;
	// this gate also catches unbound claims from external actors, for
	// example an admin deleting a PVC by hand. The list is a cached
	// read: a false pass is backstopped by Gate 0 for our own actions
	// and a false defer only costs 30 seconds.
	//
	// Exception: claims owned by provably deadlocked Pods are exempt
	// (see [Controller.stuckClaimNames]). Waiting on such a claim
	// waits forever — under WaitForFirstConsumer it binds only after
	// its Pod schedules, and the Pod schedules only after the unbinder
	// frees its mis-pinned sibling claim, which is the very action
	// this gate would defer. Typical case: a fresh cluster where a
	// broker's cache PV landed on a full node; the datadir claim then
	// waits forever. Gate 0 still serializes the destructive work.
	//
	// Under --allow-pv-rebinding there is NO exemption: freed PVs
	// float as binding candidates, and acting while any claim is
	// unbound could pair it with the wrong disk (INC-2818).
	clusterPVCsByName, err := r.listClusterPVCsByName(ctx, r.Client, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}
	var unbound []string
	for name, pvc := range clusterPVCsByName {
		if pvc.Spec.VolumeName == "" {
			unbound = append(unbound, name)
		}
	}
	// Sorted so that with several unbound claims, consecutive
	// reconciles name the SAME gating claim in the log and Event
	// (map iteration order would flap the message every 30s, and the
	// events API dedups by message content).
	slices.Sort(unbound)
	if len(unbound) > 0 {
		// The exemption evidence runs lazily — only when some claim is
		// actually unbound — so the common all-bound path costs no
		// extra live reads. If the evidence reads fail (for example a
		// 403 when RBAC lags an image upgrade), the error downgrades
		// to the conservative deferral instead of error-looping the
		// reconcile. That direction is fail-safe: it disables a
		// permission, it never grants one. Context cancellation is not
		// downgraded; it surfaces as an error.
		exemptClaims := map[string]struct{}{}
		podMispinned := false
		if !r.AllowRebinding && !r.DisableStuckClaimExemption {
			if exemptClaims, podMispinned, err = r.stuckClaimNames(ctx, &pod, unbound); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctrl.Result{}, ctxErr
				}
				logger.Error(err, "failed to compute Gate 3 stuck-claim exemptions; keeping the conservative deferral", "name", pod.Name)
				exemptClaims = map[string]struct{}{}
			}
		}
		var exempted []string
		for _, name := range unbound {
			if _, stuck := exemptClaims[name]; stuck {
				exempted = append(exempted, name)
				continue
			}
			msg := fmt.Sprintf("PVC %q has no volumeName yet; deferring", name)
			logger.Info(msg, "name", pod.Name, "pvc", name)
			r.recordGateDeferred(&pod, gatePVCRebinding, msg)
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
		// Every unbound claim was exempted. Exemptions break the
		// stuck-Pod deadlock; they must not authorize destroying an
		// unrelated Pod. If the reconciled Pod has no mis-pinned bound
		// claim of its own (it is Pending for some other reason, like
		// CPU pressure), a sibling's deadlock must not unlock deleting
		// this Pod's healthy claims. Both intended victims — the
		// deadlocked broker and the dead-node broker — pass this check
		// naturally.
		if !podMispinned {
			logger.Info(fmt.Sprintf("unbound claims %v are exempted, but the reconciled Pod lacks its own mis-pin proof; deferring", exempted), "name", pod.Name)
			r.recordGateDeferred(&pod, gatePVCRebinding, fmt.Sprintf("unbound claims %s are exempted, but the reconciled Pod lacks its own mis-pin proof; deferring", claimListForEvent(exempted)))
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
		// The cached list above can be stale in BOTH directions.
		// Extra cached-only unbound claims merely cost a 30s
		// deferral, but a LIVE unbound claim the cache has not seen
		// yet must not slip past the gate on the exemptions' back.
		// Passing the gate is an exemption-granting decision, so it
		// is confirmed against an uncached re-list: any live unbound
		// claim outside the exempted set defers as usual, and a
		// failed re-list defers conservatively (same fail-safe
		// direction as the evidence chain).
		livePVCsByName, err := r.listClusterPVCsByName(ctx, r.reader(), &pod)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctrl.Result{}, ctxErr
			}
			logger.Error(err, "failed to confirm the exempted claims against the live API server; keeping the conservative deferral", "name", pod.Name)
			r.recordGateDeferred(&pod, gatePVCRebinding, "uncached PVC re-list failed; deferring")
			return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
		}
		liveUnbound := make([]string, 0, len(livePVCsByName))
		for name, pvc := range livePVCsByName {
			if pvc.Spec.VolumeName == "" {
				liveUnbound = append(liveUnbound, name)
			}
		}
		slices.Sort(liveUnbound)
		for _, name := range liveUnbound {
			if _, stuck := exemptClaims[name]; !stuck {
				msg := fmt.Sprintf("PVC %q has no volumeName on the live API server; deferring", name)
				logger.Info(msg, "name", pod.Name, "pvc", name)
				r.recordGateDeferred(&pod, gatePVCRebinding, msg)
				return ctrl.Result{RequeueAfter: requeueDuringDisruption}, nil
			}
		}
		// A safety gate is being overridden. Leave the same paper
		// trail a deferral gets: metric, Event, and log, naming the
		// exempted claims. Recorded only when the reconcile really
		// proceeds: the freed-pv gate below is durable and can hold
		// for days, and counting a "pass" every 30s during that hold
		// would poison the metric. The Event is a Warning — it
		// precedes destructive deletion, and Warning is what event
		// pipelines filter for.
		if !pvGates.freedPVUnresolved {
			logger.Info(fmt.Sprintf("unbound claims %v are exempted as stuck-Pod claims and the reconciled Pod holds its own mis-pin proof; proceeding past the pvc-rebinding gate", exempted), "name", pod.Name)
			observability.PVCUnbinderGateExempted.Inc()
			if r.Recorder != nil {
				msg := fmt.Sprintf("unbound claims %s are exempted as stuck-Pod claims and the reconciled Pod holds its own mis-pin proof; proceeding past the pvc-rebinding gate", claimListForEvent(exempted))
				r.Recorder.Eventf(&pod, nil, corev1.EventTypeWarning, eventReasonGateExempted, "Exempt", "%s", msg)
			}
		}
	}

	// Gate 4 "freed-pv": a PV we freed earlier (--allow-pv-rebinding)
	// is still Available and its node still exists, so a new claim
	// could bind to the wrong disk. Wait until the disk re-binds or
	// its node is gone. See [FreedPVAnnotation].
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

// checkPVGates evaluates Gates 0 and 4 in one uncached pass over the
// PV list.
//
// Gate 0: for each PV with [InFlightAnnotation] == clusterKey, fetch
// its recorded claim. The unbind has settled — and the annotations
// are cleared — once the claim is either recreated (new UID) and
// bound, or still carries the old UID and is not Terminating (the
// delete never happened; pre-unbind state, safe to retry from). A
// missing, Terminating, or recreated-but-unbound claim keeps
// unbindInFlight set.
//
// Gate 4: for each PV with [FreedPVAnnotation] == clusterKey:
// Bound again → clear the annotation. Available with its pinned node
// still existing → a live rebinding candidate; freedPVUnresolved is
// set. Available with the node gone → inert; not blocking, but the
// annotation is kept in case the node name is reused.
//
// The reconciled Pod's OWN in-flight claims do not block: this
// reconcile is exactly the retry that finishes a stuck unbind (the
// pod delete is what releases a claim held by the pvc-protection
// finalizer). Counting them would deadlock. Siblings still defer.
//
// If a freed PV never re-binds, or an in-flight claim is never
// recreated, these gates hold the cluster forever. That is on
// purpose: an alertable halt (metric + Event) is better than a silent
// disk swap. Operators fix it by removing the orphaned PV or the
// annotation.
//
// clusterKey == "" (pod without the instance label) engages no gates.
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
// [InFlightClaimAnnotation] is done unbinding (uncached read). Two
// states count as settled:
//
//   - the claim was recreated (new UID) and is bound — the unbind
//     completed; or
//   - the claim still has the OLD UID and is not Terminating — the
//     previous reconcile failed before it deleted anything, so the
//     world is still in its pre-unbind state and safe to retry from.
//     Without this case a failed delete would deadlock Gate 0.
//
// Everything else (claim missing, Terminating, or recreated but not
// bound) is an unbind in progress. A malformed annotation counts as
// not settled.
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

	hostname := NodeFromPVAffinity(pv)
	if hostname == "" {
		// Can't resolve the pinned node; be conservative — an
		// Available freed PV we can't classify is treated as a live
		// candidate.
		return true, nil
	}

	// Resolve the pinned node by the kubernetes.io/hostname LABEL,
	// never the Node object name — kubelet --hostname-override makes
	// them differ, and a name-based Get would report a live node as
	// gone and OPEN this gate. Mirrors the exemption path
	// ([Controller.nodeUnavailableForScheduling]).
	var nodeList corev1.NodeList
	if err := r.reader().List(ctx, &nodeList, client.MatchingLabels{corev1.LabelHostname: hostname}); err != nil {
		// Out-of-band RBAC can lag the upgrade that introduced this
		// LIST (the lookup it replaced needed only `get`). Degrade
		// Forbidden to the conservative answer — a live candidate, so
		// the gate stays engaged and defers with its usual paper
		// trail — instead of error-looping the reconcile with no
		// metric or Event.
		if apierrors.IsForbidden(err) {
			log.FromContext(ctx).Info("nodes LIST forbidden; treating the freed PV as a live rebinding candidate", "name", pv.Name, "reason", err.Error())
			return true, nil
		}
		return false, err
	}
	if len(nodeList.Items) == 0 {
		// Node permanently gone; PV is inert. Keep annotation in case
		// of node-name reuse, but don't defer on it.
		return false, nil
	}
	// A node with this hostname exists (Ready or not — cordoned and
	// NotReady nodes can recover and bind). Live candidate; defer.
	return true, nil
}

// ShouldRemediate reports whether a Pod qualifies for remediation: it
// matches the Selector, is a Pending StatefulSet pod, and its
// Unschedulable condition matches the scheduling-failure signature.
// If the condition is younger than Timeout, it returns (true, wait):
// qualified, but re-check after `wait` in case the scheduler settles
// it on its own.
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

	// The message check is deliberately weak. Schedulers stopped
	// naming volume node affinity in the message somewhere between
	// K8s 1.21 and 1.28 (exact version never tracked down), so we
	// accept either an explicit mention or any "0/N nodes are
	// available" total failure. Stronger proof comes later, from the
	// exemption evidence chain, not from message text.
	if !SchedulingFailureRE.MatchString(cond.Message) {
		log.FromContext(ctx).Info("scheduling failure does not appear to indicate volume affinity issues; skipping", "name", pod.Name, "condition", cond)
		return false, 0
	}

	if delta := r.Timeout - time.Since(cond.LastTransitionTime.Time); delta > 0 {
		return true, delta
	}

	return true, 0
}

// pvcUnbinderPredicate is the cheap event filter: only Pending Pods
// owned by a StatefulSet are interesting to this controller.
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

// clusterKey identifies the Redpanda cluster a Pod belongs to. It is
// the value written into the gate annotations, scoping Gates 0 and 4
// per cluster. Returns "" when the Pod has no
// app.kubernetes.io/instance label; the per-cluster gates are then
// skipped. The ClusterName prefix keeps keys unique in multicluster
// mode.
func (r *Controller) clusterKey(pod *corev1.Pod) string {
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return ""
	}
	return r.ClusterName + "/" + pod.Namespace + "/" + instance
}

// isClusterPaused reports whether the Pod's owning CR carries
// [PauseAnnotation] = "true". The Pod is linked to its CR by the
// app.kubernetes.io/instance label. Three CR types are checked:
// v1alpha2.Redpanda, v1alpha2.StretchCluster (a member's broker pods
// carry the StretchCluster's name in the instance label), and the
// legacy v1alpha1.Cluster. Errors that only mean "this type is not reachable
// here" (CR absent, CRD not installed, type not in scheme) are
// ignored so the same code runs in every operator binary.
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

// multiNodeEventInProgress reports whether stuck Redpanda broker pods
// are pinned to more than one distinct node (Gate 2). If so, the
// symptom looks like a cluster-wide event and the unbinder defers.
// Nodes are counted, not pods: several pods on one dead node is a
// single-node failure the unbinder should act on. The pod list is
// limited to broker pods (see the label constants) so unrelated
// workloads cannot trip this gate. Unresolvable PVs are skipped. This
// gate is best-effort by design (cached reads); safety rests on
// Gates 0, 3, and 4.
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
		hostname := NodeFromPVAffinity(pv)
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
		if !PodHasVolumeAffinityUnschedulable(other) {
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

// NodeFromPVAffinity returns the single hostname a PV's NodeAffinity
// pins it to, for Gate 2's per-node bucketing. Only the shape that
// Local/HostPath volumes use is recognized: one kubernetes.io/hostname
// `In` selector with one value. Anything else returns "" and simply
// does not contribute to Gate 2's count (Gate 2 is best-effort; the
// exemption chain uses the stricter [pvPinnedHostnames] instead).
func NodeFromPVAffinity(pv *corev1.PersistentVolume) string {
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

// DeadNodePVCs returns the PVCs attached to the pod whose bound PVs are
// pinned (HostPath or Local volume with NodeAffinity) to nodes that no
// longer exist. As a side effect, each affected PV's reclaim policy is
// patched to Retain so the storage survives PVC deletion.
//
// PVC names listed in exclude are skipped entirely — they are neither
// returned nor Retain-patched. Callers that will not remediate a claim
// (e.g. externally-managed ExistingClaims) must exclude it here: flipping
// the reclaim policy of a PV the caller then declines to touch would
// silently override an admin's `Delete` policy and strand Released volumes.
//
// apiReader must be an uncached client for accurate Node existence checks.
func DeadNodePVCs(ctx context.Context, c client.Client, apiReader client.Reader, pod *corev1.Pod, exclude ...string) ([]corev1.PersistentVolumeClaim, error) {
	l := log.FromContext(ctx)
	var affected []corev1.PersistentVolumeClaim
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].PersistentVolumeClaim == nil {
			continue
		}
		if slices.Contains(exclude, pod.Spec.Volumes[i].PersistentVolumeClaim.ClaimName) {
			continue
		}
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, client.ObjectKey{
			Name:      pod.Spec.Volumes[i].PersistentVolumeClaim.ClaimName,
			Namespace: pod.Namespace,
		}, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if pvc.Spec.VolumeName == "" {
			continue
		}
		var pv corev1.PersistentVolume
		if err := c.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
			return nil, err
		}
		if pv.Spec.HostPath == nil && pv.Spec.Local == nil {
			continue
		}
		nodeName := NodeFromPVAffinity(&pv)
		if nodeName == "" {
			continue
		}
		var node corev1.Node
		if err := apiReader.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}
		} else {
			continue
		}
		if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
			patch := client.StrategicMergeFrom(pv.DeepCopy(), &client.MergeFromWithOptimisticLock{})
			pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
			if err := c.Patch(ctx, &pv, patch); err != nil {
				return nil, fmt.Errorf("patching PV %s to Retain: %w", pv.Name, err)
			}
			l.Info("patched PV to Retain", "pv", pv.Name, "deadNode", nodeName)
		}
		affected = append(affected, pvc)
	}
	return affected, nil
}

// listClusterPVCsByName returns a name→PVC snapshot for the PVCs that
// belong to the same Redpanda/Cluster as `pod` (matched by the
// app.kubernetes.io/instance label). Gate 3 inspects spec.volumeName
// on each entry to detect a PVC that's not yet bound to a PV. The
// caller picks the reader: cached for the deferral fast path, the
// uncached APIReader when the answer helps grant passage.
//
// Returns an empty (non-nil) map when the Pod has no instance label.
func (r *Controller) listClusterPVCsByName(ctx context.Context, reader client.Reader, pod *corev1.Pod) (map[string]corev1.PersistentVolumeClaim, error) {
	out := map[string]corev1.PersistentVolumeClaim{}
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return out, nil
	}
	var pvcList corev1.PersistentVolumeClaimList
	if err := reader.List(ctx, &pvcList, &client.ListOptions{
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

// stuckClaimNames computes Gate 3's exemption set: the names of
// claims that are unbound BECAUSE their Pod is provably deadlocked,
// so waiting on them would wait forever. It also returns whether the
// reconciled Pod itself holds a mis-pinned bound claim (the caller
// re-uses that as the own-proof check before destruction).
//
// Why exempt at all: under WaitForFirstConsumer a stuck Pod's claim
// binds only after the Pod schedules, and the Pod schedules only
// after the unbinder frees its mis-pinned claim — the very action
// Gate 3 would defer. The exemption is symmetric across victims so
// two mis-pinned brokers do not defer on each other; Gate 0 still
// serializes the destructive work.
//
// No Pod is exempted for free. The reconciled Pod and every sibling
// must prove the full deadlock shape via
// [Controller.exemptClaimNames]; a Pod that only matches the weak
// `schedulingFailureRE` message (it may be stuck on CPU, quota, or a
// provisioner failure) proves nothing. A sibling must also pass
// [Controller.ShouldRemediate] in full — same Selector, same
// predicate, and the same r.Timeout freshness check — because a
// sibling that turned Pending only seconds ago may still resolve on
// its own, and Gate 0 has no annotation yet to backstop acting early
// on its behalf.
//
// All reads here are uncached. A lagging informer could keep showing
// a sibling as stuck after it was actually recreated or scheduled,
// and that stale view must not re-create an exemption for a claim
// that is now genuinely settling.
//
// Threat note: any principal with pods/create in this namespace can
// forge a "stuck sibling" (ownerReferences, volumes, affinity, and an
// impossible resource request are all under its control). The mis-pin
// proof itself is also partly self-supplied — the tolerations and
// anti-affinity terms it consults come from the pod's own spec — so
// the evidence chain must never be treated as tamper-resistant
// against such a principal. What actually bounds the damage is scope
// confinement, not the proof: the pipeline deletes only the
// RECONCILED Pod's own claims, and only after that Pod proves its own
// mis-pin.
//
// Claims with no Pod at all (for example, orphaned by an aborted
// scale-up) always defer. Names need no namespace: every list here is
// scoped to pod.Namespace.
func (r *Controller) stuckClaimNames(ctx context.Context, pod *corev1.Pod, unbound []string) (map[string]struct{}, bool, error) {
	// The reconciled pod's mis-pin proof is computed exactly once and
	// returned to the caller, which needs it again after the Gate 3
	// loop (the own-proof check before destruction).
	podMispinned, err := r.podHasMispinnedBoundClaim(ctx, pod)
	if err != nil {
		return nil, false, err
	}
	out := map[string]struct{}{}
	if podMispinned {
		own, err := r.unboundWFFCClaimNames(ctx, pod)
		if err != nil {
			return nil, false, err
		}
		for name := range own {
			out[name] = struct{}{}
		}
	}
	instance := pod.Labels[operatorlabels.InstanceKey]
	if instance == "" {
		return out, podMispinned, nil
	}
	var podList corev1.PodList
	if err := r.reader().List(ctx, &podList, &client.ListOptions{
		Namespace: pod.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			operatorlabels.InstanceKey: instance,
		}),
	}); err != nil {
		return nil, false, err
	}
	for i := range podList.Items {
		p := &podList.Items[i]
		if p.Name == pod.Name {
			// Already evaluated above; re-running the evidence chain
			// would double the live API-server reads for nothing.
			continue
		}
		// A sibling can only exempt its own claims, so a sibling that
		// owns none of the unbound claims cannot change the outcome.
		// Skipping it avoids a full evidence run (PVC Gets, node
		// LISTs, occupant LISTs) per stuck-but-irrelevant pod — which
		// would otherwise repeat every 30s against the live API
		// server during an incident.
		if !slices.ContainsFunc(StsPVCs(p), func(key client.ObjectKey) bool {
			return slices.Contains(unbound, key.Name)
		}) {
			continue
		}
		// Tag the sibling-qualification logs: ShouldRemediate's
		// "skipping" lines would otherwise print the sibling's name in
		// the reconciled pod's context every 30s and read as
		// remediation decisions about the sibling rather than
		// exemption-evidence checks.
		sibCtx := log.IntoContext(ctx, log.FromContext(ctx).WithValues("phase", "gate3-exemption", "sibling", p.Name))
		if ok, requeueAfter := r.ShouldRemediate(sibCtx, p); !ok || requeueAfter > 0 {
			continue
		}
		exempt, err := r.exemptClaimNames(sibCtx, p)
		if err != nil {
			return nil, false, err
		}
		for name := range exempt {
			out[name] = struct{}{}
		}
	}
	return out, podMispinned, nil
}

// exemptClaimNames returns the pod's own claims that qualify for the
// Gate 3 exemption. Two conditions, both required:
//
//  1. the pod holds a Bound claim on a HostPath/Local PV whose pinned
//     node is provably unavailable ([Controller.podHasMispinnedBoundClaim]
//     — the claim that actually causes the deadlock); and
//  2. the returned claims are the pod's unbound claims that use a
//     WaitForFirstConsumer StorageClass. A claim unbound under
//     Immediate binding signals a provisioning failure, not this
//     deadlock, and keeps deferring.
//
// PVC reads are uncached: this evidence opens a gate in front of
// destructive deletion, so it must reflect true API-server state.
//
// Deliberately NOT required: that the Pod's Pending message names
// volume affinity. The mis-pin proof stands on its own — a Bound
// local PV confines the Pod to one node, and if that node is proven
// unavailable the Pod cannot schedule there, whatever the aggregate
// scheduler message blames on other nodes. Kubernetes does not
// reliably name volume affinity in the message (the production
// incident's message never did), so requiring it would re-open the
// exact gap this exemption closes. Freeing a claim that is dead-ended
// on an unavailable node is never harmful; at worst it is not enough
// by itself.
func (r *Controller) exemptClaimNames(ctx context.Context, pod *corev1.Pod) (map[string]struct{}, error) {
	mispinned, err := r.podHasMispinnedBoundClaim(ctx, pod)
	if err != nil {
		return nil, err
	}
	if !mispinned {
		return map[string]struct{}{}, nil
	}
	return r.unboundWFFCClaimNames(ctx, pod)
}

// unboundWFFCClaimNames returns the names of pod's own StatefulSet
// claims that are unbound AND use a WaitForFirstConsumer StorageClass.
// It is the claim-collection half of [Controller.exemptClaimNames];
// callers must establish the mis-pin proof first. PVC reads are
// uncached (they feed an exemption decision).
func (r *Controller) unboundWFFCClaimNames(ctx context.Context, pod *corev1.Pod) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, key := range StsPVCs(pod) {
		var pvc corev1.PersistentVolumeClaim
		if err := r.reader().Get(ctx, key, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if pvc.Spec.VolumeName != "" {
			continue
		}
		if wffc, err := r.claimUsesWaitForFirstConsumer(ctx, &pvc); err != nil {
			return nil, err
		} else if wffc {
			out[key.Name] = struct{}{}
		}
	}
	return out, nil
}

// podHasMispinnedBoundClaim is the mis-pin proof: it reports whether
// the pod holds a Bound claim on a HostPath/Local PV (the only shape
// the unbinder ever acts on) whose EVERY eligible node is unavailable
// to the pod. This is what makes a Pending pod "provably deadlocked"
// instead of merely stuck: nearly every broker holds a bound local
// claim, and the weak scheduling message also fires on CPU or quota
// failures, so the shape alone proves nothing — the node must be
// proven unavailable too.
//
// The PV's NodeAffinity can accept several nodes ([pvPinnedHostnames]).
// If even one of them is available, the pod's failure to schedule
// cannot be blamed on this claim, so it is not proof. A PV whose node
// set cannot be fully resolved is skipped, never guessed at.
//
// "Bound" is proven by the PV's ClaimRef back-reference (namespace,
// name, and UID all matching the claim), never by the claim's
// volumeName alone — that field is user-settable at creation.
//
// The PVC read is uncached: a stale volumeName pointing at an old PV
// would fabricate the evidence. The PV read stays cached because the
// fields used (NodeAffinity, HostPath/Local, ClaimRef UID) never
// change on a live Bound PV.
func (r *Controller) podHasMispinnedBoundClaim(ctx context.Context, pod *corev1.Pod) (bool, error) {
	for _, key := range StsPVCs(pod) {
		var pvc corev1.PersistentVolumeClaim
		if err := r.reader().Get(ctx, key, &pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		if pvc.Spec.VolumeName == "" {
			continue
		}
		var pv corev1.PersistentVolume
		if err := r.Client.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		// The reference must be a real two-way binding. volumeName is
		// user-settable at claim creation (static pre-binding), so on
		// its own it proves nothing: a claim pre-pointed at an
		// arbitrary local PV must not mint mis-pin evidence. Only the
		// binder completes the back-reference with the claim's UID.
		// (The destructive pipeline filters PVs by ClaimRef
		// namespace/name only; the UID match here is deliberately
		// stricter, and Gate 0's settle check self-heals any
		// stale-UID PV it stamps.)
		if pv.Spec.ClaimRef == nil ||
			pv.Spec.ClaimRef.Namespace != pvc.Namespace ||
			pv.Spec.ClaimRef.Name != pvc.Name ||
			pv.Spec.ClaimRef.UID != pvc.UID {
			continue
		}
		if pv.Spec.NodeAffinity == nil || (pv.Spec.HostPath == nil && pv.Spec.Local == nil) {
			continue
		}
		hostnames, ok := pvPinnedHostnames(&pv)
		if !ok {
			continue
		}
		allUnavailable := true
		for _, hostname := range hostnames {
			unavailable, err := r.nodeUnavailableForScheduling(ctx, hostname, pod)
			if err != nil {
				return false, err
			}
			if !unavailable {
				allUnavailable = false
				break
			}
		}
		if allUnavailable {
			return true, nil
		}
	}
	return false, nil
}

// pvPinnedHostnames returns every hostname the PV's Required
// NodeAffinity accepts (terms are OR'd, so their hostname values are
// unioned), plus ok=false when the set cannot be trusted as complete.
//
// ok is false when there is no Required NodeAffinity, or when any
// term is more complex than exactly one "kubernetes.io/hostname In
// [values]" expression (extra expressions, MatchFields, other keys or
// operators). A partial answer would understate where the PV can
// bind, so the caller must treat ok=false as "cannot evaluate", never
// as "no eligible nodes".
//
// This is the strict counterpart of [nodeFromPVAffinity]: that one
// serves best-effort gates; this one backs a deletion decision, where
// collapsing several eligible nodes into one could delete a claim
// that would still bind fine elsewhere.
func pvPinnedHostnames(pv *corev1.PersistentVolume) ([]string, bool) {
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return nil, false
	}
	terms := pv.Spec.NodeAffinity.Required.NodeSelectorTerms
	if len(terms) == 0 {
		return nil, false
	}
	var hostnames []string
	for _, term := range terms {
		if len(term.MatchFields) > 0 || len(term.MatchExpressions) != 1 {
			return nil, false
		}
		expr := term.MatchExpressions[0]
		if expr.Key != corev1.LabelHostname || expr.Operator != corev1.NodeSelectorOpIn || len(expr.Values) == 0 {
			return nil, false
		}
		hostnames = append(hostnames, expr.Values...)
	}
	return hostnames, true
}

// taintNodeNotReady and taintNodeUnreachable are the taints the node
// lifecycle controller puts on a Node whose Ready condition goes
// False (not-ready) or Unknown (unreachable).
const (
	taintNodeNotReady    = corev1.TaintNodeNotReady
	taintNodeUnreachable = corev1.TaintNodeUnreachable
)

// nodeUnavailableForScheduling reports whether the node behind
// `hostname` (one of the hostnames a mis-pinned PV accepts) is truly
// unable to host pod — the fact that turns "stuck" into "deadlocked".
//
// The node is found by LISTING Nodes with a matching
// kubernetes.io/hostname label, not by name: NodeAffinity matches the
// label, and the label does not have to equal the object name
// (--hostname-override, manual relabels). Zero matches means the node
// is gone → unavailable. More than one match is a misconfiguration
// this function refuses to interpret → available (fail closed).
//
// A single matching node is unavailable when any of these holds:
//
//   - it is cordoned (Spec.Unschedulable);
//   - its Ready condition is False/Unknown, or it carries the
//     not-ready/unreachable taint — in both forms judged through the
//     pod's own tolerations (Ready=False maps to the not-ready taint,
//     Ready=Unknown to unreachable). An unconditional toleration (no
//     TolerationSeconds) suppresses this leg. That is POLICY for
//     --broker-pod-node-unavailable-toleration=-1s deployments, whose
//     contract says only Node DELETION means permanent loss: the
//     scheduler might refuse the node right now (its NoSchedule twin
//     taint is not covered by the injected NoExecute tolerations),
//     but a transient partition must never justify deleting data. Do
//     not "fix" this to match raw scheduler semantics. Grace-period
//     tolerations (finite TolerationSeconds, auto-injected on every
//     pod) do NOT suppress the leg — they describe eviction timing,
//     not node health;
//   - a live pod already on the node matches one of pod's own
//     REQUIRED anti-affinity terms ([podRequiredAntiAffinityMatches])
//     — the production-incident shape, where a broker's PV landed on
//     a node another broker occupies. Candidate occupants are every
//     pod in the pod's own namespace (the only scope interpretable
//     terms can name); the term's own LabelSelector decides which of
//     them conflict. Occupancy alone proves nothing (soft or custom
//     anti-affinity allows co-location), and Terminating or
//     Succeeded/Failed pods do not count as occupants.
//
// If none of these hold, the node looks schedulable, so the pod's
// Pending state cannot be blamed on this claim (more likely CPU,
// quota, or unrelated taints) and this returns false.
//
// Every read here is uncached. This evidence directly unlocks
// destructive deletion, and a stale occupant or node view could
// manufacture proof of a conflict that no longer exists. Gate 0 does
// not backstop that: it only tracks the unbinder's OWN past actions.
func (r *Controller) nodeUnavailableForScheduling(ctx context.Context, hostname string, pod *corev1.Pod) (bool, error) {
	var nodeList corev1.NodeList
	if err := r.reader().List(ctx, &nodeList, client.MatchingLabels{corev1.LabelHostname: hostname}); err != nil {
		return false, err
	}
	if len(nodeList.Items) == 0 {
		return true, nil
	}
	if len(nodeList.Items) > 1 {
		return false, nil
	}
	node := nodeList.Items[0]
	if node.Spec.Unschedulable {
		return true, nil
	}
	for _, cond := range node.Status.Conditions {
		if cond.Type != corev1.NodeReady || cond.Status == corev1.ConditionTrue {
			continue
		}
		// A not-True Ready condition is judged through the same
		// toleration lens as the taint it maps to (Ready=False maps to
		// the not-ready taint, Ready=Unknown to the unreachable
		// taint). This is a POLICY choice, not scheduler emulation:
		// the scheduler may in fact refuse to place the Pod on this
		// node right now (the NoSchedule twin taint is not covered by
		// the NoExecute-shaped tolerations that
		// --broker-pod-node-unavailable-toleration=-1s injects). But
		// that flag's contract says only Node-object DELETION signals
		// permanent loss, so for such Pods a transiently unreachable
		// node must never count as proof that justifies deleting
		// data. Do not "fix" this to match raw scheduler semantics —
		// that would convert transient partitions into PVC deletion
		// for exactly the deployments that opted out of it.
		key := taintNodeNotReady
		if cond.Status == corev1.ConditionUnknown {
			key = taintNodeUnreachable
		}
		if !podUnconditionallyTolerates(pod.Spec.Tolerations, &corev1.Taint{Key: key, Effect: corev1.TaintEffectNoExecute}) {
			return true, nil
		}
	}
	for i := range node.Spec.Taints {
		taint := &node.Spec.Taints[i]
		if taint.Key != taintNodeNotReady && taint.Key != taintNodeUnreachable {
			continue
		}
		// Judge through the canonical NoExecute lens regardless of the
		// taint's actual effect: the node lifecycle controller applies
		// these keys with BOTH NoExecute (eviction pass) and NoSchedule
		// (condition pass) effects on every NotReady/unreachable node,
		// while --broker-pod-node-unavailable-toleration injects
		// NoExecute-shaped tolerations only. Checking the raw NoSchedule
		// twin against those tolerations would fail the effect match
		// and mark the node unavailable on every real NotReady node —
		// silently defeating the tolerate-forever carve-out the
		// condition leg above implements. Both twins are applied and
		// removed together off the same Ready condition, so one lens
		// decides for the pair; and an untolerated NoExecute-lens check
		// still catches every pod the taints genuinely exclude.
		if !podUnconditionallyTolerates(pod.Spec.Tolerations, &corev1.Taint{Key: taint.Key, Effect: corev1.TaintEffectNoExecute}) {
			return true, nil
		}
	}
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil ||
		len(pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) == 0 {
		// No hard anti-affinity at all (nil affinity, soft-only
		// podAntiAffinity.type, or a custom/overridden affinity with
		// no required terms) — occupancy can't be evaluated as proof,
		// so skip the Pod LIST entirely.
		return false, nil
	}
	// Candidates are ALL pods in the pod's namespace, not just
	// same-instance ones. Interpretable terms are already restricted
	// to own-namespace scope, and the term's own LabelSelector decides
	// who conflicts — the scheduler rejects the node for a matching
	// occupant from ANY workload, so an instance-scoped list would
	// hide such occupants and silently withhold this proof leg for
	// custom terms that select beyond the release.
	var podList corev1.PodList
	if err := r.reader().List(ctx, &podList, &client.ListOptions{Namespace: pod.Namespace}); err != nil {
		return false, err
	}
	for i := range podList.Items {
		other := &podList.Items[i]
		if other.Name == pod.Name {
			continue
		}
		if other.DeletionTimestamp != nil {
			continue
		}
		if other.Status.Phase == corev1.PodSucceeded || other.Status.Phase == corev1.PodFailed {
			continue
		}
		if other.Spec.NodeName != node.Name {
			continue
		}
		if podRequiredAntiAffinityMatches(pod, other) {
			return true, nil
		}
	}
	return false, nil
}

// hostnameTopologyKey is the per-node topology key used by the
// redpanda chart's default hard anti-affinity. It is the only
// TopologyKey [podRequiredAntiAffinityMatches] accepts, because at
// node granularity "same domain" can be decided without reading node
// labels.
const hostnameTopologyKey = corev1.LabelHostname

// podRequiredAntiAffinityMatches reports whether one of pod's
// REQUIRED anti-affinity terms matches occupant, proving the shared
// node is off-limits for pod. Real PodAffinityTerm semantics are
// richer than a label match, so a term only counts when its full
// shape is one this function can interpret:
//
//   - TopologyKey is exactly [hostnameTopologyKey]. Any other key
//     would need node-label lookups to compare topology domains.
//   - NamespaceSelector is nil, and Namespaces is empty or names
//     exactly pod's own namespace. Both mean "this pod's namespace",
//     which is all the caller's namespace-scoped list can verify.
//     The explicit single-namespace form matters: the v1 Cluster's
//     default hard anti-affinity always sets it.
//   - MatchLabelKeys and MismatchLabelKeys are empty; their
//     dynamic-selector semantics are not implemented here.
//
// Any other term shape is skipped, never guessed at. Terms are
// judged by SHAPE, not by which chart option produced them: a
// statefulset.podAntiAffinity type "custom" term that happens to be
// hostname-scoped, own-namespace, and matchLabelKeys-free qualifies
// like the default "hard" one; soft (Preferred-only) anti-affinity
// yields no required terms and never qualifies. Skipping only makes
// Gate 3 keep deferring (alertable via the gate metric); it can never
// falsely open the gate. An invalid LabelSelector is skipped the same
// way.
func podRequiredAntiAffinityMatches(pod, occupant *corev1.Pod) bool {
	for _, term := range pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		if term.TopologyKey != hostnameTopologyKey {
			continue
		}
		if term.NamespaceSelector != nil {
			continue
		}
		if len(term.Namespaces) > 1 || (len(term.Namespaces) == 1 && term.Namespaces[0] != pod.Namespace) {
			continue
		}
		if len(term.MatchLabelKeys) > 0 || len(term.MismatchLabelKeys) > 0 {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
		if err != nil {
			continue
		}
		if selector.Matches(labels.Set(occupant.Labels)) {
			return true
		}
	}
	return false
}

// podUnconditionallyTolerates reports whether one of tolerations
// matches taint (per the standard Kubernetes toleration-match rules:
// empty Key/Effect act as wildcards, Operator Exists ignores Value,
// Operator Equal/"" requires it) AND carries no TolerationSeconds —
// i.e. the Pod tolerates the taint indefinitely, not just for a grace
// period before eviction. See [Controller.nodeUnavailableForScheduling]
// for why the grace-period form doesn't count.
func podUnconditionallyTolerates(tolerations []corev1.Toleration, taint *corev1.Taint) bool {
	for i := range tolerations {
		t := &tolerations[i]
		if t.TolerationSeconds != nil {
			continue
		}
		if t.Key != "" && t.Key != taint.Key {
			continue
		}
		if t.Effect != "" && t.Effect != taint.Effect {
			continue
		}
		switch t.Operator {
		case corev1.TolerationOpExists:
			return true
		case corev1.TolerationOpEqual, "":
			if t.Value == taint.Value {
				return true
			}
		}
	}
	return false
}

// claimUsesWaitForFirstConsumer reports whether pvc binds under a
// WaitForFirstConsumer StorageClass — the only mode in which a claim
// is EXPECTED to sit unbound while its Pod has not scheduled.
//
// The class is resolved exactly the way Kubernetes resolves it
// (mirrors component-helpers' GetPersistentVolumeClaimClass as of
// k8s.io/api v0.35.1; component-helpers is not a dependency, keep the
// copy in sync by hand): the legacy [corev1.BetaStorageClassAnnotation]
// wins whenever the KEY is present, even with an empty value; only
// when the key is absent does Spec.StorageClassName apply. This looks
// backwards but is what the PV controller does, so both fields on one
// claim must resolve the same way here.
//
// There is deliberately no fallback to the cluster's current default
// StorageClass. Defaulting happens once, at admission, by writing
// Spec.StorageClassName onto the object. A claim that still has nil
// there was never defaulted; guessing today's default could disagree
// with what was true at creation. A claim with no class binds only by
// static PV matching, immediately — never via WaitForFirstConsumer —
// so "no class" resolves to false. A named class that does not exist
// also resolves to false (unknown defers).
//
// The read is uncached: VolumeBindingMode only "changes" through
// delete-and-recreate under the same name, which is exactly what a
// lagging informer would hide. An uncached Get also keeps the RBAC
// grant at bare `get`.
func (r *Controller) claimUsesWaitForFirstConsumer(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	var name string
	if class, found := pvc.Annotations[corev1.BetaStorageClassAnnotation]; found {
		name = class
	} else if pvc.Spec.StorageClassName != nil {
		name = *pvc.Spec.StorageClassName
	} else {
		return false, nil
	}
	if name == "" {
		return false, nil
	}
	var sc storagev1.StorageClass
	if err := r.reader().Get(ctx, client.ObjectKey{Name: name}, &sc); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sc.VolumeBindingMode != nil && *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer, nil
}

// PodHasVolumeAffinityUnschedulable reports whether a Pod is Pending
// because the scheduler couldn't satisfy volume node affinity. Used by
// [Controller.multiNodeEventInProgress] and by the Broker controller to
// detect dead-node scenarios.
func PodHasVolumeAffinityUnschedulable(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodScheduled || cond.Status != corev1.ConditionFalse || cond.Reason != "Unschedulable" {
			continue
		}
		return SchedulingFailureRE.MatchString(cond.Message)
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
