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
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/finalizers,verbs=update

type MulticlusterReconciler struct {
	Manager         multicluster.Manager
	LifecycleClient *lifecycle.ResourceClient[lifecycle.StretchClusterWithPools, *lifecycle.StretchClusterWithPools]
	ClientFactory   internalclient.ClientFactory
	UseNodePools    bool
}

type stretchClusterReconciliationState struct {
	cluster               *lifecycle.StretchClusterWithPools
	pools                 *lifecycle.PoolTracker
	status                *lifecycle.ClusterStatus
	restartOnConfigChange bool
	admin                 *rpadmin.AdminAPI
}

func (s *stretchClusterReconciliationState) cleanup() {
	if s.admin != nil {
		s.admin.Close()
	}
}

type stretchClusterReconciliationFn func(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error)

func (r *MulticlusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	start := time.Now()
	l := log.FromContext(ctx).WithName("MulticlusterReconciler.Reconcile")

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

	stretchCluster := &redpandav1alpha2.StretchCluster{}
	if err := k8sClient.Get(ctx, req.NamespacedName, stretchCluster); err != nil {
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

	ctx, span := trace.Start(otelkube.Extract(ctx, stretchCluster), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
		attribute.String("clusterName", req.ClusterName),
	))
	defer func() { trace.EndSpan(span, err) }()

	// TODO Should it be removed?
	if !feature.V2Managed.Get(ctx, stretchCluster) {
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				l.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	state, err := r.fetchInitialState(ctx, stretchCluster, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer state.cleanup()

	// Examine if the object is under deletion
	if !stretchCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, state.cluster); deleted || err != nil {
			return r.syncStatus(ctx, cluster, state, ctrl.Result{}, err)
		}
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				l.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Update our StretchCluster with our finalizer and any default Annotation FFs.
	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(stretchCluster, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, stretchCluster) {
		if err := k8sClient.Update(ctx, stretchCluster); err != nil {
			l.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	reconcilers := []stretchClusterReconciliationFn{
		// we sync all our non pool resources first so that they're in-place
		// prior to us scaling up our node pools
		r.reconcileResources,
		// next we sync up all of our pools themselves
		r.reconcilePools,
		// now we memoize the admin client onto the state
		r.initAdminClient,
		// now we ensure that we reconcile all of our decommissioning nodes
		r.reconcileDecommission,
		// now reconcile cluster configuration
		r.reconcileClusterConfig,
		// finally reconcile all of our license information
		r.reconcileLicense,
	}

	for _, reconciler := range reconcilers {
		result, err := reconciler(ctx, state, cluster)
		// if we have an error or an explicit requeue from one of our
		// sub reconcilers, then just early return
		if err != nil || result.RequeueAfter > 0 {
			l.V(log.TraceLevel).Info("aborting reconciliation early", "error", err, "requeueAfter", result.RequeueAfter)
			return r.syncStatus(ctx, cluster, state, result, err)
		}
	}

	// we're at the end of reconciliation, so sync back our status
	l.V(log.TraceLevel).Info("finished normal reconciliation loop")
	return r.syncStatus(ctx, cluster, state, ctrl.Result{}, nil)
}

func (r *MulticlusterReconciler) fetchInitialState(ctx context.Context, sc *redpandav1alpha2.StretchCluster, cluster cluster.Cluster) (*stretchClusterReconciliationState, error) {
	logger := log.FromContext(ctx)

	// fetch any related node pools
	var existingPools []*redpandav1alpha2.NodePool
	poolList := &redpandav1alpha2.NodePoolList{}
	err := cluster.GetClient().List(ctx, poolList, &client.ListOptions{
		Namespace: sc.Namespace,
	})
	if err != nil {
		logger.Error(err, "fetching desired node pools")
		return nil, err
	}
	// Filter pools that reference this stretch cluster
	for i := range poolList.Items {
		pool := &poolList.Items[i]
		if pool.Spec.ClusterRef.Name == sc.Name {
			existingPools = append(existingPools, pool)
		}
	}

	sccluster := lifecycle.NewStretchClusterWithPools(sc, r.Manager.GetClusterNames(), existingPools...)

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	restartOnConfigChange := feature.RestartOnConfigChange.Get(ctx, sc)
	injectedConfigVersion := ""
	if restartOnConfigChange {
		injectedConfigVersion = sc.Status.ConfigVersion
	}
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, sccluster, injectedConfigVersion)
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

	return &stretchClusterReconciliationState{
		cluster:               sccluster,
		pools:                 pools,
		status:                status,
		restartOnConfigChange: restartOnConfigChange,
	}, nil
}

func (r *MulticlusterReconciler) reconcileResources(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
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

func (r *MulticlusterReconciler) reconcilePools(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
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

func (r *MulticlusterReconciler) initAdminClient(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, state.cluster.StretchCluster)
	if err != nil {
		logger.Error(err, "error fetching redpanda admin client")
		return ctrl.Result{}, err
	}
	state.admin = admin
	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcileDecommission(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
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

func (r *MulticlusterReconciler) reconcileLicense(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
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
			state.cluster.StretchCluster.Status.LicenseStatus = license
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		state.status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonNotPresent, messageNoBrokers)
		return ctrl.Result{}, nil
	}

	// rate limit license reconciliation
	if statuses.HasRecentCondition(state.cluster.StretchCluster, statuses.ClusterLicenseValid, metav1.ConditionTrue, time.Minute) {
		// just copy over the license status from our existing status
		state.status.Status.SetLicenseValidFromCurrent(state.cluster.StretchCluster)
		return ctrl.Result{}, nil
	}

	if err := r.setupLicense(ctx, state.cluster.StretchCluster, state.admin, cluster); err != nil {
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

func (r *MulticlusterReconciler) reconcileClusterConfig(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
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
	if statuses.HasRecentCondition(state.cluster.StretchCluster, statuses.ClusterConfigurationApplied, metav1.ConditionTrue, time.Minute) {
		state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)
		return ctrl.Result{}, nil
	}

	// TODO(stretch-cluster): Implement full cluster configuration reconciliation for StretchCluster
	// This will require:
	// 1. Extracting cluster config from the multicluster package render state
	// 2. Getting superusers configuration
	// 3. Syncing configuration via admin API using syncclusterconfig.Syncer
	// 4. Tracking config version for restart-on-change functionality
	//
	// For now, mark as applied to allow other reconciliation to proceed
	logger.V(log.TraceLevel).Info("cluster configuration reconciliation not yet fully implemented for StretchCluster")
	state.status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)

	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) syncStatus(ctx context.Context, cluster cluster.Cluster, state *stretchClusterReconciliationState, result ctrl.Result, err error) (ctrl.Result, error) {
	original := state.cluster.StretchCluster.Status.DeepCopy()
	if r.LifecycleClient.SetClusterStatus(state.cluster, state.status) {
		log.FromContext(ctx).V(log.TraceLevel).Info("setting cluster status from diff", "original", original, "new", state.cluster.StretchCluster.Status)
		syncErr := cluster.GetClient().Status().Update(ctx, state.cluster.StretchCluster)
		err = errors.Join(syncErr, err)
	}

	syncResult, syncErr := ignoreConflict(err)
	if syncErr == nil && (result.RequeueAfter > 0) {
		syncResult.RequeueAfter = result.RequeueAfter
	}
	return syncResult, syncErr
}

func (r *MulticlusterReconciler) fetchClusterHealth(ctx context.Context, admin *rpadmin.AdminAPI) (_ rpadmin.ClusterHealthOverview, err error) {
	ctx, span := trace.Start(ctx, "fetchClusterHealth")
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
func (r *MulticlusterReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.StretchClusterWithPools, set *lifecycle.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("MulticlusterReconciler[%T].scaleDown", *cluster))
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
func (r *MulticlusterReconciler) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.StretchClusterWithPools, set *lifecycle.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("MulticlusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(log.TraceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(log.TraceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, errors.Wrap(err, "decommissioning broker")
			}

			return true, nil
		}
		return false, errors.Wrap(err, "fetching decommission status")
	}
	if !decommissionStatus.Finished {
		logger.V(log.TraceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

		// just requeue since we're still decommissioning
		return true, nil
	}

	// we're finished
	return false, nil
}

func (r *MulticlusterReconciler) setupLicense(ctx context.Context, sc *redpandav1alpha2.StretchCluster, adminClient *rpadmin.AdminAPI, cluster cluster.Cluster) error {
	if sc.Spec.Enterprise == nil {
		return nil
	}

	if literalLicense := ptr.Deref(sc.Spec.Enterprise.License, ""); literalLicense != "" {
		if err := adminClient.SetLicense(ctx, strings.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	if secretReference := sc.Spec.Enterprise.LicenseSecretRef; secretReference != nil {
		var licenseSecret corev1.Secret

		name := ptr.Deref(secretReference.Name, "")
		key := ptr.Deref(secretReference.Key, "")
		if name == "" || key == "" {
			return errors.Newf("both name %q and key %q of the secret can not be empty string", name, key)
		}

		if err := cluster.GetClient().Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: name}, &licenseSecret); err != nil {
			return errors.WithStack(err)
		}

		literalLicense := licenseSecret.Data[key]
		if err := adminClient.SetLicense(ctx, bytes.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		// NB: This is gross, but currently the multicluster runtime doesn't hand this global option off to the controller
		// registration properly, so we can't boot multiple controllers in test without doing this.
		// Consider an upstream fix.
		SkipNameValidation: ptr.To(true),
	}).For(&redpandav1alpha2.StretchCluster{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).Complete(&MulticlusterReconciler{Manager: mgr})
}
