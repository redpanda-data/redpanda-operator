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
	"strings"
	"time"

	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// nodepool resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=nodepools/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// NodePoolReconciler reconciles a NodePool object. This reconciler in particular should only update status
// fields and finalizers on the NodePool objects, rendering of NodePools takes place within the RedpandaReconciler.
type NodePoolReconciler struct {
	Manager multicluster.Manager
	// BrokerCREnabled mirrors the operator's broker-controller flag. When
	// set, pools without a StatefulSet fall back to deriving their status
	// from Broker CRs; when unset the Broker CRD may not even be installed,
	// so the Broker watch and lookups are skipped entirely.
	BrokerCREnabled bool
}

func createCanonicalClusterNameList(mgr multicluster.Manager) []string {
	var canonicalClusterList []string
	for _, clusterName := range mgr.GetClusterNames() {
		canonicalClusterList = append(canonicalClusterList, lifecycle.CanonicalClusterName(clusterName, mgr.GetLocalClusterName))
	}
	return canonicalClusterList
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(ctx context.Context, mgr multicluster.Manager, namespace string) error {
	builder := mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		SkipNameValidation: ptr.To(true),
	}).For(
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
	if r.BrokerCREnabled {
		builder = builder.Watches(&redpandav1alpha2.Broker{}, mchandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			name := o.GetLabels()[redpandav1alpha2.NodePoolLabel]
			if name == "" || strings.EqualFold(name, redpandav1alpha2.DefaultNodePoolName) {
				return nil
			}

			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: o.GetNamespace(),
					Name:      name,
				},
			}}
		}))
	}
	for _, clusterName := range mgr.GetClusterNames() {
		enqueueNodePoolFromCluster, err := controller.RegisterClusterSourceIndex(ctx, mgr, "pool", clusterName, &redpandav1alpha2.NodePool{}, &redpandav1alpha2.NodePoolList{})
		if err != nil {
			return err
		}

		builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueNodePoolFromCluster, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, observability.Wrap[mcreconcile.Request](r, "NodePool", periodicRequeue)))
}

// Reconcile reconciles NodePool objects
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	l := log.FromContext(ctx).WithName("NodePoolReconciler.Reconcile").WithValues("object", req.NamespacedName.String(), "cluster", req.ClusterName)
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

	// Update our NodePool with our finalizer and any default Annotation FFs.
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

	// In broker mode the pool has no StatefulSet: its pods are managed by
	// Broker CRs. Fall back to deriving status from those. A live
	// StatefulSet stays authoritative (mid-migration shadow Brokers are
	// inert), matching lifecycle's broker-backed pool accounting.
	var brokers []redpandav1alpha2.Broker
	if sts == nil && r.BrokerCREnabled {
		var brokerList redpandav1alpha2.BrokerList
		if err := k8sClient.List(ctx, &brokerList, client.InNamespace(pool.Namespace), client.MatchingLabels{
			redpandav1alpha2.NodePoolLabel: pool.Name,
		}); err != nil {
			return ctrl.Result{}, err
		}
		brokers = brokersForNodePool(brokerList.Items, pool.Name)
	}

	if sts == nil && len(brokers) == 0 {
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

	if sts == nil && len(brokers) > 0 {
		embedded, deployedGeneration, reason := brokerBackedPoolStatus(pool, brokers)
		if deployedGeneration >= 0 {
			pool.Status.DeployedGeneration = deployedGeneration
		}
		status.SetDeployed(reason)
		pool.Status.EmbeddedNodePoolStatus = embedded
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

// brokersForNodePool keeps only the Brokers whose clusterRef names this
// NodePool. The pool-name label the Reconcile listing matches on is not
// unique across owners: a V1 Cluster with a broker-mode node pool of the
// same name in the same namespace stamps the identical label on ITS Brokers.
// Counting those would pollute this NodePool's replica counts and drag
// DeployedGeneration (a minimum) down indefinitely.
func brokersForNodePool(brokers []redpandav1alpha2.Broker, poolName string) []redpandav1alpha2.Broker {
	var owned []redpandav1alpha2.Broker
	for _, b := range brokers {
		if b.Spec.ClusterRef.IsNodePool() && b.Spec.ClusterRef.Name == poolName {
			owned = append(owned, b)
		}
	}
	return owned
}

// brokerBackedPoolStatus synthesizes the pool's status from its Broker CRs —
// the broker-mode analog of the StatefulSet-derived status, computed from
// Broker statuses alone (no pod reads). Field mapping:
//   - Replicas: brokers whose pod exists (PodScheduled reason is not
//     PodMissing; Unknown means not yet reconciled and does not count)
//   - ReadyReplicas / RunningReplicas: brokers with Ready=True — the Broker
//     controller sets it from the pod's Ready condition, i.e. the same
//     Kubernetes readiness probe StatefulSet readyReplicas counts, just read
//     through the Broker CR instead of the pod
//   - UpToDateReplicas: brokers with ConfigSynced=True (pod matches the
//     desired pod template across all rotation keys)
//   - CondemnedReplicas: brokers marked for decommission
//
// deployedGeneration is the minimum NodePoolLabelGeneration across the
// brokers (conservative during a metadata sync), or -1 when no broker
// carries the label yet.
func brokerBackedPoolStatus(pool *redpandav1alpha2.NodePool, brokers []redpandav1alpha2.Broker) (embedded redpandav1alpha2.EmbeddedNodePoolStatus, deployedGeneration int64, reason statuses.NodePoolDeployedCondition) {
	desiredReplicas := ptr.Deref(pool.Spec.Replicas, 3)

	deployedGeneration = -1
	var replicas, ready, upToDate, condemned int32
	for i := range brokers {
		b := &brokers[i]
		if b.IsDiskLost() {
			// A dead incarnation awaiting cleanup is not a live replica —
			// its replacement (same network index) carries the pool state.
			continue
		}
		if generationString := b.Labels[redpanda.NodePoolLabelGeneration]; generationString != "" {
			if generation, err := strconv.ParseInt(generationString, 10, 0); err == nil && (deployedGeneration < 0 || generation < deployedGeneration) {
				deployedGeneration = generation
			}
		}
		if c := apimeta.FindStatusCondition(b.Status.Conditions, statuses.BrokerPodScheduled); c != nil &&
			c.Status != metav1.ConditionUnknown && c.Reason != string(statuses.BrokerPodScheduledReasonPodMissing) {
			replicas++
		}
		if apimeta.IsStatusConditionTrue(b.Status.Conditions, statuses.BrokerReady) {
			ready++
		}
		if apimeta.IsStatusConditionTrue(b.Status.Conditions, statuses.BrokerConfigSynced) {
			upToDate++
		}
		if b.Spec.Decommission {
			condemned++
		}
	}

	reason = statuses.NodePoolDeployedReasonScaling
	if desiredReplicas == replicas {
		reason = statuses.NodePoolDeployedReasonDeployed
	}

	return redpandav1alpha2.EmbeddedNodePoolStatus{
		Name:              pool.Name,
		Replicas:          replicas,
		DesiredReplicas:   desiredReplicas,
		ReadyReplicas:     ready,
		RunningReplicas:   ready,
		UpToDateReplicas:  upToDate,
		OutOfDateReplicas: replicas - upToDate,
		CondemnedReplicas: condemned,
	}, deployedGeneration, reason
}

func (r *NodePoolReconciler) getRedpandaCluster(
	ctx context.Context,
	req mcreconcile.Request,
	pool *redpandav1alpha2.NodePool,
	k8sClient client.Client,
) error {
	return k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, &redpandav1alpha2.Redpanda{})
}
