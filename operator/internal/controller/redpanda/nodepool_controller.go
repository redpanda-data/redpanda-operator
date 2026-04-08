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

	"github.com/redpanda-data/common-go/otelutil/log"
	// Remove controller-runtime logger after https://github.com/redpanda-data/common-go/pull/160
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	controllerlog "sigs.k8s.io/controller-runtime/pkg/log"
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

func SetupWithMultiClusterManager(mgr multicluster.Manager) error {
	mgr.GetLogger().WithName("SetupWithMultiClusterManager").Info("registering NodePool controller", "knownClusters", mgr.GetClusterNames())
	return mcbuilder.ControllerManagedBy(mgr).
		For(
			&redpandav1alpha2.NodePool{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Watches(&redpandav1alpha2.StretchCluster{}, func(_ string, _ cluster.Cluster) mchandler.EventHandler {
			return mchandler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, object client.Object) []mcreconcile.Request {
				l := controllerlog.FromContext(ctx).WithName("NodePoolReconciler.StretchClusterWatch").V(log.TraceLevel)
				l.Info("StretchCluster event received", "stretchCluster", client.ObjectKeyFromObject(object).String(), "knownClusters", mgr.GetClusterNames())
				var reqs []mcreconcile.Request
				for _, clusterName := range mgr.GetClusterNames() {
					k8sCluster, err := mgr.GetCluster(ctx, clusterName)
					if err != nil {
						l.Error(err, "cannot get cluster", "cluster", clusterName)
						continue
					}
					k8sClient := k8sCluster.GetClient()
					var nodePools redpandav1alpha2.NodePoolList
					err = k8sClient.List(ctx, &nodePools, client.InNamespace(object.GetNamespace()))
					if err != nil {
						l.Error(err, "cannot list NodePools", "cluster", clusterName)
						continue
					}
					l.Info("listed NodePools", "cluster", clusterName, "count", len(nodePools.Items))
					for _, pool := range nodePools.Items {
						l.Info("checking NodePool", "cluster", clusterName, "nodePool", pool.Name, "clusterRefName", pool.Spec.ClusterRef.Name, "isStretchCluster", pool.Spec.ClusterRef.IsStretchCluster())
						if pool.Spec.ClusterRef.IsStretchCluster() && pool.Spec.ClusterRef.Name == object.GetName() {
							reqs = append(reqs, mcreconcile.Request{
								Request: reconcile.Request{
									NamespacedName: types.NamespacedName{
										Namespace: pool.Namespace,
										Name:      pool.Name,
									},
								},
								ClusterName: clusterName,
							})
						}
					}
				}
				l.Info("enqueuing NodePool reconcile requests", "requests", reqs)
				return reqs
			})
		}).
		Complete(
			&NodePoolReconciler{
				Manager: mgr,
			},
		)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(ctx context.Context, mgr multicluster.Manager, namespace string) error {
	builder := mcbuilder.ControllerManagedBy(mgr).
		For(
			&redpandav1alpha2.NodePool{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
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

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, r))
}

// Reconcile reconciles NodePool objects
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	l := controllerlog.FromContext(ctx).WithName("NodePoolReconciler.Reconcile").WithValues("object", req.NamespacedName.String(), "cluster", req.ClusterName)
	l.V(log.DebugLevel).Info("Starting reconcile loop")
	start := time.Now()
	defer func() {
		l.V(log.DebugLevel).Info("Finished reconciling", "elapsed", time.Since(start))
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

	ctx, span := trace.Start(otelkube.Extract(ctx, pool), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := controllerlog.FromContext(ctx)

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

	if err := r.getRedpandaCluster(ctx, req, pool, k8sClient); err != nil {
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

func (r *NodePoolReconciler) getRedpandaCluster(
	ctx context.Context,
	req mcreconcile.Request,
	pool *redpandav1alpha2.NodePool,
	k8sClient client.Client,
) error {
	switch {
	case pool.Spec.ClusterRef.IsStretchCluster():
		return k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, &redpandav1alpha2.StretchCluster{})
	default:
		return k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, &redpandav1alpha2.Redpanda{})
	}
}
