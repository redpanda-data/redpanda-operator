// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda-operator/operator/internal/resources"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const (
	traceLevel       = 2
	debugLevel       = 1
	infoLevel        = 0
	clusterFinalizer = "cluster.redpanda.com/finalizer"
)

var requeueTimeout = 10 * time.Second

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler[T any, U resources.Cluster[T]] struct {
	client.Client
	ResourceClient *resources.ResourceClient[T, U]
	ClientFactory  internalclient.ClientFactory
}

func ignoreConflict(err error) (ctrl.Result, error) {
	if k8sapierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}

func (r *ClusterReconciler[T, U]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cluster := resources.NewClusterObject[T, U]()
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T]", *cluster))

	if err := r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	pools, err := r.ResourceClient.FetchExistingAndDesiredPools(ctx, cluster)
	if err != nil {
		logger.Error(err, "fetching pools")
		return ctrl.Result{}, err
	}

	status := resources.ClusterStatus{}

	// we are being deleted, clean up everything
	if cluster.GetDeletionTimestamp() != nil {
		// clean up all dependant resources
		if deleted, err := r.ResourceClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return r.syncStatus(ctx, err, status, cluster)
		}

		if controllerutil.RemoveFinalizer(cluster, clusterFinalizer) {
			if err := r.Client.Update(ctx, cluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}

		return ctrl.Result{}, nil
	}

	// we are not deleting, so add a finalizer first before
	// allocating any additional resources
	if controllerutil.AddFinalizer(cluster, clusterFinalizer) {
		if err := r.Client.Update(ctx, cluster); err != nil {
			logger.Error(err, "updating cluster finalizer")
			return ignoreConflict(err)
		}
		return ctrl.Result{}, nil
	}

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.reconcileResources(ctx, cluster); err != nil {
		logger.Error(err, "error reconciling resources")
		return r.syncStatus(ctx, err, status, cluster)
	}

	// next we sync up all of our pools themselves
	admin, requeue, err := r.reconcilePools(ctx, cluster, pools)
	if err != nil {
		logger.Error(err, "error reconciling pools")
		return r.syncStatus(ctx, err, status, cluster)
	}
	if requeue {
		return r.requeue(ctx, status, cluster)
	}

	// finally we synchronize any cluster configuration needed
	if err := r.reconcileClusterConfiguration(ctx, cluster, admin); err != nil {
		return r.syncStatus(ctx, err, status, cluster)
	}

	logger.V(traceLevel).Info("cluster quiesced")

	// if we got here, then all of the loops above were no-ops
	// and so we can mark the status as quiesced.
	status.Quiesced = true

	return r.syncStatus(ctx, nil, status, cluster)
}

func (r *ClusterReconciler[T, U]) reconcileResources(ctx context.Context, cluster U) error {
	return r.ResourceClient.SyncAll(ctx, cluster)
}

func (r *ClusterReconciler[T, U]) reconcilePools(ctx context.Context, cluster U, pools *resources.PoolTracker) (*rpadmin.AdminAPI, bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].reconcilePools", *cluster))

	if !pools.CheckScale() {
		logger.V(traceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return nil, true, nil
	}

	logger.V(traceLevel).Info("ready to scale and apply node pools", "existing", pools.ExistingStatefulSets(), "desired", pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range pools.ToCreate() {
		logger.V(traceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.ResourceClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("creating statefulset: %w", err)
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range pools.ToScaleUp() {
		logger.V(traceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.ResourceClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("scaling up statefulset: %w", err)
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range pools.RequiresUpdate() {
		logger.V(traceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.ResourceClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("updating statefulset: %w", err)
		}
	}

	admin, health, err := r.fetchClusterHealth(ctx, cluster)
	if err != nil {
		return nil, false, fmt.Errorf("fetching cluster health: %w", err)
	}

	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := admin.Broker(ctx, brokerID)
		if err != nil {
			return nil, false, fmt.Errorf("fetching broker: %w", err)
		}

		brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
		brokerMap[brokerTokens[0]] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, admin, cluster, set, brokerMap)
		return nil, requeue, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range pools.ToDelete() {
		logger.V(traceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.Client.Delete(ctx, set); err != nil {
			return nil, false, fmt.Errorf("deleting statefulset: %w", err)
		}
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := pools.PodsToRoll()
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
			logger.V(traceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := r.Client.Delete(ctx, pod); err != nil {
				return nil, false, fmt.Errorf("deleting pod: %w", err)
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return nil, true, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		return nil, true, nil
	}

	return admin, false, nil
}

func (r *ClusterReconciler[T, U]) reconcileClusterConfiguration(_ context.Context, _ U, _ *rpadmin.AdminAPI) error {
	// TODO
	return nil
}

func (r *ClusterReconciler[T, U]) fetchClusterHealth(ctx context.Context, cluster U) (*rpadmin.AdminAPI, rpadmin.ClusterHealthOverview, error) {
	var health rpadmin.ClusterHealthOverview

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, cluster)
	if err != nil {
		return nil, health, err
	}
	health, err = admin.GetHealthOverview(ctx)
	if err != nil {
		return nil, health, err
	}

	return admin, health, nil
}

func (r *ClusterReconciler[T, U]) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster U, set *resources.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].scaleDown", *cluster))
	logger.V(traceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

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

	logger.V(traceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// now patch the statefulset to remove the pod
	if err := r.ResourceClient.PatchNodePoolSet(ctx, cluster, set.StatefulSet); err != nil {
		return false, fmt.Errorf("scaling down statefulset: %w", err)
	}
	// we only do a statefulset at a time, waiting for them to
	// become stable first
	return true, nil
}

func (r *ClusterReconciler[T, U]) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster U, set *resources.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(traceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(traceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, fmt.Errorf("decommissioning broker: %w", err)
			}

			return true, nil
		} else {
			return false, fmt.Errorf("fetching decommission status: %w", err)
		}
	}
	if !decommissionStatus.Finished {
		logger.V(traceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

		// just requeue since we're still decommissioning
		return true, nil
	}

	// we're finished
	return false, nil
}

func (r *ClusterReconciler[T, U]) syncStatus(ctx context.Context, err error, status resources.ClusterStatus, cluster U) (ctrl.Result, error) {
	if r.ResourceClient.SetClusterStatus(cluster, status) {
		syncErr := r.Client.Status().Update(ctx, cluster)
		err = errors.Join(syncErr, err)
	}

	return ignoreConflict(err)
}

func (r *ClusterReconciler[T, U]) requeue(ctx context.Context, status resources.ClusterStatus, cluster U) (ctrl.Result, error) {
	var err error
	if r.ResourceClient.SetClusterStatus(cluster, status) {
		err = r.Client.Status().Update(ctx, cluster)
	}

	result, err := ignoreConflict(err)
	result.Requeue = true
	result.RequeueAfter = requeueTimeout

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler[T, U]) SetupWithManager(mgr ctrl.Manager) error {
	cluster := resources.NewClusterObject[T, U]()

	builder := ctrl.NewControllerManagedBy(mgr).For(cluster).Owns(&appsv1.StatefulSet{})

	if err := r.ResourceClient.WatchResources(builder, cluster); err != nil {
		return err
	}

	return builder.Complete(r)
}
