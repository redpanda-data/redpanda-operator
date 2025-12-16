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

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel/attribute"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/locking/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/finalizers,verbs=update

type MulticlusterReconciler struct {
	manager multicluster.Manager
}

func (r *MulticlusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	// log := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName, "namespace", req.Namespace, "name", req.Name)
	// log.Info("reconciling cluster")

	mgr, err := r.manager.GetManager(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
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

	sc := &redpandav1alpha2.StretchCluster{}
	if err := mgr.GetClient().Get(ctx, req.NamespacedName, sc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := trace.Start(otelkube.Extract(ctx, sc), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx).WithValues("cluster", req.ClusterName, "namespace", req.Namespace, "name", req.Name)
	logger.Info("reconciling cluster")

	// INITIAL STATE

	cloudSecrets := lifecycle.CloudSecretsFlags{}
	redpandaImage := lifecycle.Image{
		Repository: "docker.redpanda.com/redpandadata/redpanda",
		Tag:        "v25.2.1",
	}
	sidecarImage := lifecycle.Image{
		Repository: "docker.redpanda.com/redpandadata/redpanda-operator",
		Tag:        "dev",
	}

	resClient := lifecycle.NewMulticlusterResourceClient(r.manager, lifecycle.StrechResourceManagers(redpandaImage, sidecarImage, cloudSecrets))

	cluster := lifecycle.NewStrechClusterWithPools(sc)

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	restartOnConfigChange := feature.RestartOnConfigChange.Get(ctx, sc)
	injectedConfigVersion := ""
	if restartOnConfigChange {
		injectedConfigVersion = sc.Status.ConfigVersion
	}

	pools, err := resClient.FetchExistingAndDesiredPools(ctx, cluster, injectedConfigVersion)
	if err != nil {
		logger.Error(err, "fetching pools")
		return ctrl.Result{}, err
	}
	status := lifecycle.NewClusterStatus()
	status.Pools = pools.PoolStatuses()

	// Examine if the object is under deletion
	if !sc.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := resClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return r.syncStatus(ctx, mgr.GetClient(), resClient, cluster, status, reconcile.Result{}, err)
		}
		if controllerutil.RemoveFinalizer(sc, FinalizerKey) {
			if err := mgr.GetClient().Update(ctx, sc); err != nil {
				logger.Error(err, "updating stretch cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	if err = resClient.SyncAll(ctx, cluster); err != nil {
		return ctrl.Result{}, errors.WithStack(err)
	}

	return ctrl.Result{}, nil
}

// syncStatus updates the status of the Redpanda cluster at the end of reconciliation when
// no more reconciliation should occur.
func (r *MulticlusterReconciler) syncStatus(ctx context.Context, kubeClient client.Client, client *lifecycle.ResourceClient[lifecycle.StretchClusterWithPools, *lifecycle.StretchClusterWithPools], cluster *lifecycle.StretchClusterWithPools, status *lifecycle.ClusterStatus, result ctrl.Result, err error) (ctrl.Result, error) {
	original := cluster.StretchCluster.Status.DeepCopy()
	if client.SetClusterStatus(cluster, status) {
		log.FromContext(ctx).V(log.TraceLevel).Info("setting cluster status from diff", "original", original, "new", cluster.StretchCluster.Status)
		syncErr := kubeClient.Status().Update(ctx, cluster.StretchCluster)
		err = errors.Join(syncErr, err)
	}

	syncResult, syncErr := ignoreConflict(err)
	if syncErr == nil && (result.RequeueAfter > 0) {
		syncResult.RequeueAfter = requeueTimeout
	}
	return syncResult, syncErr
}

func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.StretchCluster{}, mcbuilder.WithEngageWithLocalCluster(true)).
		Complete(&MulticlusterReconciler{manager: mgr})
}
