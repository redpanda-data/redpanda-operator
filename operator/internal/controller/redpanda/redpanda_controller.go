// Copyright 2025 Redpanda Data, Inc.
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
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

const (
	FinalizerKey = "operator.redpanda.com/finalizer"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"

	// reqeueueTimeout is the time that the reconciler will
	// wait before requeueing a cluster to be reconciled when
	// we've explicitly aborted a reconciliation loop early
	// due to an in-progress operation
	requeueTimeout = 10 * time.Second

	// periodicRequeue is the maximal period between re-examining
	// a cluster; this is used to ensure that we regularly reassert
	// cluster configuration (which may depend on external secrets,
	// for which there's no change event we can latch onto).
	periodicRequeue = 3 * time.Minute

	messageNoBrokers = "Cluster has no desired brokers"
)

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	// KubeConfig is the [rest.Config] that provides the go helm chart
	// Kubernetes access. It should be the same config used to create client.
	KubeConfig           *rest.Config
	Client               client.Client
	LifecycleClient      *lifecycle.ResourceClient[lifecycle.ClusterWithPools, *lifecycle.ClusterWithPools]
	EventRecorder        kuberecorder.EventRecorder
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

// Console chart
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// redpanda resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// sidecar resources
// The leases is used by controller-runtime in sidecar. Operator main reconciliation needs to have leases permissions in order to create role that have the same permissions.
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	enqueueClusterFromNodePool := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		pool := o.(*redpandav1alpha2.NodePool)
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name:      pool.Spec.ClusterRef.Name,
				Namespace: pool.Namespace,
			},
		}}
	})

	builder := ctrl.NewControllerManagedBy(mgr)

	if err := r.LifecycleClient.WatchResources(builder, &redpandav1alpha2.Redpanda{}); err != nil {
		return err
	}

	if r.UseNodePools {
		builder.Watches(&redpandav1alpha2.NodePool{}, enqueueClusterFromNodePool)
	}

	return builder.Complete(r)
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

type clusterReconciliationFn func(ctx context.Context, state *clusterReconciliationState) (ctrl.Result, error)

func (r *RedpandaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	rp := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer func() {
		// If we have a resource to manage, ensure that we re-enqueue to re-examine it on a regular basis
		if err != nil {
			// Error returns cause a re-enqueuing this with exponential backoff
			return
		}

		if result.Requeue || result.RequeueAfter > 0 {
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
			if err := r.Client.Update(ctx, rp); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	state, err := r.fetchInitialState(ctx, rp)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer state.cleanup()

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, state.cluster); deleted || err != nil {
			return r.syncStatus(ctx, state, reconcile.Result{}, err)
		}
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := r.Client.Update(ctx, rp); err != nil {
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
		if err := r.Client.Update(ctx, rp); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{Requeue: true}, nil
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
		// now reconcile cluster configuration
		r.reconcileClusterConfig,
		// finally reconcile all of our license information
		r.reconcileLicense,
	}

	for _, reconciler := range reconcilers {
		result, err := reconciler(ctx, state)
		// if we have an error or an explicit requeue from one of our
		// sub reconcilers, then just early return
		if err != nil || result.Requeue || result.RequeueAfter > 0 {
			return r.syncStatus(ctx, state, result, err)
		}
	}

	// we're at the end of reconciliation, so sync back our status
	return r.syncStatus(ctx, state, ctrl.Result{}, nil)
}

func (r *RedpandaReconciler) fetchInitialState(ctx context.Context, rp *redpandav1alpha2.Redpanda) (*clusterReconciliationState, error) {
	logger := log.FromContext(ctx)

	// fetch any related node pools
	var err error
	var existingPools []*redpandav1alpha2.NodePool
	if r.UseNodePools {
		existingPools, err = fromSourceCluster(ctx, r.Client, "pool", rp, &redpandav1alpha2.NodePoolList{})
		if err != nil {
			logger.Error(err, "fetching desired node pools")
			return nil, err
		}
	}

	cluster := lifecycle.NewClusterWithPools(rp, existingPools...)

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	restartOnConfigChange := feature.RestartOnConfigChange.Get(ctx, rp)
	injectedConfigVersion := ""
	if restartOnConfigChange {
		injectedConfigVersion = rp.Status.ConfigVersion
	}
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, cluster, injectedConfigVersion)
	if err != nil {
		logger.Error(err, "fetching pools")
		return nil, err
	}

	status := lifecycle.NewClusterStatus()
	status.Pools = pools.PoolStatuses()

	if pools.AnyReady() {
		status.Status.SetReady(statuses.ClusterReadyReasonReady)
	} else if pools.AllZero() {
		status.Status.SetReady(statuses.ClusterReadyReasonReady, messageNoBrokers)
	} else {
		status.Status.SetReady(statuses.ClusterReadyReasonNotReady, "No pods are ready")
	}

	return &clusterReconciliationState{
		cluster:               cluster,
		pools:                 pools,
		status:                status,
		restartOnConfigChange: restartOnConfigChange,
	}, nil
}

func (r *RedpandaReconciler) reconcileParameterValidation(ctx context.Context, state *clusterReconciliationState) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := validateClusterParameters(state.cluster.Redpanda); err != nil {
		state.status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())

		logger.Error(err, "validating cluster parameters")
		r.EventRecorder.Eventf(state.cluster.Redpanda, "Warning", redpandav1alpha2.EventSeverityError, err.Error())
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileResources(ctx context.Context, state *clusterReconciliationState) (_ ctrl.Result, err error) {
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
func (r *RedpandaReconciler) reconcilePools(ctx context.Context, state *clusterReconciliationState) (_ ctrl.Result, err error) {
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

	if !state.pools.CheckScale() {
		logger.V(log.TraceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return ctrl.Result{Requeue: true}, nil
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

	return ctrl.Result{Requeue: !(state.pools.AnyReady() || state.pools.AllZero())}, nil
}

func (r *RedpandaReconciler) initAdminClient(ctx context.Context, state *clusterReconciliationState) (ctrl.Result, error) {
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

func (r *RedpandaReconciler) reconcileDecommission(ctx context.Context, state *clusterReconciliationState) (_ reconcile.Result, err error) {
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

	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := state.admin.Broker(ctx, brokerID)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "fetching broker")
		}

		brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
		brokerMap[brokerTokens[0]] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range state.pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, state.admin, state.cluster, set, brokerMap)
		return ctrl.Result{Requeue: requeue}, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range state.pools.ToDelete() {
		logger.V(log.TraceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.Client.Delete(ctx, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting statefulset")
		}
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := state.pools.PodsToRoll()
	rolled := false
	for _, pod := range rollSet {
		shouldRoll, continueExecution := false, false

		if _, ok := brokerMap[pod.GetName()]; !ok {
			// we don't actually have this broker in the cluster
			// anymore, which means it's always safe to delete
			// the pod and continue with the next operations
			shouldRoll, continueExecution = true, true
		} else if health.IsHealthy {
			// TODO: don't just check overall cluster health, but use
			// scoped API endpoints for rolling a broker

			// roll and halt execution
			shouldRoll, continueExecution = true, false
		} else {
			// see if we can at least roll the next pods
			shouldRoll, continueExecution = false, true
		}

		if shouldRoll {
			rolled = true
			logger.V(log.TraceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := r.Client.Delete(ctx, pod); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "deleting pod")
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) setupLicense(ctx context.Context, rp *redpandav1alpha2.Redpanda, adminClient *rpadmin.AdminAPI) error {
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

		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: rp.Namespace, Name: name}, &licenseSecret); err != nil {
			return errors.WithStack(err)
		}

		literalLicense := licenseSecret.Data[key]
		if err := adminClient.SetLicense(ctx, bytes.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *RedpandaReconciler) reconcileLicense(ctx context.Context, state *clusterReconciliationState) (_ ctrl.Result, err error) {
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
			state.cluster.Redpanda.Status.LicenseStatus = license
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

	if err := r.setupLicense(ctx, state.cluster.Redpanda, state.admin); err != nil {
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

func (r *RedpandaReconciler) reconcileClusterConfig(ctx context.Context, state *clusterReconciliationState) (_ reconcile.Result, err error) {
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
		state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied, messageNoBrokers)
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

	config, err := r.clusterConfigFor(ctx, state.cluster.Redpanda, schema)
	if err != nil {
		logger.Error(err, "fetching cluster config")
		return ctrl.Result{}, errors.WithStack(err)
	}

	superusers, err := r.superusersFor(ctx, state.cluster.Redpanda)
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

	shouldRequeue := didConfigChange && state.restartOnConfigChange

	return ctrl.Result{Requeue: shouldRequeue}, nil
}

func (r *RedpandaReconciler) superusersFor(ctx context.Context, rp *redpandav1alpha2.Redpanda) ([]string, error) {
	values, err := rp.GetValues()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if !values.Auth.SASL.Enabled {
		return nil, nil
	}

	key := client.ObjectKey{Namespace: rp.Namespace, Name: values.Auth.SASL.SecretRef}

	var users corev1.Secret
	if err := r.Client.Get(ctx, key, &users); err != nil {
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

func (r *RedpandaReconciler) clusterConfigFor(ctx context.Context, rp *redpandav1alpha2.Redpanda, schema rpadmin.ConfigSchema) (_ map[string]any, err error) {
	// Parinoided panic catch as we're calling directly into helm functions.
	defer func() {
		if r := recover(); r != nil {
			err = errors.Newf("recovered panic: %+v", r)
		}
	}()

	dot, err := rp.GetDot(r.KubeConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// The most reliable way to get the correct and full cluster config is to
	// "envsubst" the bootstrap file itself as various components feed into the
	// final cluster config and they may be referencing values stored in
	// configmaps or secrets.
	state, err := redpanda.RenderStateFromDot(dot)
	if err != nil {
		return nil, errors.WithStack(err)
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
			return nil, errors.WithStack(err)
		}
	}
	for _, e := range job.Spec.Template.Spec.InitContainers[0].EnvFrom {
		if err := conf.EnsureInitEnvFrom(e); err != nil {
			return nil, errors.WithStack(err)
		}
	}

	desired, err := conf.Reify(ctx, r.Client, r.CloudSecretsExpander, schema)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return desired, nil
}

// syncStatus updates the status of the Redpanda cluster at the end of reconciliation when
// no more reconciliation should occur.
func (r *RedpandaReconciler) syncStatus(ctx context.Context, state *clusterReconciliationState, result ctrl.Result, err error) (ctrl.Result, error) {
	if r.LifecycleClient.SetClusterStatus(state.cluster, state.status) {
		syncErr := r.Client.Status().Update(ctx, state.cluster.Redpanda)
		err = errors.Join(syncErr, err)
	}

	syncResult, syncErr := ignoreConflict(err)
	if syncErr == nil && (result.Requeue || result.RequeueAfter > 0) {
		syncResult.Requeue = true
		syncResult.RequeueAfter = requeueTimeout
	}
	return syncResult, syncErr
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
		return ctrl.Result{Requeue: true}, nil
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
