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
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	requeueTimeout = 10 * time.Second
)

type Decommissioner interface {
	Decommission(ctx context.Context, set *appsv1.StatefulSet) (*appsv1ac.StatefulSetConditionApplyConfiguration, bool, error)
}

type SidecarDecommissionReconciler struct {
	client.Client
	Decommissioner Decommissioner
}

// SetupWithManager sets up the controller with the Manager.
func (r *SidecarDecommissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&appsv1.StatefulSet{}).WithEventFilter(UpdateEventFilter).Complete(r)
}

func (r *SidecarDecommissionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("SidecarDecommissionReconciler.Reconcile")

	set := &appsv1.StatefulSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, set); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not retrieve the statefulset: %w", err)
	}

	// Examine if the object is under deletion
	if !set.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	condition, requeue, err := r.Decommissioner.Decommission(ctx, set)
	if condition != nil {
		config := appsv1ac.StatefulSet(set.Name, set.Namespace).WithStatus(appsv1ac.StatefulSetStatus().WithConditions(
			condition,
		))

		statusError := r.Client.Status().Patch(ctx, set, kubernetes.ApplyPatch(config), client.ForceOwnership, fieldOwner)
		if statusError != nil {
			log.Error(err, "syncing StatefulSet status")
			err = errors.Join(err, statusError)
		}
	}

	if err != nil {
		// we already logged any error, just requeue directly
		return ctrl.Result{Requeue: true}, nil
	}

	if requeue {
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}
