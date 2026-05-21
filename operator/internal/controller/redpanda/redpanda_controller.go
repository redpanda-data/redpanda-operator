// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package redpanda contains reconciliation logic for cluster.redpanda.com CRDs
package redpanda

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

const (
	FinalizerKey = "operator.redpanda.com/finalizer"

	revisionPath = "/revision"

	// reqeueueTimeout is the time that the reconciler will
	// wait before requeueing a cluster to be reconciled when
	// we've explicitly aborted a reconciliation loop early
	// due to an in-progress operation
	requeueTimeout = 10 * time.Second

	// finalizerRequeueTimeout is the time that the reconciler
	// will wait before requeueing a reconciliation after
	// patching a finalizer
	finalizerRequeueTimeout = 1 * time.Second

	// periodicRequeue is the maximal period between re-examining
	// a cluster; this is used to ensure that we regularly reassert
	// cluster configuration (which may depend on external secrets,
	// for which there's no change event we can latch onto).
	periodicRequeue = 3 * time.Minute

	// defaultReconcileTimeout is a defense-in-depth ceiling on a single
	// reconcile pass when the reconciler struct leaves ReconcileTimeout at
	// zero. The preferred discipline is that every downstream call sets its
	// own context timeout; this wrapper exists because the reconciler
	// fans out to many external endpoints (peer K8s API, admin API,
	// etc.) and we've found several sites that historically had no bound
	// of their own. On deadline, the reconcile aborts with
	// context.DeadlineExceeded and controller-runtime requeues with
	// backoff. Sized to comfortably exceed a healthy p99 reconcile
	// (sub-second today) so healthy passes never hit it.
	defaultReconcileTimeout = 2 * time.Minute

	// the message when a cluster has no desired brokers
	messageNoBrokers = "Cluster has no desired brokers"
)

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	// KubeConfig is the [rest.Config] that provides the go helm chart
	// Kubernetes access. It should be the same config used to create client.
	Manager              multicluster.Manager
	LifecycleClient      *lifecycle.ResourceClient[lifecycle.ClusterWithPools, *lifecycle.ClusterWithPools]
	ClientFactory        internalclient.ClientFactory
	CloudSecretsExpander *pkgsecrets.CloudExpander
	UseNodePools         bool
}

// Any resource that the Redpanda helm chart creates and needs to reconcile.
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions,verbs=get;list;watch;
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;create;update;patch;delete;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;create;update;patch;delete;list;watch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors;servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch

// Console chart
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=consoles,verbs=get;list;watch;create;update;patch;delete

// redpanda resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// sidecar resources
// The leases is used by controller-runtime in sidecar. Operator main reconciliation needs to have leases permissions in order to create role that have the same permissions.
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaReconciler) SetupWithManager(ctx context.Context, mgr multicluster.Manager, namespace string) error {
	builder := mcbuilder.ControllerManagedBy(mgr)

	if err := r.LifecycleClient.WatchResources(builder, &redpandav1alpha2.Redpanda{}, mgr.GetClusterNames()); err != nil {
		return err
	}

	if r.UseNodePools {
		for _, clusterName := range mgr.GetClusterNames() {
			enqueueClusterFromNodePool := mchandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				pool := o.(*redpandav1alpha2.NodePool)
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name:      pool.Spec.ClusterRef.Name,
						Namespace: pool.Namespace,
					},
				}}
			})
			builder.Watches(&redpandav1alpha2.NodePool{}, enqueueClusterFromNodePool, controller.WatchOptions(clusterName)...)
		}
	}

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, observability.Wrap[mcreconcile.Request](r, "Redpanda", periodicRequeue)))
}

type clusterReconciliationState struct {
	cluster               *lifecycle.ClusterWithPools
	pools                 *lifecycle.PoolTracker
	status                *lifecycle.ClusterStatus
	restartOnConfigChange bool
	admin                 *rpadmin.AdminAPI
}

func (s *clusterReconciliationState) cleanup() {
	if s.admin != nil {
		s.admin.Close()
	}
}

type clusterReconciliationFn func(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error)

func (r *RedpandaReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	start := time.Now()
	l := log.FromContext(ctx).WithName("RedpandaReconciler.Reconcile")

	l.V(1).Info("Starting reconcile loop")
	defer func() {
		l.V(1).Info("Finished reconciling", "elapsed", time.Since(start))
	}()

	cluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		l.Error(err, "unable to fetch cluster, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	k8sClient := cluster.GetClient()

	rp := &redpandav1alpha2.Redpanda{}

	if err := k8sClient.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer func() {
		// If we have a resource to manage, ensure that we re-enqueue to re-examine it on a regular basis
		if err != nil {
			// Error returns cause a re-enqueuing this with exponential backoff
			return
		}

		if result.RequeueAfter > 0 {
			// We're already set up to enqueue this resource again
			return
		}

		result.RequeueAfter = periodicRequeue
	}()

	ctx, span := trace.Start(otelkube.Extract(ctx, rp), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	if !feature.V2Managed.Get(ctx, rp) {
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := k8sClient.Update(ctx, rp); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	state, err := r.fetchInitialState(ctx, rp, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer state.cleanup()

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, state.cluster); deleted || err != nil {
			return r.syncStatus(ctx, cluster, state, reconcile.Result{}, err)
		}
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := k8sClient.Update(ctx, rp); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Update our Redpanda with our finalizer and any default Annotation FFs.
	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(rp, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, rp) {
		if err := k8sClient.Update(ctx, rp); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	reconcilers := []clusterReconciliationFn{
		r.reconcileParameterValidation,
		// we sync all our non pool resources first so that they're in-place
		// prior to us scaling up our node pools
		r.reconcileResources,
		// next we sync up all of our pools themselves
		r.reconcilePools,
		// now we memoize the admin client onto the state
		r.initAdminClient,
		// now we ensure that we reconcile all of our decommissioning nodes
		// TODO: Do we want to rate limit this as well given that it also calls the admin API?
		// My thought is no since we want to be snappy with decommissioning.
		r.reconcileDecommission,
		// finally reconcile all of our license information
		r.reconcileLicense,
		// now reconcile cluster configuration
		r.reconcileClusterConfig,
	}

	for _, reconciler := range reconcilers {
		result, err := reconciler(ctx, state, cluster)
		// if we have an error or an explicit requeue from one of our
		// sub reconcilers, then just early return
		if err != nil || result.RequeueAfter > 0 {
			log.FromContext(ctx).V(log.TraceLevel).Info("aborting reconciliation early", "error", err, "requeueAfter", result.RequeueAfter)
			return r.syncStatus(ctx, cluster, state, result, err)
		}
	}

	// we're at the end of reconciliation, so sync back our status
	log.FromContext(ctx).V(log.TraceLevel).Info("finished normal reconciliation loop")
	return r.syncStatus(ctx, cluster, state, ctrl.Result{}, nil)
}

func (r *RedpandaReconciler) fetchInitialState(ctx context.Context, rp *redpandav1alpha2.Redpanda, cluster cluster.Cluster) (*clusterReconciliationState, error) {
	logger := log.FromContext(ctx)

	// fetch any related node pools
	var err error
	var existingPools []*redpandav1alpha2.NodePool
	if r.UseNodePools {
		existingPools, err = controller.FromSourceCluster(ctx, cluster.GetClient(), "pool", rp, &redpandav1alpha2.NodePoolList{})
		if err != nil {
			logger.Error(err, "fetching desired node pools")
			return nil, err
		}
	}

	rpcluster := lifecycle.NewClusterWithPools(rp, existingPools...)

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	restartOnConfigChange := feature.RestartOnConfigChange.Get(ctx, rp)
	injectedConfigVersion := ""
	if restartOnConfigChange {
		injectedConfigVersion = rp.Status.ConfigVersion
	}
	// Single-cluster path: only the local cluster exists. nodePoolsObserved=nil
	// is fine because FetchExistingAndDesiredPools unconditionally marks the
	// local cluster as observed regardless of the map.
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, rpcluster, injectedConfigVersion, nil)
	if err != nil {
		logger.Error(err, "fetching pools")
		return nil, err
	}

	status := lifecycle.NewClusterStatus()
	status.Pools = pools.PoolStatuses()

	if pools.AnyReady() {
		status.Status.SetReady(statuses.ClusterReadyReasonReady)
	} else if pools.AllZero() {
		status.Status.SetReady(statuses.ClusterReadyReasonNotReady, messageNoBrokers)
	} else {
		status.Status.SetReady(statuses.ClusterReadyReasonNotReady, "No pods are ready")
	}

	return &clusterReconciliationState{
		cluster:               rpcluster,
		pools:                 pools,
		status:                status,
		restartOnConfigChange: restartOnConfigChange,
	}, nil
}

func (r *RedpandaReconciler) reconcileParameterValidation(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := validateClusterParameters(state.cluster.Redpanda); err != nil {
		state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())

		logger.Error(err, "validating cluster parameters")
		cluster.GetEventRecorderFor("RedpandaReconciler").Eventf(state.cluster.Redpanda, "Warning", redpandav1alpha2.EventSeverityError, err.Error()) //nolint:staticcheck // TODO: migrate to GetEventRecorder (new events API)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileResources(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcileResources")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error reconciling resources")
			if internalclient.IsTerminalClientError(err) {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())
			} else {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())
			}
		}

		trace.EndSpan(span, err)
	}()

	if err = r.LifecycleClient.SyncAll(ctx, state.cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// reconcilePools is the meat of the controller. It handles creation and scale up/scale down
// of the given cluster pools. All scale up and update routines can happen concurrently, but
// every scale down happens a single broker at a time, ending reconciliation early and requeueing
// the cluster if a decommissioning operation/scale down is currently in progress.
func (r *RedpandaReconciler) reconcilePools(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcilePools")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error reconciling pools")
			if internalclient.IsTerminalClientError(err) {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())
			} else {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())
			}
		}

		trace.EndSpan(span, err)
	}()

	if !state.pools.CheckScale(ctx) {
		logger.V(log.TraceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	logger.V(log.TraceLevel).Info("ready to scale and apply node pools", "existing", state.pools.ExistingStatefulSets(), "desired", state.pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range state.pools.ToCreate() {
		logger.V(log.TraceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "creating statefulset")
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range state.pools.ToScaleUp() {
		logger.V(log.TraceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "scaling up statefulset")
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range state.pools.RequiresUpdate() {
		logger.V(log.TraceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "updating statefulset")
		}
	}

	result := ctrl.Result{}
	requeue := !(state.pools.AnyReady() || state.pools.AllZero())

	if requeue {
		result.RequeueAfter = requeueTimeout
	}
	return result, nil
}

func (r *RedpandaReconciler) initAdminClient(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, state.cluster.Redpanda)
	if err != nil {
		logger.Error(err, "error fetching redpanda admin client")
		return ctrl.Result{}, err
	}
	state.admin = admin
	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileDecommission(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
	var health rpadmin.ClusterHealthOverview

	ctx, span := trace.Start(ctx, "reconcileDecommission")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error decommissioning brokers")
			if internalclient.IsTerminalClientError(err) {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())
				state.status.Status.SetHealthy(statuses.ClusterHealthyReasonTerminalError, err.Error())
			} else {
				state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())
				state.status.Status.SetHealthy(statuses.ClusterHealthyReasonError, err.Error())
			}
			trace.EndSpan(span, err)
			return
		}

		if state.pools.AllZero() {
			state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonSynced)
			state.status.Status.SetHealthy(statuses.ClusterHealthyReasonNotHealthy, messageNoBrokers)
		} else if health.IsHealthy {
			state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonSynced)
			state.status.Status.SetHealthy(statuses.ClusterHealthyReasonHealthy)
		} else {
			state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonSynced)
			// TODO: give more specific message here
			state.status.Status.SetHealthy(statuses.ClusterHealthyReasonNotHealthy, "Cluster is not healthy")
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		return reconcile.Result{}, nil
	}

	health, err = r.fetchClusterHealth(ctx, state.admin)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "fetching cluster health")
	}

	// brokerMap keys brokers by the first DNS label of their internal RPC
	// address (pod name when InternalRPCAddress is the per-pod FQDN).
	// InternalRPCAddress can also come back as "host:port" (advertised RPC
	// port suffix) or — in rarer misconfigurations — as a bare IP. Strip
	// the port if present, then key by both the first label (pod-name
	// lookup) and the raw host, so the roll loop below doesn't silently
	// misclassify a registered broker as orphan.
	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := state.admin.Broker(ctx, brokerID)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "fetching broker")
		}

		host := broker.InternalRPCAddress
		if h, _, splitErr := net.SplitHostPort(broker.InternalRPCAddress); splitErr == nil {
			host = h
		}
		brokerMap[strings.SplitN(host, ".", 2)[0]] = brokerID
		brokerMap[host] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range state.pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, state.admin, state.cluster, set, brokerMap)
		result := ctrl.Result{}
		if requeue {
			result.RequeueAfter = requeueTimeout
		}
		//nolint:staticcheck // SA4004 this is intentionally early terminated
		return result, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range state.pools.ToDelete() {
		logger.V(log.TraceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := cluster.GetClient().Delete(ctx, set.StatefulSet); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting statefulset")
		}
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := state.pools.PodsToRoll()

	// Don't start rolling while a recently replaced pod is still coming up.
	// The cluster health view (brokerMap, isHealthy) lags behind pod state,
	// and rolling a second pod before the first one's replacement is ready
	// would cause two pods to be unavailable simultaneously.
	// Only check when there are actually pods to roll — otherwise we'd block
	// normal reconciliation when a pod is unready for unrelated reasons.
	if len(rollSet) > 0 && state.pools.HasRecentlyReplacedPods() {
		logger.V(log.DebugLevel).Info("recently replaced pods not ready, deferring rolling restart")
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	// Second gate, Redpanda 25.1+: even when the prior pod is K8s-Ready, the
	// broker inside it may still be replaying partition state from its
	// peers. /v1/broker/post_restart_probe (load_reclaimed_pc) lets us wait
	// for the broker to confirm it has reached the in-sync fraction we
	// expect before we roll the next one. This is the ENG-222 RFC's "wait
	// for post-restart probe" step. On Redpanda <25.1 the endpoint returns
	// 404 for every broker; brokersStillRecovering then returns
	// (false, nil) so behavior on older clusters is unchanged.
	if len(rollSet) > 0 {
		recovering, err := brokersStillRecovering(ctx, state.admin, brokerMap, logger)
		if err != nil {
			// Probe error is not fatal — the K8s-Ready gate above and
			// the per-broker pre-restart probe below still protect us.
			logger.V(log.DebugLevel).Info("post-restart probe check failed, proceeding without it", "error", err)
		} else if recovering {
			logger.V(log.DebugLevel).Info("a broker is still post-restart recovering, deferring rolling restart")
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
	}

	rolled := false
	for _, pod := range rollSet {
		shouldRoll, continueExecution := false, false

		brokerID, inBrokerMap := brokerMap[pod.GetName()]
		switch {
		case !inBrokerMap:
			// The pod has no matching broker in our cluster view. That's
			// usually safe to delete (orphan, ghost broker, mis-named
			// replica), but treating *every* unmapped pod that way in a
			// single reconcile would tear them all down at once if the
			// mismatch is transient — slow admin API on a just-restarted
			// broker, advertised-address format the parser missed, etc.
			// Delete this one and requeue so we re-evaluate against a
			// fresh view before touching the next pod.
			shouldRoll, continueExecution = true, false
		default:
			// Per-broker restart-safety probe (Redpanda 25.1+):
			// /v1/broker/pre_restart_probe answers a per-broker
			// counterfactual — "if I restart this broker now, which
			// partitions are affected?" — rather than the cluster-wide
			// IsHealthy boolean. When the endpoint returns true we know
			// this specific broker can be restarted without risking
			// acks=1 data loss, acks=-1 produce rejection, or partition
			// unavailability. On clusters that don't expose the endpoint
			// the helper falls back to cluster.IsHealthy so behavior on
			// pre-25.1 brokers is unchanged.
			safe, err := brokerSafeToRestart(ctx, state.admin, brokerID, health.IsHealthy, logger, pod.GetName())
			switch {
			case err != nil:
				// Be conservative — skip rolling this pod but try the
				// next one. The reconciler will retry on the next loop.
				logger.V(log.DebugLevel).Info("pre-restart probe error, skipping pod", "pod", pod.GetName(), "error", err)
				shouldRoll, continueExecution = false, true
			case safe:
				// roll and halt execution
				shouldRoll, continueExecution = true, false
			default:
				// not safe right now — skip this pod, try the next
				shouldRoll, continueExecution = false, true
			}
		}

		if shouldRoll {
			rolled = true
			logger.V(log.TraceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := cluster.GetClient().Delete(ctx, pod.Pod); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "deleting pod")
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) setupLicense(ctx context.Context, rp *redpandav1alpha2.Redpanda, adminClient *rpadmin.AdminAPI, cluster cluster.Cluster) error {
	if rp.Spec.ClusterSpec.Enterprise == nil {
		return nil
	}

	if literalLicense := ptr.Deref(rp.Spec.ClusterSpec.Enterprise.License, ""); literalLicense != "" {
		if err := adminClient.SetLicense(ctx, strings.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	if secretReference := rp.Spec.ClusterSpec.Enterprise.LicenseSecretRef; secretReference != nil {
		var licenseSecret corev1.Secret

		name := ptr.Deref(secretReference.Name, "")
		key := ptr.Deref(secretReference.Key, "")
		if name == "" || key == "" {
			return errors.Newf("both name %q and key %q of the secret can not be empty string", name, key)
		}

		if err := cluster.GetClient().Get(ctx, client.ObjectKey{Namespace: rp.Namespace, Name: name}, &licenseSecret); err != nil {
			return errors.WithStack(err)
		}

		literalLicense := licenseSecret.Data[key]
		if err := adminClient.SetLicense(ctx, bytes.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *RedpandaReconciler) reconcileLicense(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	var license *redpandav1alpha2.RedpandaLicenseStatus

	ctx, span := trace.Start(ctx, "reconcileLicense")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			if internalclient.IsTerminalClientError(err) {
				state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonTerminalError, err.Error())
			} else {
				state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonError, err.Error())
			}
		} else if license != nil {
			state.status.LicenseStatus = license
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonNotPresent, messageNoBrokers)
		return ctrl.Result{}, nil
	}

	// rate limit license reconciliation
	if statuses.HasRecentCondition(state.cluster.Redpanda, statuses.ClusterLicenseValid, metav1.ConditionTrue, time.Minute) {
		// just copy over the license status from our existing status
		state.status.Status.SetLicenseValidFromCurrent(state.cluster.Redpanda)
		return ctrl.Result{}, nil
	}

	if err := r.setupLicense(ctx, state.cluster.Redpanda, state.admin, cluster); err != nil {
		logger.Error(err, "error setting up license")
		return ctrl.Result{}, errors.WithStack(err)
	}

	features, err := state.admin.GetEnterpriseFeatures(ctx)
	if err != nil {
		logger.Error(err, "error getting enterprise features")
		return ctrl.Result{}, errors.WithStack(err)
	}

	licenseInfo, err := state.admin.GetLicenseInfo(ctx)
	if err != nil {
		logger.Error(err, "error getting license info")
		return ctrl.Result{}, errors.WithStack(err)
	}

	switch features.LicenseStatus {
	case rpadmin.LicenseStatusExpired:
		state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonExpired)
	case rpadmin.LicenseStatusNotPresent:
		state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonNotPresent)
	case rpadmin.LicenseStatusValid:
		state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonValid)
	}

	licenseStatus := func() *redpandav1alpha2.RedpandaLicenseStatus {
		inUseFeatures := []string{}
		for _, feature := range features.Features {
			if feature.Enabled {
				inUseFeatures = append(inUseFeatures, feature.Name)
			}
		}
		sort.Strings(inUseFeatures)

		status := &redpandav1alpha2.RedpandaLicenseStatus{
			InUseFeatures: inUseFeatures,
			Violation:     features.Violation,
		}

		// make sure we can actually format the extend license properties
		if !licenseInfo.Loaded {
			return status
		}

		status.Organization = ptr.To(licenseInfo.Properties.Organization)
		status.Type = ptr.To(licenseInfo.Properties.Type)
		expirationTime := time.Unix(licenseInfo.Properties.Expires, 0)

		// if we have an expiration that is below 0 we are already expired
		// so no need to set the expiration time
		status.Expired = ptr.To(licenseInfo.Properties.Expires <= 0 || expirationTime.Before(time.Now()))

		if !*status.Expired {
			status.Expiration = &metav1.Time{Time: expirationTime.UTC()}
		}

		return status
	}

	license = licenseStatus()
	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileClusterConfig(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcileClusterConfig")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			if internalclient.IsTerminalClientError(err) {
				state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonTerminalError, err.Error())
			} else {
				state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonError, err.Error())
			}
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonNotApplied, messageNoBrokers)
		return ctrl.Result{}, nil
	}

	// we rate limit setting cluster configuration to once a minute if it's already been applied and is up-to-date
	// NB: For this and the next block, we heavily rely on the likelihood of things getting requeued on a fairly regular
	// basis, but in some odd cases this could potentially be problematic. Take for instance someone changing a ConfigMap
	// that gets materialized into the cluster configuration twice in somewhat rapid succession. Let's say that between changes
	// reconciliation gets run and configuration is synced. After the second change, if reconciliation is triggered again, then
	// the underlying configuration will not wind up getting updated because:
	//   1. the generation of the RP cluster hasn't changed
	//   2. we have set the given condition within the last minute
	// This could cause issues of staleness where we have to wait for retrigger of reconciliation either via some watched resource
	// change, or, in worst case, the default runtime cache-sync interval of ~10 hours. On the flip-side, it causes us to hammer
	// the API less often.
	if statuses.HasRecentCondition(state.cluster.Redpanda, statuses.ClusterConfigurationApplied, metav1.ConditionTrue, time.Minute) {
		state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)
		return ctrl.Result{}, nil
	}

	schema, err := state.admin.ClusterConfigSchema(ctx)
	if err != nil {
		logger.Error(err, "fetching cluster config schema")
		return ctrl.Result{}, errors.WithStack(err)
	}

	config, warnings, err := r.clusterConfigFor(ctx, state.cluster.Redpanda, schema, cluster)
	if err != nil {
		logger.Error(err, "fetching cluster config")
		return ctrl.Result{}, errors.WithStack(err)
	}
	// If any fixups produced warnings (typically `errorToWarning`-wrapped
	// failed external secret lookups on Optional secrets), the
	// corresponding entries in `config` still hold their unexpanded
	// `${secrets.X}` placeholders. Pushing that to PatchClusterConfig
	// would surface Redpanda's downstream validation error (e.g. "Must
	// set both of iceberg_rest_catalog_client_id ...") instead of the
	// actual root cause (e.g. an AccessDeniedException from the secret
	// store). Fail the reconcile with the warning text so the user sees
	// the actionable cause directly in the status condition. See K8S-858.
	if msg := clusterconfiguration.FormatWarnings(warnings); msg != "" {
		err := errors.Newf("cluster config has unresolved external secret references: %s", msg)
		logger.Error(err, "fetching cluster config")
		return ctrl.Result{}, err
	}

	superusers, err := r.superusersFor(ctx, state.cluster.Redpanda, cluster)
	if err != nil {
		logger.Error(err, "fetching super users")
		return ctrl.Result{}, errors.WithStack(err)
	}

	mode := feature.ClusterConfigSyncMode.Get(ctx, state.cluster.Redpanda)

	syncer := syncclusterconfig.Syncer{Client: state.admin, Mode: mode}
	configStatus, err := syncer.Sync(ctx, config, superusers)
	if err != nil {
		logger.Error(err, "syncing cluster config")
		return ctrl.Result{}, errors.WithStack(err)
	}

	state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)

	version := configStatus.PropertiesThatNeedRestartHash
	didConfigChange := state.cluster.Redpanda.Status.ConfigVersion != version
	state.status.ConfigVersion = ptr.To(version)

	result := ctrl.Result{}
	shouldRequeue := didConfigChange && state.restartOnConfigChange

	if shouldRequeue {
		result.RequeueAfter = requeueTimeout
	}

	return result, nil
}

func (r *RedpandaReconciler) superusersFor(ctx context.Context, rp *redpandav1alpha2.Redpanda, cluster cluster.Cluster) ([]string, error) {
	values, err := rp.GetValues()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if !values.Auth.SASL.Enabled {
		return nil, nil
	}

	key := client.ObjectKey{Namespace: rp.Namespace, Name: values.Auth.SASL.SecretRef}

	var users corev1.Secret
	if err := cluster.GetClient().Get(ctx, key, &users); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.WithStack(err)
	}

	superusers := []string{}
	for filename, userTXT := range users.Data {
		superusers = append(superusers, syncclusterconfig.LoadUsersFile(ctx, filename, userTXT)...)
	}

	// internal superuser should always be added
	return syncclusterconfig.NormalizeSuperusers(append(superusers, values.Auth.SASL.BootstrapUser.Username())), nil
}

func (r *RedpandaReconciler) clusterConfigFor(ctx context.Context, rp *redpandav1alpha2.Redpanda, schema rpadmin.ConfigSchema, cluster cluster.Cluster) (_ map[string]any, _ []error, err error) {
	// Parinoided panic catch as we're calling directly into helm functions.
	defer func() {
		if r := recover(); r != nil {
			err = errors.Newf("recovered panic: %+v", r)
		}
	}()

	dot, err := rp.GetDot(cluster.GetConfig())
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	// The most reliable way to get the correct and full cluster config is to
	// "envsubst" the bootstrap file itself as various components feed into the
	// final cluster config and they may be referencing values stored in
	// configmaps or secrets.
	state, err := redpanda.RenderStateFromDot(dot)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}
	job := redpanda.PostInstallUpgradeJob(state)
	clusterConfigTemplate, fixups := redpanda.BootstrapContents(state, redpanda.Pool{Statefulset: state.Values.Statefulset})
	conf := clusterconfiguration.NewClusterCfg(clusterconfiguration.NewPodContext(rp.Namespace))
	for k, v := range clusterConfigTemplate {
		conf.SetAdditionalConfiguration(k, v)
	}
	for _, f := range fixups {
		conf.AddFixup(f.Field, f.CEL)
	}
	for _, e := range job.Spec.Template.Spec.InitContainers[0].Env {
		if err := conf.EnsureInitEnv(e); err != nil {
			return nil, nil, errors.WithStack(err)
		}
	}
	for _, e := range job.Spec.Template.Spec.InitContainers[0].EnvFrom {
		if err := conf.EnsureInitEnvFrom(e); err != nil {
			return nil, nil, errors.WithStack(err)
		}
	}

	desired, err := conf.Reify(ctx, cluster.GetClient(), r.CloudSecretsExpander, schema)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	return desired, conf.Warnings(), nil
}

// syncStatus updates the status of the Redpanda cluster at the end of reconciliation when
// no more reconciliation should occur.
func (r *RedpandaReconciler) syncStatus(ctx context.Context, cluster cluster.Cluster, state *clusterReconciliationState, result ctrl.Result, err error) (ctrl.Result, error) {
	original := state.cluster.Redpanda.Status.DeepCopy()
	if r.LifecycleClient.SetClusterStatus(state.cluster, state.status) {
		log.FromContext(ctx).V(log.TraceLevel).Info("setting cluster status from diff", "original", original, "new", state.cluster.Redpanda.Status)
		syncErr := cluster.GetClient().Status().Update(ctx, state.cluster.Redpanda)
		err = errors.Join(syncErr, err)
	}

	syncResult, syncErr := ignoreConflict(err)
	if syncErr == nil && (result.RequeueAfter > 0) {
		syncResult.RequeueAfter = result.RequeueAfter
	}
	return syncResult, syncErr
}

// brokerSafeToRestart consults the broker's pre-restart probe (Redpanda 25.1+
// — /v1/broker/pre_restart_probe) to decide whether rolling this particular
// broker is currently safe. It returns true when none of the dangerous risk
// categories are populated for this broker:
//
//   - acks1_data_loss                 (acks=1 producers may lose data)
//   - unavailable                     (partitions reject produce and consume)
//   - full_acks_produce_unavailable   (acks=-1 produce rejected)
//
// rf1_offline is acceptable — RF=1 already has no redundancy.
//
// When the broker is on a Redpanda version without the probe endpoint (404),
// the function falls back to the legacy cluster-wide IsHealthy heuristic
// passed in via clusterIsHealthy so behavior on older brokers is unchanged.
// All other errors are returned as-is and the caller should treat them as
// "skip this pod, retry next reconcile".
func brokerSafeToRestart(ctx context.Context, admin *rpadmin.AdminAPI, brokerID int, clusterIsHealthy bool, logger logr.Logger, podName string) (bool, error) {
	brokerURL, err := admin.BrokerIDToURL(ctx, brokerID)
	if err != nil {
		return false, fmt.Errorf("resolving broker %d URL: %w", brokerID, err)
	}
	scoped, err := admin.ForHost(brokerURL)
	if err != nil {
		return false, fmt.Errorf("scoping admin client to broker %d (%s): %w", brokerID, brokerURL, err)
	}
	defer scoped.Close()

	result, err := scoped.PreRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			// Pre-25.1 broker — fall back to the cluster-wide
			// heuristic. This preserves behavior on older clusters
			// while letting 25.1+ benefit from the precise probe.
			logger.V(log.DebugLevel).Info("pre-restart probe unsupported on broker, falling back to cluster IsHealthy", "pod", podName, "brokerID", brokerID)
			return clusterIsHealthy, nil
		}
		return false, fmt.Errorf("fetching pre-restart probe for broker %d: %w", brokerID, err)
	}

	if n := len(result.Risks.Acks1DataLoss); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: acks=1 data loss risk", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.Unavailable); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: partitions would become unavailable", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.FullAcksProduceUnavailable); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: acks=-1 produce would be rejected", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.RF1Offline); n > 0 {
		logger.V(log.TraceLevel).Info("broker restart will briefly offline RF=1 partitions (acceptable)", "pod", podName, "partitions", n)
	}
	return true, nil
}

// brokerCaughtUp consults the broker's post-restart probe (Redpanda 25.1+ —
// /v1/broker/post_restart_probe) to decide whether the broker has finished
// replaying partition state since its last restart. Returns true when
// LoadReclaimedPercent >= threshold (DefaultPostRestartCaughtUpPercent gives
// the strictest 100% reading).
//
// On 404 the function returns (true, nil): the endpoint is absent on
// pre-25.1 brokers, and we've never previously gated on this signal, so the
// safe behavior is to act as though the broker is caught up (the
// HasRecentlyReplacedPods K8s-Ready gate is still in place above).
//
// All other errors propagate so the caller can decide whether to skip or
// retry — the controller treats them as non-fatal at the gate (see usage
// in reconcileDecommission) because the K8s-Ready gate and the per-broker
// pre-restart probe still cover the dangerous failure modes.
func brokerCaughtUp(ctx context.Context, admin *rpadmin.AdminAPI, brokerID, threshold int, logger logr.Logger, podName string) (bool, error) {
	brokerURL, err := admin.BrokerIDToURL(ctx, brokerID)
	if err != nil {
		return false, fmt.Errorf("resolving broker %d URL: %w", brokerID, err)
	}
	scoped, err := admin.ForHost(brokerURL)
	if err != nil {
		return false, fmt.Errorf("scoping admin client to broker %d (%s): %w", brokerID, brokerURL, err)
	}
	defer scoped.Close()

	result, err := scoped.PostRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			logger.V(log.DebugLevel).Info("post-restart probe unsupported on broker, treating as caught up", "pod", podName, "brokerID", brokerID)
			return true, nil
		}
		return false, fmt.Errorf("fetching post-restart probe for broker %d: %w", brokerID, err)
	}

	if result.LoadReclaimedPercent < threshold {
		logger.V(log.DebugLevel).Info("broker still post-restart recovering",
			"pod", podName, "brokerID", brokerID,
			"load_reclaimed_pc", result.LoadReclaimedPercent, "threshold", threshold)
		return false, nil
	}
	return true, nil
}

// brokersStillRecovering returns true when any broker in brokerMap reports
// load_reclaimed_pc < probes.DefaultPostRestartCaughtUpPercent via the
// post-restart probe. The roll loop uses this to wait for a just-restarted
// broker to finish replaying its in-sync replicas before proceeding to the
// next pod.
//
// The threshold is hardcoded to the package default rather than plumbed
// through as a parameter because every caller wants the strictest reading
// — there's no operator-level use case yet for accepting partial recovery
// at this gate. If we ever expose tunable rolling-restart tolerance via the
// CR we'll lift this back into a parameter. (For tests of the threshold
// arithmetic itself, see brokerCaughtUp, which still accepts it.)
//
// Implementation note: we query every broker in the map rather than
// tracking which specific pods were "recently rolled," because the probe
// answer is per-broker and consistent regardless — a broker that has been
// running for hours and is fully caught up returns 100 every time. The
// extra cost is one admin call per broker per reconcile, gated on
// len(rollSet) > 0 so steady-state clusters don't pay it.
func brokersStillRecovering(ctx context.Context, admin *rpadmin.AdminAPI, brokerMap map[string]int, logger logr.Logger) (bool, error) {
	// Deduplicate broker IDs — brokerMap intentionally double-keys (by
	// first DNS label and raw host) so iterating values directly would
	// query each broker twice.
	seen := map[int]struct{}{}
	for podName, brokerID := range brokerMap {
		if _, dup := seen[brokerID]; dup {
			continue
		}
		seen[brokerID] = struct{}{}

		caughtUp, err := brokerCaughtUp(ctx, admin, brokerID, probes.DefaultPostRestartCaughtUpPercent, logger, podName)
		if err != nil {
			return false, err
		}
		if !caughtUp {
			return true, nil
		}
	}
	return false, nil
}

func (r *RedpandaReconciler) fetchClusterHealth(ctx context.Context, admin *rpadmin.AdminAPI) (_ rpadmin.ClusterHealthOverview, err error) {
	ctx, span := trace.Start(ctx, "reconcileResources")
	defer func() { trace.EndSpan(span, err) }()

	var health rpadmin.ClusterHealthOverview

	health, err = admin.GetHealthOverview(ctx)
	if err != nil {
		return health, err
	}

	return health, nil
}

// scaleDown contains the majority of the logic for scaling down a statefulset incrementally, first
// decommissioning the broker with the last pod ordinal and then patching the statefulset with
// a single less replica.
func (r *RedpandaReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.ClusterWithPools, set *lifecycle.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].scaleDown", *cluster))
	logger.V(log.TraceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	brokerID, ok := brokerMap[set.LastPod.GetName()]
	if ok {
		// decommission if we have a brokerID, if not
		// then the node has already been fully removed from
		// the cluster and we can go ahead and delete the pod
		// through patching the statefulset

		requeue, err := r.decommissionBroker(ctx, admin, cluster, set, brokerID)
		if err != nil {
			return false, err
		}

		if requeue {
			return true, nil
		}
	}

	logger.V(log.TraceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// now patch the statefulset to remove the pod
	if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set.StatefulSet); err != nil {
		return false, errors.Wrap(err, "scaling down statefulset")
	}
	// we only do a statefulset at a time, waiting for them to
	// become stable first
	return true, nil
}

// decommissionBroker handles decommissioning a broker and waiting until it has finished decommissioning
func (r *RedpandaReconciler) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.ClusterWithPools, set *lifecycle.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(log.TraceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(log.TraceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, errors.Wrap(err, "decommissioning broker")
			}

			return true, nil
		} else {
			return false, errors.Wrap(err, "fetching decommission status")
		}
	}
	if !decommissionStatus.Finished {
		logger.V(log.TraceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

		// just requeue since we're still decommissioning
		return true, nil
	}

	// we're finished
	return false, nil
}

// ignoreConflict ignores errors that happen due to optimistic locking
// checks. This is safe because it means that the client-side cache
// hasn't yet received the update of the resource from the server, and
// once it does reconciliation will be retriggered. To be safe, we
// also explicitly trigger a requeue.
func ignoreConflict(err error) (ctrl.Result, error) {
	if apierrors.IsConflict(err) {
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}
	return ctrl.Result{}, err
}

func validateClusterParameters(rp *redpandav1alpha2.Redpanda) error {
	// Upgrade checks. Don't reconcile if UseFlux is true or if ChartRef is set.
	if rp.Spec.ChartRef.UseFlux != nil && *rp.Spec.ChartRef.UseFlux {
		return errors.New("useFlux: true is no longer supported. Please downgrade or unset `useFlux`")
	}

	if rp.Spec.ChartRef.ChartVersion != "" {
		return errors.New("Specifying chartVersion is no longer supported. Please downgrade or unset `chartRef.chartVersion`")
	}

	return nil
}
