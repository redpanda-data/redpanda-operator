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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/license"
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

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
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
// fields and finalizers on the RedpandaBrokerPool objects, rendering of RedpandaBrokerPools takes place within the
// StretchClusterReconciler or, for pools of a Redpanda cluster, the RedpandaReconciler.
//
//nolint:laconiccomments
type BrokerPoolReconciler struct {
	Manager multicluster.Manager
	// When set, pools of Redpanda clusters without a StatefulSet fall back
	// to deriving their status from Broker CRs.
	BrokerCREnabled bool
	// LicenseFilePath is the operator-level enterprise license that pools
	// of Redpanda clusters require.
	LicenseFilePath string

	// stretch selects pools of StretchClusters (the multicluster operator)
	// over pools of Redpanda clusters. The operator modes never share a
	// process, and each leaves the other's pools alone, finalizers included.
	stretch bool
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
				&BrokerPoolReconciler{Manager: mgr, stretch: true},
				name,
				periodicRequeue,
			),
		)
}

// SetupWithManager sets up the controller for pools of Redpanda clusters.
func (r *BrokerPoolReconciler) SetupWithManager(ctx context.Context, mgr multicluster.Manager, namespace string) error {
	builder := mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		SkipNameValidation: ptr.To(true),
	}).For(
		&redpandav1alpha2.RedpandaBrokerPool{},
		mcbuilder.WithEngageWithLocalCluster(true),
		mcbuilder.WithEngageWithProviderClusters(true),
	).
		Watches(&appsv1.StatefulSet{}, mchandler.EnqueueRequestsFromMapFunc(poolRequestsForStatefulSet)).
		// A NodePool releasing the pool, or taking it back, changes this
		// pool's status.
		Watches(&redpandav1alpha2.NodePool{}, mchandler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
		}))
	if r.BrokerCREnabled {
		builder = builder.Watches(&redpandav1alpha2.Broker{}, mchandler.EnqueueRequestsFromMapFunc(poolRequestsForBroker))
	}
	for _, clusterName := range mgr.GetClusterNames() {
		enqueueBrokerPoolFromCluster, err := controller.RegisterClusterSourceIndex(ctx, mgr, controller.PoolIndex, clusterName, &redpandav1alpha2.RedpandaBrokerPool{}, &redpandav1alpha2.RedpandaBrokerPoolList{})
		if err != nil {
			return err
		}

		builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueBrokerPoolFromCluster, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, observability.Wrap[mcreconcile.Request](r, "BrokerPool", periodicRequeue)))
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
	if r.stretch != pool.Spec.ClusterRef.IsStretchCluster() || (!r.stretch && !pool.Spec.ClusterRef.IsV2()) {
		return ctrl.Result{}, nil
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

	if !r.stretch {
		return r.reconcileRedpandaPoolStatus(ctx, pool, k8sClient)
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

		desiredReplicas := pool.GetReplicas()
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

// reconcileRedpandaPoolStatus is Reconcile's status pass for a pool of a
// Redpanda cluster. The pool reports the same counts a NodePool does, from
// the same StatefulSet, while it owns the pool.
func (r *BrokerPoolReconciler) reconcileRedpandaPoolStatus(ctx context.Context, pool *redpandav1alpha2.RedpandaBrokerPool, k8sClient client.Client) (ctrl.Result, error) {
	var status statuses.RedpandaBrokerPoolStatus

	clusterKey := types.NamespacedName{Namespace: pool.Spec.ClusterRef.GetNamespace(pool.Namespace), Name: pool.Spec.ClusterRef.Name}
	if err := k8sClient.Get(ctx, clusterKey, &redpandav1alpha2.Redpanda{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		status.SetBound(statuses.RedpandaBrokerPoolBoundReasonNotBound)
	} else {
		status.SetBound(statuses.RedpandaBrokerPoolBoundReasonBound)
	}

	pools, err := controller.PoolsForCluster(ctx, k8sClient, clusterKey)
	if err != nil {
		return ctrl.Result{}, err
	}
	owner, _ := conversion.FindPool(pools, pool.Name)

	originalPoolGeneration := pool.Status.DeployedGeneration
	originalPoolStatus := pool.Status.EmbeddedBrokerPoolStatus

	var obs poolObservation
	if owner.NodePool == nil {
		obs, err = observePool(ctx, k8sClient, r.BrokerCREnabled, poolSource{
			name: pool.Name,
			// The Redpanda chart's default, which an unset replicas renders.
			desiredReplicas: ptr.Deref(pool.Spec.Replicas, 3),
			// The owner label tells this pool's StatefulSet apart from a
			// same-named pool's on another cluster in the namespace.
			statefulSetLabels: client.MatchingLabels{
				lifecycle.DefaultNamespaceLabel: clusterKey.Namespace,
				lifecycle.DefaultOwnerLabel:     clusterKey.Name,
				redpandachart.NodePoolLabelName: pool.Name,
			},
			brokerNamespace: clusterKey.Namespace,
			brokerRef:       (*redpandav1alpha2.ClusterRef).IsBrokerPool,
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		if obs.deployedGeneration >= 0 {
			pool.Status.DeployedGeneration = obs.deployedGeneration
		}
	}
	pool.Status.EmbeddedBrokerPoolStatus = redpandav1alpha2.EmbeddedBrokerPoolStatus(obs.status)

	// The license is checked first so it surfaces before a handoff, which
	// would suspend the cluster's pools.
	if err := brokerPoolLicenseError(r.LicenseFilePath); err != nil {
		status.SetDeployed(statuses.RedpandaBrokerPoolDeployedReasonLicenseInvalid, fmt.Sprintf("Managing a RedpandaBrokerPool requires an enterprise license: %v", err))
	} else if owner.NodePool != nil {
		status.SetDeployed(statuses.RedpandaBrokerPoolDeployedReasonSupersededByNodePool, supersededMessage(owner.NodePool, pool))
	} else {
		status.SetDeployed(map[poolDeployState]statuses.RedpandaBrokerPoolDeployedCondition{
			poolNotDeployed: statuses.RedpandaBrokerPoolDeployedReasonNotDeployed,
			poolScaling:     statuses.RedpandaBrokerPoolDeployedReasonScaling,
			poolDeployed:    statuses.RedpandaBrokerPoolDeployedReasonDeployed,
		}[obs.state])
	}

	if status.UpdateConditions(pool) ||
		!reflect.DeepEqual(originalPoolStatus, pool.Status.EmbeddedBrokerPoolStatus) ||
		(pool.Status.DeployedGeneration != originalPoolGeneration) {
		return ignoreConflict(k8sClient.Status().Update(ctx, pool))
	}

	return ctrl.Result{}, nil
}

// supersededMessage tells the user how to hand np's pool to bp and whether
// doing so changes the pool's brokers.
func supersededMessage(np *redpandav1alpha2.NodePool, bp *redpandav1alpha2.RedpandaBrokerPool) string {
	msg := fmt.Sprintf("NodePool %s owns this pool; annotate it with %s: \"true\" or delete it to hand the pool to this RedpandaBrokerPool", client.ObjectKeyFromObject(np), redpandav1alpha2.MigrateToBrokerPoolAnnotation)
	if fields := differingFields(np, bp); len(fields) > 0 {
		return fmt.Sprintf("%s. The specs differ in %s, so the handoff applies this RedpandaBrokerPool's values", msg, strings.Join(fields, ", "))
	}
	return msg + ". The specs match, so the handoff leaves the brokers unchanged"
}

// differingFields lists the spec fields, by YAML key, in which bp differs
// from np.
func differingFields(np *redpandav1alpha2.NodePool, bp *redpandav1alpha2.RedpandaBrokerPool) []string {
	want := jsonFields(conversion.NodePoolToBrokerPool(np).Spec.EmbeddedBrokerPoolSpec)
	have := jsonFields(bp.Spec.EmbeddedBrokerPoolSpec)

	keys := slices.Collect(maps.Keys(want))
	for key := range have {
		if _, ok := want[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	var fields []string
	for _, key := range keys {
		if !bytes.Equal(want[key], have[key]) {
			fields = append(fields, key)
		}
	}
	return fields
}

func jsonFields(spec redpandav1alpha2.EmbeddedBrokerPoolSpec) map[string]json.RawMessage {
	// Unset renders the chart's default.
	spec.Replicas = ptr.To(ptr.Deref(spec.Replicas, 3))

	var fields map[string]json.RawMessage
	data, err := json.Marshal(spec)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil
	}
	return fields
}

// brokerPoolSuspension returns why a cluster's pools are frozen, or "" if
// they aren't: a pool owned by a RedpandaBrokerPool needs an enterprise
// license. A missing license freezes the pools rather than dropping them
// from the desired set, which would decommission their brokers.
func brokerPoolSuspension(pools []conversion.OwnedPool, licenseFilePath string) string {
	var owners []string
	for _, p := range pools {
		if p.NodePool == nil {
			owners = append(owners, p.BrokerPool.Name)
		}
	}
	if len(owners) == 0 {
		return ""
	}
	err := brokerPoolLicenseError(licenseFilePath)
	if err == nil {
		return ""
	}
	return fmt.Sprintf("RedpandaBrokerPools %s require an enterprise license: %v; changes to this cluster's resources and brokers are suspended until one is configured", strings.Join(owners, ", "), err)
}

// brokerPoolLicenseError reports why the operator-level license does not
// allow managing RedpandaBrokerPools, or nil if it does.
func brokerPoolLicenseError(licenseFilePath string) error {
	if licenseFilePath == "" {
		return errors.New("no license configured: set enterprise.licenseSecretRef in the operator Helm chart values")
	}

	l, err := license.ReadLicense(licenseFilePath)
	if err != nil {
		return errors.Wrap(err, "reading license")
	}
	if err := license.CheckExpiration(l.Expires()); err != nil {
		return errors.Wrap(err, "license expired")
	}
	if !l.AllowsEnterpriseFeatures() {
		return errors.New("license does not allow enterprise features")
	}
	return nil
}
