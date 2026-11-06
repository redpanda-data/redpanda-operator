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
	"fmt"
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

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
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
	// When set, pools without a StatefulSet fall back to deriving their status from Broker CRs.
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
		Watches(&appsv1.StatefulSet{}, mchandler.EnqueueRequestsFromMapFunc(poolRequestsForStatefulSet))
	if r.BrokerCREnabled {
		builder = builder.Watches(&redpandav1alpha2.Broker{}, mchandler.EnqueueRequestsFromMapFunc(poolRequestsForBroker))
	}
	// A RedpandaBrokerPool taking or returning the pool changes this
	// NodePool's status.
	builder = builder.Watches(&redpandav1alpha2.RedpandaBrokerPool{}, mchandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
	}))
	for _, clusterName := range mgr.GetClusterNames() {
		enqueueNodePoolFromCluster, err := controller.RegisterClusterSourceIndex(ctx, mgr, controller.PoolIndex, clusterName, &redpandav1alpha2.NodePool{}, &redpandav1alpha2.NodePoolList{})
		if err != nil {
			return err
		}

		builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueNodePoolFromCluster, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, observability.Wrap[mcreconcile.Request](r, "NodePool", periodicRequeue)))
}

// poolRequestsForStatefulSet maps a pool's StatefulSet to the same-named
// NodePool or RedpandaBrokerPool; each pool controller enqueues its own kind.
func poolRequestsForStatefulSet(_ context.Context, o client.Object) []reconcile.Request {
	labels := o.GetLabels()
	namespace := labels[lifecycle.DefaultNamespaceLabel]
	name := labels[redpandachart.NodePoolLabelName]
	if namespace == "" || name == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}}
}

// poolRequestsForBroker is poolRequestsForStatefulSet for a Broker CR.
func poolRequestsForBroker(_ context.Context, o client.Object) []reconcile.Request {
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
		pending, err := brokerPoolPendingInCache(ctx, k8sCluster.GetAPIReader(), k8sClient, pool)
		if err != nil {
			return ctrl.Result{}, err
		}
		if pending {
			logger.V(log.DebugLevel).Info("holding finalizer until the RedpandaBrokerPool taking the pool is cached")
			return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
		}

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

	clusterKey := types.NamespacedName{Namespace: req.Namespace, Name: pool.Spec.ClusterRef.Name}
	if err := r.getRedpandaCluster(ctx, req, pool, k8sClient); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		status.SetBound(statuses.NodePoolBoundReasonNotBound)
	} else {
		status.SetBound(statuses.NodePoolBoundReasonBound)
	}

	pools, err := controller.PoolsForCluster(ctx, k8sClient, clusterKey)
	if err != nil {
		return ctrl.Result{}, err
	}

	originalPoolGeneration := pool.Status.DeployedGeneration
	originalPoolStatus := pool.Status.EmbeddedNodePoolStatus

	if owner, ok := conversion.FindPool(pools, pool.Name); ok && owner.NodePool == nil {
		// Stable stays True through the handoff, so automation waiting on it
		// doesn't stall; the counts freeze until the NodePool is deleted.
		status.SetDeployed(statuses.NodePoolDeployedReasonDeployed, fmt.Sprintf("Pool is owned by RedpandaBrokerPool %s", client.ObjectKeyFromObject(owner.BrokerPool)))
	} else {
		obs, err := observePool(ctx, k8sClient, r.BrokerCREnabled, poolSource{
			name:            pool.Name,
			cluster:         clusterKey,
			desiredReplicas: ptr.Deref(pool.Spec.Replicas, 3),
			statefulSetLabels: client.MatchingLabels{
				lifecycle.DefaultNamespaceLabel: pool.Namespace,
				redpandachart.NodePoolLabelName: pool.Name,
			},
			brokerRef: (*redpandav1alpha2.ClusterRef).IsNodePool,
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		status.SetDeployed(map[poolDeployState]statuses.NodePoolDeployedCondition{
			poolNotDeployed: statuses.NodePoolDeployedReasonNotDeployed,
			poolScaling:     statuses.NodePoolDeployedReasonScaling,
			poolDeployed:    statuses.NodePoolDeployedReasonDeployed,
		}[obs.state])
		pool.Status.EmbeddedNodePoolStatus = obs.status
		if obs.deployedGeneration >= 0 {
			pool.Status.DeployedGeneration = obs.deployedGeneration
		}
	}

	if status.UpdateConditions(pool) ||
		!reflect.DeepEqual(originalPoolStatus, pool.Status.EmbeddedNodePoolStatus) ||
		(pool.Status.DeployedGeneration != originalPoolGeneration) {
		return ignoreConflict(k8sClient.Status().Update(ctx, pool))
	}

	return ctrl.Result{}, nil
}

// poolDeployState is how far a pool has deployed; each pool kind maps it to
// its own Deployed reason.
type poolDeployState int

const (
	poolNotDeployed poolDeployState = iota
	poolScaling
	poolDeployed
)

// poolSource locates a pool's StatefulSet or, in broker mode, its Broker CRs.
type poolSource struct {
	name string
	// cluster is the Redpanda that owns the pool's Broker CRs, which tells
	// them apart from a same-named pool's on another cluster.
	cluster           types.NamespacedName
	desiredReplicas   int32
	statefulSetLabels client.MatchingLabels
	// brokerRef matches the clusterRef kind of the pool's Broker CRs.
	brokerRef func(*redpandav1alpha2.ClusterRef) bool
}

// poolObservation is a pool's status as its StatefulSet, or its Broker CRs
// when it has none, report it.
type poolObservation struct {
	status redpandav1alpha2.EmbeddedNodePoolStatus
	// deployedGeneration is -1 when nothing reports one.
	deployedGeneration int64
	state              poolDeployState
}

func observePool(ctx context.Context, c client.Client, brokerCREnabled bool, src poolSource) (poolObservation, error) {
	obs := poolObservation{deployedGeneration: -1}

	var statefulSets appsv1.StatefulSetList
	if err := c.List(ctx, &statefulSets, src.statefulSetLabels); err != nil {
		return obs, err
	}
	if len(statefulSets.Items) > 0 {
		sts := &statefulSets.Items[0]
		if generationString := sts.GetLabels()[redpandachart.NodePoolLabelGeneration]; generationString != "" {
			// if we have a parsing error, just skip the generation propagation
			if generation, err := strconv.ParseInt(generationString, 10, 0); err == nil {
				obs.deployedGeneration = generation
			}
		}

		condemnedReplicas := max(sts.Status.Replicas-src.desiredReplicas, 0)

		obs.state = poolScaling
		if src.desiredReplicas == sts.Status.Replicas {
			obs.state = poolDeployed
		}

		obs.status = redpandav1alpha2.EmbeddedNodePoolStatus{
			Name:              src.name,
			Replicas:          sts.Status.Replicas,
			DesiredReplicas:   src.desiredReplicas,
			ReadyReplicas:     sts.Status.ReadyReplicas,
			RunningReplicas:   sts.Status.AvailableReplicas,
			UpToDateReplicas:  sts.Status.UpdatedReplicas,
			OutOfDateReplicas: sts.Status.Replicas - sts.Status.UpdatedReplicas,
			CondemnedReplicas: condemnedReplicas,
		}
		return obs, nil
	}

	if !brokerCREnabled {
		return obs, nil
	}

	var brokerList redpandav1alpha2.BrokerList
	if err := c.List(ctx, &brokerList, client.InNamespace(src.cluster.Namespace), client.MatchingLabels{
		redpandav1alpha2.NodePoolLabel: src.name,
	}); err != nil {
		return obs, err
	}
	if brokers := brokersForPool(brokerList.Items, src.cluster.Name, src.name, src.brokerRef); len(brokers) > 0 {
		obs.status, obs.deployedGeneration, obs.state = brokerBackedPoolStatus(src.name, src.desiredReplicas, brokers)
	}
	return obs, nil
}

func brokersForPool(brokers []redpandav1alpha2.Broker, clusterName, poolName string, isRef func(*redpandav1alpha2.ClusterRef) bool) []redpandav1alpha2.Broker {
	var owned []redpandav1alpha2.Broker
	for _, b := range brokers {
		owner := metav1.GetControllerOf(&b)
		if owner == nil || owner.Kind != redpandav1alpha2.RedpandaKind || owner.Name != clusterName {
			continue
		}
		if isRef(&b.Spec.ClusterRef) && b.Spec.ClusterRef.Name == poolName {
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
func brokerBackedPoolStatus(poolName string, desiredReplicas int32, brokers []redpandav1alpha2.Broker) (embedded redpandav1alpha2.EmbeddedNodePoolStatus, deployedGeneration int64, state poolDeployState) {
	deployedGeneration = -1
	var replicas, ready, upToDate, condemned int32
	for i := range brokers {
		b := &brokers[i]
		if b.IsDiskLost() {
			// A dead incarnation awaiting cleanup is not a live replica —
			// its replacement (same network index) carries the pool state.
			continue
		}
		if generationString := b.Labels[redpandachart.NodePoolLabelGeneration]; generationString != "" {
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

	state = poolScaling
	if desiredReplicas == replicas {
		state = poolDeployed
	}

	return redpandav1alpha2.EmbeddedNodePoolStatus{
		Name:              poolName,
		Replicas:          replicas,
		DesiredReplicas:   desiredReplicas,
		ReadyReplicas:     ready,
		RunningReplicas:   ready,
		UpToDateReplicas:  upToDate,
		OutOfDateReplicas: replicas - upToDate,
		CondemnedReplicas: condemned,
	}, deployedGeneration, state
}

// brokerPoolPendingInCache reports whether a RedpandaBrokerPool that takes
// np's pool exists but hasn't reached the cache yet. Every controller in the
// process reads that cache, so releasing np before then lets the Redpanda
// controller see the pool with no owner and scale it down. With no such
// RedpandaBrokerPool, deleting np deletes the pool.
func brokerPoolPendingInCache(ctx context.Context, live, cached client.Reader, np *redpandav1alpha2.NodePool) (bool, error) {
	key := client.ObjectKeyFromObject(np)

	var bp redpandav1alpha2.RedpandaBrokerPool
	if err := live.Get(ctx, key, &bp); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	ref := bp.Spec.ClusterRef
	if !bp.DeletionTimestamp.IsZero() || !ref.IsV2() || ref.Name != np.Spec.ClusterRef.Name || ref.GetNamespace(bp.Namespace) != bp.Namespace {
		return false, nil
	}

	if err := cached.Get(ctx, key, &redpandav1alpha2.RedpandaBrokerPool{}); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (r *NodePoolReconciler) getRedpandaCluster(
	ctx context.Context,
	req mcreconcile.Request,
	pool *redpandav1alpha2.NodePool,
	k8sClient client.Client,
) error {
	return k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: req.Namespace}, &redpandav1alpha2.Redpanda{})
}
