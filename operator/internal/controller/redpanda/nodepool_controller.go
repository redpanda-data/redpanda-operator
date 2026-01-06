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
	"context"
	"reflect"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

// nodepool resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// NodePoolReconciler reconciles a NodePool object. This reconciler in particular should only update status
// fields and finalizers on the NodePool objects, rendering of NodePools takes place within the RedpandaReconciler.
type NodePoolReconciler struct {
	Manager multicluster.Manager
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(ctx context.Context, mgr multicluster.Manager) error {
	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.NodePool{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Watches(&appsv1.StatefulSet{}, mchandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			labels := o.GetLabels()
			if labels == nil {
				return nil
			}

			namespace := labels[lifecycle.DefaultNamespaceLabel]
			name := labels[redpanda.NodePoolLabelName]

			if namespace == "" || name == "" {
				return nil
			}

			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				},
			}}
		}))

	for _, clusterName := range mgr.GetClusterNames() {
		enqueueNodePoolFromCluster, err := controller.RegisterClusterSourceIndex(ctx, mgr, "pool", clusterName, &redpandav1alpha2.NodePool{}, &redpandav1alpha2.NodePoolList{})
		if err != nil {
			return err
		}

		builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueNodePoolFromCluster, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(r)
}

// Reconcile reconciles NodePool objects
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	l := log.FromContext(ctx).WithName("NodePoolReconciler.Reconcile")
	l.V(1).Info("Starting reconcile loop")
	start := time.Now()
	defer func() {
		l.V(1).Info("Finished reconciling", "elapsed", time.Since(start))
	}()

	k8sCluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		l.Error(err, "unable to fetch cluster, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	k8sClient := k8sCluster.GetClient()

	pool := &redpandav1alpha2.NodePool{}

	if err := k8sClient.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := trace.Start(otelkube.Extract(ctx, pool), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	if !feature.V2Managed.Get(ctx, pool) {
		if controllerutil.RemoveFinalizer(pool, FinalizerKey) {
			if err := k8sClient.Update(ctx, pool); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Examine if the object is under deletion
	if !pool.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(pool, FinalizerKey) {
			if err := k8sClient.Update(ctx, pool); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Update our NodePool with our finalizer and any default Annotation FFs.
	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(pool, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, pool) {
		if err := k8sClient.Update(ctx, pool); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	var status statuses.NodePoolStatus
	var statefulSets appsv1.StatefulSetList
	if err := k8sClient.List(ctx, &statefulSets, client.MatchingLabels{
		lifecycle.DefaultNamespaceLabel: pool.Namespace,
		redpanda.NodePoolLabelName:      pool.Name,
	}); err != nil {
		return ctrl.Result{}, err
	}

	var sts *appsv1.StatefulSet
	if len(statefulSets.Items) > 0 {
		sts = &statefulSets.Items[0]
	}

	if sts == nil {
		status.SetDeployed(statuses.NodePoolDeployedReasonNotDeployed)
	}

	originalPoolGeneration := pool.Status.DeployedGeneration
	originalPoolStatus := pool.Status.EmbeddedNodePoolStatus
	pool.Status.EmbeddedNodePoolStatus = redpandav1alpha2.EmbeddedNodePoolStatus{}

	if sts != nil {
		stsLabels := sts.GetLabels()
		if stsLabels != nil {
			generationString := stsLabels[redpanda.NodePoolLabelGeneration]
			if generationString != "" {
				// if we have a parsing error, just skip the generation propagation
				if generation, err := strconv.ParseInt(generationString, 10, 0); err == nil {
					pool.Status.DeployedGeneration = generation
				}
			}
		}

		desiredReplicas := ptr.Deref(pool.Spec.Replicas, 3)
		condemnedReplicas := sts.Status.Replicas - desiredReplicas
		if condemnedReplicas < 0 {
			condemnedReplicas = 0
		}

		if desiredReplicas == sts.Status.Replicas {
			status.SetDeployed(statuses.NodePoolDeployedReasonDeployed)
		} else {
			status.SetDeployed(statuses.NodePoolDeployedReasonScaling)
		}

		pool.Status.EmbeddedNodePoolStatus = redpandav1alpha2.EmbeddedNodePoolStatus{
			Name:              pool.Name,
			Replicas:          sts.Status.Replicas,
			DesiredReplicas:   desiredReplicas,
			ReadyReplicas:     sts.Status.ReadyReplicas,
			RunningReplicas:   sts.Status.AvailableReplicas,
			UpToDateReplicas:  sts.Status.UpdatedReplicas,
			OutOfDateReplicas: sts.Status.Replicas - sts.Status.UpdatedReplicas,
			CondemnedReplicas: condemnedReplicas,
		}
	}

	cluster := &redpandav1alpha2.Redpanda{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			status.SetBound(statuses.NodePoolBoundReasonNotBound)
		} else {
			return ctrl.Result{}, err
		}
	} else {
		status.SetBound(statuses.NodePoolBoundReasonBound)
	}

	if status.UpdateConditions(pool) ||
		!reflect.DeepEqual(originalPoolStatus, pool.Status.EmbeddedNodePoolStatus) ||
		(pool.Status.DeployedGeneration != originalPoolGeneration) {
		return ignoreConflict(k8sClient.Status().Update(ctx, pool))
	}

	return ctrl.Result{}, nil
}
