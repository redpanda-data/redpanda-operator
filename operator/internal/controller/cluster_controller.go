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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/operator/internal/resources"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const clusterFinalizer = "cluster.redpanda.com/finalizer"

var requeueErr = errors.New("requeue")

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler[T any, U resources.Cluster[T]] struct {
	client.Client
	ResourceManager resources.ResourceManager[T, U]
	ResourceClient  resources.ResourceClient[T, U]
	ClientFactory   internalclient.ClientFactory
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

	sets, err := r.ResourceClient.ListOwnedResources(ctx, cluster, &appsv1.StatefulSet{})
	if err != nil {
		logger.Error(err, "fetching cluster pods")
		return ctrl.Result{}, err
	}

	pools := resources.NewPoolManager()

	for _, set := range sets {
		statefulSet := set.(*appsv1.StatefulSet)

		selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
		if err != nil {
			logger.Error(err, "constructing label selector")
			return ctrl.Result{}, err
		}

		pods, err := r.ResourceClient.ListResources(ctx, &corev1.Pod{}, client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			logger.Error(err, "listing pods")
			return ctrl.Result{}, err
		}

		pools.AddExisting(statefulSet, pods...)
	}

	status := resources.ClusterStatus{}

	syncStatus := func(err error) (ctrl.Result, error) {
		var requeue bool
		if errors.Is(err, requeueErr) {
			err = nil
			requeue = true
		}

		if r.ResourceManager.SetClusterStatus(cluster, status) {
			syncErr := r.Client.Status().Update(ctx, cluster)
			err = errors.Join(syncErr, err)
		}

		result, err := ignoreConflict(err)
		if requeue {
			result.Requeue = true
		}

		return result, err
	}

	// we are being deleted, clean up everything
	if cluster.GetDeletionTimestamp() != nil {
		// clean up all dependant resources
		if deleted, err := r.ResourceClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return syncStatus(err)
		}

		if controllerutil.RemoveFinalizer(cluster, clusterFinalizer) {
			if err := r.Client.Update(ctx, cluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point
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

	desired, err := r.ResourceManager.NodePools(ctx, cluster)
	if err != nil {
		logger.Error(err, "constructing cluster resources")
		return syncStatus(err)
	}

	pools.AddDesired(desired...)

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.ResourceClient.SyncAll(ctx, cluster); err != nil {
		logger.Error(err, "error synchronizing resources")
		return syncStatus(err)
	}

	switch pools.CheckScale() {
	case resources.ScaleNotReady:
		// we're not yet ready to scale, so wait
		return syncStatus(requeueErr)
	case resources.ScaleNeedsClusterCheck:
		admin, err := r.ClientFactory.RedpandaAdminClient(ctx, cluster)
		if err != nil {
			logger.Error(err, "constructing admin client")
			return syncStatus(err)
		}
		health, err := admin.GetHealthOverview(ctx)
		if err != nil {
			logger.Error(err, "fetching cluster health")
			return syncStatus(err)
		}
		if !health.IsHealthy {
			// TODO: is this the right check?
			// do we also need a check for decommissioning
			// nodes?
			logger.Info("cluster not currently healthy")
			return syncStatus(requeueErr)
		}

		fallthrough
	case resources.ScaleReady:
		patchStatefulSet := func(set *appsv1.StatefulSet, onError string) (ctrl.Result, error) {
			if err := r.ResourceClient.PatchOwnedResource(ctx, cluster, set); err != nil {
				logger.Error(err, onError)
				return syncStatus(err)
			}
			// we only do a statefulset at a time, waiting for them to
			// become stable first
			return syncStatus(requeueErr)
		}

		// first create any pools that don't currently exists
		for _, set := range pools.ToCreate() {
			return patchStatefulSet(set, "creating node pool statefulset")
		}

		// next scale up any under-provisioned pools and patch them to use the new spec
		for _, set := range pools.ToScaleUp() {
			return patchStatefulSet(set, "scaling up statefulset")
		}

		// next scale down any over-provisioned pools, patching them to use the new spec
		for _, set := range pools.ToScaleDown() {
			return patchStatefulSet(set, "scaling down statefulset")
		}

		// at this point any set that needs to be deleted should have 0 replicas
		// so we can attempt to delete them all in one pass
		for _, set := range pools.ToDelete() {
			if err := r.Client.Delete(ctx, set); err != nil {
				logger.Error(err, "deleting statefulset")
				return syncStatus(err)
			}
		}

		// now patch any sets that might have changed without affecting the cluster size
		// here we can just wholesale patch everything
		for _, set := range pools.Desired() {
			if _, err := patchStatefulSet(set, "scaling down statefulset"); err != nil {
				return syncStatus(err)
			}
		}
	}

	// set the quiescent status
	status.Quiescent = true

	return syncStatus(nil)
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
