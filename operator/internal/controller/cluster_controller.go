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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/client"
	"github.com/redpanda-data/redpanda-operator/operator/internal/resources"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler[T any, U resources.Cluster[T]] struct {
	client.Client
	finalizer      string
	dialer         redpanda.DialContextFunc
	manager        resources.ResourceManager[T, U]
	resourceClient resources.ResourceClient[T, U]
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

	existingPods, err := r.resourceClient.ListOwnedResources(ctx, cluster, &corev1.Pod{})
	if err != nil {
		logger.Error(err, "fetching cluster pods")
		return ctrl.Result{}, err
	}

	existingPools, err := r.resourceClient.ListOwnedResources(ctx, cluster, &appsv1.StatefulSet{})
	if err != nil {
		logger.Error(err, "fetching cluster pods")
		return ctrl.Result{}, err
	}

	status := resources.ClusterStatus{
		ObservedGeneration: cluster.GetGeneration(),
	}

	syncStatus := func(err error) (ctrl.Result, error) {
		if r.manager.SetClusterStatus(cluster, status) {
			syncErr := r.Client.Status().Update(ctx, cluster)
			err = errors.Join(syncErr, err)
		}

		return ignoreConflict(err)
	}

	// TODO: handle deletions
	// we are being deleted, clean up everything
	if cluster.GetDeletionTimestamp() != nil {
		// TODO: begin decommission process

		// clean up all dependant resources
		if deleted, err := r.resourceClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return syncStatus(err)
		}

		if controllerutil.RemoveFinalizer(cluster, r.finalizer) {
			if err := r.Client.Update(ctx, cluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				return ignoreConflict(err)
			}
		}
	}

	// this is the desired node pools
	desiredPools, err := r.manager.NodePools(ctx, cluster)
	if err != nil {
		logger.Error(err, "constructing cluster resources")
		return ctrl.Result{}, err
	}

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.resourceClient.SyncAll(ctx, cluster); err != nil {
		logger.Error(err, "error synchronizing resources")
		return ctrl.Result{}, err
	}

	// do something with pools

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler[T, U]) SetupWithManager(mgr ctrl.Manager) error {
	cluster := resources.NewClusterObject[T, U]()

	builder := ctrl.NewControllerManagedBy(mgr).For(cluster).Owns(&appsv1.StatefulSet{})

	if err := r.resourceClient.WatchResources(builder, cluster); err != nil {
		return err
	}

	return builder.Complete(r)
}
