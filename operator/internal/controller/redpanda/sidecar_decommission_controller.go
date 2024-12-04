// Copyright 2024 Redpanda Data, Inc.
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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	requeueTimeout = 10 * time.Second
)

type Decommissioner interface {
	Decommission(ctx context.Context, set *appsv1.StatefulSet) (bool, error)
}

type SidecarDecommissionReconciler struct {
	client.Client
	Decommissioner Decommissioner
}

// SetupWithManager sets up the controller with the Manager.
func (r *SidecarDecommissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}

func (r *SidecarDecommissionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("SidecarDecommissionReconciler.Reconcile")

	set := &appsv1.StatefulSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, set); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "fetching StatefulSet")
		// avoid the internal controller runtime stacktrace
		return ctrl.Result{Requeue: true}, nil
	}

	// Examine if the object is under deletion
	if !set.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	requeue, err := r.Decommissioner.Decommission(ctx, set)
	if err != nil {
		// we already logged any error, just requeue directly
		return ctrl.Result{Requeue: true}, nil
	}

	if requeue {
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}
