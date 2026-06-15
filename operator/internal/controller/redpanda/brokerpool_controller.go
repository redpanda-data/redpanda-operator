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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// brokerpool resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandabrokerpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandabrokerpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandabrokerpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// BrokerPoolReconciler reconciles a RedpandaBrokerPool object. This reconciler in particular should only update status
// fields and finalizers on the RedpandaBrokerPool objects, rendering of RedpandaBrokerPools takes place within the StretchClusterReconciler.
type BrokerPoolReconciler struct {
	Manager multicluster.Manager
}

func SetupWithMultiClusterManager(mgr multicluster.Manager) error {
	name := "BrokerPool"
	mgr.GetLogger().WithName("SetupWithMultiClusterManager").Info(
		"registering "+name+" controller",
		"knownClusters", createCanonicalClusterNameList(mgr),
	)
	return mcbuilder.ControllerManagedBy(mgr).
		For(
			&redpandav1alpha2.RedpandaBrokerPool{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Watches(&redpandav1alpha2.StretchCluster{}, func(_ string, _ cluster.Cluster) mchandler.EventHandler {
			return mchandler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, object client.Object) []mcreconcile.Request {
				l := log.FromContext(ctx).WithName("BrokerPoolReconciler.StretchClusterWatch").V(log.TraceLevel)
				l.Info("StretchCluster event received", "stretchCluster", client.ObjectKeyFromObject(object).String(), "knownClusters", createCanonicalClusterNameList(mgr))
				var reqs []mcreconcile.Request
				for _, clusterName := range mgr.GetClusterNames() {
					k8sCluster, err := mgr.GetCluster(ctx, clusterName)
					if err != nil {
						l.Error(err, "cannot get cluster", "cluster", clusterName)
						continue
					}
					k8sClient := k8sCluster.GetClient()
					var brokerPools redpandav1alpha2.RedpandaBrokerPoolList
					err = k8sClient.List(ctx, &brokerPools, client.InNamespace(object.GetNamespace()))
					if err != nil {
						l.Error(err, "cannot list RedpandaBrokerPools", "cluster", clusterName)
						continue
					}
					l.Info("listed RedpandaBrokerPools", "cluster", lifecycle.CanonicalClusterName(clusterName, mgr.GetLocalClusterName), "count", len(brokerPools.Items))
					for _, pool := range brokerPools.Items {
						l.Info("checking RedpandaBrokerPool", "cluster", clusterName, "brokerPool", pool.Name, "clusterRefName", pool.Spec.ClusterRef.Name, "isStretchCluster", pool.Spec.ClusterRef.IsStretchCluster())
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
				l.Info("enqueuing RedpandaBrokerPools reconcile requests", "requests", reqs)
				return reqs
			})
		}).
		Complete(
			observability.Wrap[mcreconcile.Request](
				&BrokerPoolReconciler{Manager: mgr},
				name,
				periodicRequeue,
			),
		)
}

// Reconcile reconciles RedpandaBrokerPool objects
func (r *BrokerPoolReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	l := log.FromContext(ctx).WithName("RedpandaBrokerPoolReconciler.Reconcile").WithValues("object", req.NamespacedName.String(), "cluster", req.ClusterName)
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

	pool := &redpandav1alpha2.RedpandaBrokerPool{}

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
		logger.V(log.TraceLevel).Info("deleting finalizer")
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

	// Update our RedpandaBrokerPool with our finalizer and any default Annotation FFs.
	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(pool, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, pool) {
		logger.V(log.TraceLevel).Info("adding finalizer")
		if err := k8sClient.Update(ctx, pool); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	var status statuses.RedpandaBrokerPoolStatus

	// Check binding first: a broker pool's StatefulSet is only ever rendered
	// for a known Redpanda cluster, so there's no point in inspecting
	// deployment state if we can't even resolve the cluster reference.
	if err := r.getRedpandaCluster(ctx, req, pool, k8sClient); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		status.SetBound(statuses.RedpandaBrokerPoolBoundReasonNotBound)
	} else {
		status.SetBound(statuses.RedpandaBrokerPoolBoundReasonBound)
	}

	var statefulSets appsv1.StatefulSetList
	if err := k8sClient.List(ctx, &statefulSets, client.MatchingLabels{
		lifecycle.DefaultNamespaceLabel:          pool.Namespace,
		multiclusterRenderer.BrokerPoolLabelName: pool.Name,
	}); err != nil {
		return ctrl.Result{}, err
	}

	var sts *appsv1.StatefulSet
	if len(statefulSets.Items) > 0 {
		sts = &statefulSets.Items[0]
	}

	if sts == nil {
		status.SetDeployed(statuses.RedpandaBrokerPoolDeployedReasonNotDeployed)
	}

	originalPoolGeneration := pool.Status.DeployedGeneration
	originalPoolStatus := pool.Status.EmbeddedBrokerPoolStatus
	pool.Status.EmbeddedBrokerPoolStatus = redpandav1alpha2.EmbeddedBrokerPoolStatus{}

	if sts != nil {
		stsLabels := sts.GetLabels()
		if stsLabels != nil {
			generationString := stsLabels[multiclusterRenderer.BrokerPoolLabelGeneration]
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

		specReplicas := ptr.Deref(sts.Spec.Replicas, 0)
		if specReplicas != 0 && specReplicas == sts.Status.ReadyReplicas {
			status.SetDeployed(statuses.RedpandaBrokerPoolDeployedReasonDeployed)
		} else {
			status.SetDeployed(statuses.RedpandaBrokerPoolDeployedReasonScaling)
		}

		pool.Status.EmbeddedBrokerPoolStatus = redpandav1alpha2.EmbeddedBrokerPoolStatus{
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

	if status.UpdateConditions(pool) ||
		!reflect.DeepEqual(originalPoolStatus, pool.Status.EmbeddedBrokerPoolStatus) ||
		(pool.Status.DeployedGeneration != originalPoolGeneration) {
		return ignoreConflict(k8sClient.Status().Update(ctx, pool))
	}

	return ctrl.Result{}, nil
}

func (r *BrokerPoolReconciler) getRedpandaCluster(
	ctx context.Context,
	req mcreconcile.Request,
	pool *redpandav1alpha2.RedpandaBrokerPool,
	k8sClient client.Client,
) error {
	return k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, &redpandav1alpha2.StretchCluster{})
}
