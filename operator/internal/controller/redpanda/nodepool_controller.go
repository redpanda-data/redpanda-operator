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
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
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
	mgr.GetLogger().WithName("SetupWithMultiClusterManager").Info("registering NodePool controller", "knownClusters", createCanonicalClusterNameList(mgr))
	return mcbuilder.ControllerManagedBy(mgr).
		WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
			// Mirrors SetupMulticlusterController: the multicluster
			// runtime currently doesn't hand the global option off to
			// controller registration cleanly, so booting more than one
			// manager in-process (e.g. the integration test env with
			// three raft peers) hits "controller with name X already
			// exists". Disabling name validation here matches the
			// StretchCluster reconciler's setup and is safe in
			// production where there is only one manager per process.
			SkipNameValidation: ptr.To(true),
		}).
		For(
			&redpandav1alpha2.NodePool{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Watches(&redpandav1alpha2.StretchCluster{}, func(_ string, _ cluster.Cluster) mchandler.EventHandler {
			return mchandler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, object client.Object) []mcreconcile.Request {
				l := log.FromContext(ctx).WithName("NodePoolReconciler.StretchClusterWatch").V(log.TraceLevel)
				l.Info("StretchCluster event received", "stretchCluster", client.ObjectKeyFromObject(object).String(), "knownClusters", createCanonicalClusterNameList(mgr))
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
					l.Info("listed NodePools", "cluster", lifecycle.CanonicalClusterName(clusterName, mgr.GetLocalClusterName), "count", len(nodePools.Items))
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
			observability.Wrap[mcreconcile.Request](
				&NodePoolReconciler{Manager: mgr},
				"NodePool",
				periodicRequeue,
			),
		)
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

	if err := r.setExternalAccessReady(ctx, k8sClient, pool, &status); err != nil {
		return ctrl.Result{}, err
	}

	if status.UpdateConditions(pool) ||
		!reflect.DeepEqual(originalPoolStatus, pool.Status.EmbeddedNodePoolStatus) ||
		(pool.Status.DeployedGeneration != originalPoolGeneration) {
		return ignoreConflict(k8sClient.Status().Update(ctx, pool))
	}

	return ctrl.Result{}, nil
}

// setExternalAccessReady writes the ExternalAccessReady condition based on
// this pool's external configuration and any cross-pool nodePort conflicts
// with sibling pools in the same Kubernetes cluster.
//
// Reasons:
//   - Disabled: this pool has external access turned off (or no external
//     Service requested), so there is nothing to provision.
//   - NodePortConflict: this pool is stretch-cluster-backed and another
//     local sibling pool already claims one of the requested nodePort
//     numbers. The lexically-first pool wins; subsequent pools surface
//     this condition until the user disambiguates with explicit
//     external.advertisedPorts overrides.
//   - Available: external access is configured and no conflict was
//     detected. Single-cluster pools also get Available here — the v1
//     chart renderer only produces one external Service per Redpanda
//     cluster, so cross-pool nodePort conflicts in that path are not
//     possible.
func (r *NodePoolReconciler) setExternalAccessReady(
	ctx context.Context,
	k8sClient client.Client,
	pool *redpandav1alpha2.NodePool,
	status *statuses.NodePoolStatus,
) error {
	ext := pool.Spec.External
	extWanted := ext != nil && ext.IsEnabled() && (ext.Service == nil || ext.Service.IsEnabled())
	if !extWanted {
		status.SetExternalAccessReady(statuses.NodePoolExternalAccessReadyReasonDisabled)
		return nil
	}

	if !pool.Spec.ClusterRef.IsStretchCluster() {
		status.SetExternalAccessReady(statuses.NodePoolExternalAccessReadyReasonAvailable)
		return nil
	}

	var siblings redpandav1alpha2.NodePoolList
	if err := k8sClient.List(ctx, &siblings, client.InNamespace(pool.Namespace)); err != nil {
		return err
	}
	var localPools []*redpandav1alpha2.NodePool
	for i := range siblings.Items {
		sp := &siblings.Items[i]
		if !sp.Spec.ClusterRef.IsStretchCluster() {
			continue
		}
		if sp.Spec.ClusterRef.Name != pool.Spec.ClusterRef.Name {
			continue
		}
		localPools = append(localPools, sp)
	}

	// Fall through with sc=nil when the parent is briefly missing — the
	// pool is technically unbound (Bound=False covers that), and we want
	// ExternalAccessReady to still transition off the default
	// "NotReconciled" reason so consumers see a real verdict. With nil
	// cluster DetectExternalNodePortConflicts can't apply cluster
	// defaults, but the per-pool listener/external defaults still kick in
	// from MergeDefaultsFrom(zero-value) so collision detection runs the
	// same way it would once the parent reappears.
	var sc *redpandav1alpha2.StretchCluster
	var fetched redpandav1alpha2.StretchCluster
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: pool.Spec.ClusterRef.Name, Namespace: pool.Namespace}, &fetched); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		sc = &fetched
	}

	conflicts := rendermulticluster.DetectExternalNodePortConflicts(sc, localPools)
	for _, c := range conflicts {
		if c.Pool != pool.Name {
			continue
		}
		status.SetExternalAccessReady(
			statuses.NodePoolExternalAccessReadyReasonNodePortConflict,
			fmt.Sprintf("conflicts with NodePool %q on nodePort(s) %v; override external.advertisedPorts on one of the pools to disambiguate", c.ConflictsWith, c.Ports),
		)
		return nil
	}
	status.SetExternalAccessReady(statuses.NodePoolExternalAccessReadyReasonAvailable)
	return nil
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
