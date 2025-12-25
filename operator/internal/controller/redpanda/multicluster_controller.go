// Copyright 2025 Redpanda Data, Inc.
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

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/finalizers,verbs=update

type MulticlusterReconciler struct {
	manager multicluster.Manager
}

func (r *MulticlusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName, "namespace", req.Namespace, "name", req.Name)
	logger.Info("reconciling cluster")

	cluster, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		// we encountered an error fetching the cluster, just ignore the reconciliation request
		return ctrl.Result{}, nil
	}
	k8sClient := cluster.GetClient()

	stretchCluster := &redpandav1alpha2.StretchCluster{}
	if err := k8sClient.Get(ctx, req.NamespacedName, stretchCluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !stretchCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(stretchCluster, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, stretchCluster) {
		if err := k8sClient.Update(ctx, stretchCluster); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	// do whatever reconciliation here

	return ctrl.Result{}, nil
}

func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(controller.TypedOptions[mcreconcile.Request]{
		// NB: This is gross, but currently the multicluster runtime doesn't hand this global option off to the controller
		// registration properly, so we can't boot multiple controllers in test without doing this.
		// Consider an upstream fix.
		SkipNameValidation: ptr.To(true),
	}).For(&redpandav1alpha2.StretchCluster{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).Complete(&MulticlusterReconciler{manager: mgr})
}
