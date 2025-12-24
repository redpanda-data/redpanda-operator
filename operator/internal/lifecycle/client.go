// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

// Cluster is a generic interface for a pointer to a Kubernetes object
// that represents a cluster.
type Cluster[T any] interface {
	client.Object
	*T
}

type MultiCluster[T any] interface {
	Cluster[T]
	GetClusters() []string
}

type multi interface {
	GetClusters() []string
}

// NewClusterObject creates a new instance of a typed cluster object.
func NewClusterObject[T any, U Cluster[T]]() U {
	var t T
	return U(&t)
}

// NewMulticlusterResourceClient
// mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
func NewMulticlusterResourceClient[T any, U MultiCluster[T]](mgr multicluster.Manager, resourcesFn MulticlusterResourceManagerFactory[T, U]) *ResourceClient[T, U] {
	ownershipResolver, statusUpdater, nodePoolRenderer, simpleResourceRenderer := resourcesFn(mgr)
	return &ResourceClient[T, U]{
		manager:                mgr,
		logger:                 mgr.GetLogger().WithName("MulticlusterResourceClient"),
		ownershipResolver:      ownershipResolver,
		statusUpdater:          statusUpdater,
		nodePoolRenderer:       nodePoolRenderer,
		simpleResourceRenderer: simpleResourceRenderer,
		traceLogging:           true,
	}
}

// NewResourceClient creates a new instance of a ResourceClient for managing resources.
func NewResourceClient[T any, U Cluster[T]](mgr multicluster.Manager, resourcesFn ResourceManagerFactory[T, U]) *ResourceClient[T, U] {
	ownershipResolver, statusUpdater, nodePoolRenderer, simpleResourceRenderer := resourcesFn(mgr.GetLocalManager())

	return &ResourceClient[T, U]{
		manager:                mgr,
		logger:                 mgr.GetLogger().WithName("ResourceClient"),
		ownershipResolver:      ownershipResolver,
		statusUpdater:          statusUpdater,
		nodePoolRenderer:       nodePoolRenderer,
		simpleResourceRenderer: simpleResourceRenderer,
		traceLogging:           true,
	}
}

// ResourceClient is a client used to manage dependent resources,
// both simple and node pools, for a given cluster type.
type ResourceClient[T any, U Cluster[T]] struct {
	manager                multicluster.Manager
	logger                 logr.Logger
	traceLogging           bool
	ownershipResolver      OwnershipResolver[T, U]
	statusUpdater          ClusterStatusUpdater[T, U]
	nodePoolRenderer       NodePoolRenderer[T, U]
	simpleResourceRenderer SimpleResourceRenderer[T, U]
}

func (r *ResourceClient[T, U]) ctl(ctx context.Context, clusterName string) (*kube.Ctl, error) {
	cluster, err := r.manager.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	return kube.FromRESTConfig(cluster.GetConfig(), kube.Options{
		Options: client.Options{
			Scheme: cluster.GetScheme(),
			Mapper: cluster.GetRESTMapper(),
		},
		FieldManager: string(DefaultFieldOwner),
	})
}

func (r *ResourceClient[T, U]) clusterList(cluster any) []string {
	if mc, ok := cluster.(multi); ok {
		return mc.GetClusters()
	}
	return []string{mcmanager.LocalCluster}
}

// PatchlNodePoolSet updates a StatefulSet for a specific node pool.
func (r *ResourceClient[T, U]) PatchNodePoolSet(ctx context.Context, owner U, set *MulticlusterStatefulSet) error {
	ctl, err := r.ctl(ctx, set.clusterName)
	if err != nil {
		return err
	}

	if set.GetLabels() == nil {
		set.SetLabels(map[string]string{})
	}
	maps.Copy(set.GetLabels(), r.ownershipResolver.AddLabels(owner))
	set.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})

	return ctl.Apply(ctx, set.StatefulSet, client.ForceOwnership)
}

// SetClusterStatus sets the status of the given cluster.
func (r *ResourceClient[T, U]) SetClusterStatus(cluster U, status *ClusterStatus) bool {
	return r.statusUpdater.Update(cluster, status)
}

type renderer[T any, U Cluster[T]] struct {
	SimpleResourceRenderer[T, U]
	Cluster     U
	ClusterName string
}

func (r *renderer[T, U]) Render(ctx context.Context) ([]kube.Object, error) {
	return r.SimpleResourceRenderer.Render(ctx, r.Cluster, r.ClusterName)
}

func (r *renderer[T, U]) Types() []kube.Object {
	types := r.SimpleResourceRenderer.WatchedResourceTypes()
	return slices.DeleteFunc(types, func(o kube.Object) bool {
		_, ok := o.(*appsv1.StatefulSet)
		return ok
	})
}

func (r *ResourceClient[T, U]) syncer(ctx context.Context, owner U, clusterName string) (*kube.Syncer, error) {
	ctl, err := r.ctl(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	migratingResources := map[string]struct{}{}
	if mr, ok := r.simpleResourceRenderer.(MigratingRenderer); ok {
		for _, resource := range mr.MigratingResources() {
			gvk := resource.GetObjectKind().GroupVersionKind()
			migratingResources[gvk.String()] = struct{}{}
		}
	}

	return &kube.Syncer{
		Ctl:       ctl,
		Namespace: owner.GetNamespace(),
		Renderer: &renderer[T, U]{
			Cluster:                owner,
			SimpleResourceRenderer: r.simpleResourceRenderer,
		},
		MigratedResource: func(o kube.Object) bool {
			if _, ok := migratingResources[o.GetObjectKind().GroupVersionKind().String()]; ok {
				return true
			}
			return false
		},
		OwnershipLabels: r.ownershipResolver.GetOwnerLabels(owner),
		Preprocess: func(o kube.Object) {
			if o.GetLabels() == nil {
				o.SetLabels(map[string]string{})
			}
			maps.Copy(o.GetLabels(), r.ownershipResolver.AddLabels(owner))
		},
		Owner: *metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind()),
	}, nil
}

// SyncAll synchronizes the simple resources associated with the given cluster,
// cleaning up any resources that should no longer exist.
func (r *ResourceClient[T, U]) SyncAll(ctx context.Context, owner U) error {
	var syncErr error
	for _, clusterName := range r.clusterList(owner) {
		syncer, err := r.syncer(ctx, owner, clusterName)
		if err != nil {
			return err
		}
		_, err = syncer.Sync(ctx)
		syncErr = errors.Join(syncErr, err)
	}
	return syncErr
}

// FetchExistingAndDesiredPools fetches the existing and desired node pools for a given cluster, returning
// a tracker that can be used for determining necessary operations on the pools.
func (r *ResourceClient[T, U]) FetchExistingAndDesiredPools(ctx context.Context, cluster U, configVersion string) (*PoolTracker, error) {
	pools := NewPoolTracker(cluster.GetGeneration())

	for _, clusterName := range r.clusterList(cluster) {
		existingPools, err := r.fetchExistingPools(ctx, cluster, clusterName)
		if err != nil {
			return nil, fmt.Errorf("fetching existing pools: %w", err)
		}

		desired, err := r.nodePoolRenderer.Render(ctx, cluster, clusterName)
		if err != nil {
			return nil, fmt.Errorf("constructing desired pools: %w", err)
		}

		wrapped := []*MulticlusterStatefulSet{}
		for _, set := range desired {
			wrapped = append(wrapped, &MulticlusterStatefulSet{StatefulSet: set, clusterName: clusterName})
		}

		// ensure we have and OnDelete type for our statefulset
		for _, set := range desired {
			set.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
		}

		// normalize the config version label
		if configVersion != "" {
			for _, set := range desired {
				set.Labels = setConfigVersionLabels(set.Labels, configVersion)
				set.Spec.Template.Labels = setConfigVersionLabels(set.Spec.Template.Labels, configVersion)
			}
		}

		pools.addExisting(existingPools...)
		pools.addDesired(wrapped...)
	}

	return pools, nil
}

// Builder is an interface for our used methods of *sigs.k8s.io/controller-runtime/pkg/builder.Builder
type Builder interface {
	For(object client.Object, opts ...mcbuilder.ForOption) *mcbuilder.Builder
	Owns(object client.Object, opts ...mcbuilder.OwnsOption) *mcbuilder.Builder
	Watches(object client.Object, eventHandler mchandler.TypedEventHandlerFunc[client.Object, mcreconcile.Request], opts ...mcbuilder.WatchesOption) *mcbuilder.Builder
}

type loggingHandler[T client.Object] struct {
	handler.TypedEventHandler[T, mcreconcile.Request]
}

func (h *loggingHandler[T]) Create(ctx context.Context, evt event.TypedCreateEvent[T], wq workqueue.TypedRateLimitingInterface[mcreconcile.Request]) {
	h.TypedEventHandler.Create(ctx, evt, wrapQueue(ctx, evt.Object, wq))
}

func (h *loggingHandler[T]) Update(ctx context.Context, evt event.TypedUpdateEvent[T], wq workqueue.TypedRateLimitingInterface[mcreconcile.Request]) {
	h.TypedEventHandler.Update(ctx, evt, wrapQueue(ctx, evt.ObjectNew, wq))
}

func (h *loggingHandler[T]) Delete(ctx context.Context, evt event.TypedDeleteEvent[T], wq workqueue.TypedRateLimitingInterface[mcreconcile.Request]) {
	h.TypedEventHandler.Delete(ctx, evt, wrapQueue(ctx, evt.Object, wq))
}

func (h *loggingHandler[T]) Generic(ctx context.Context, evt event.TypedGenericEvent[T], wq workqueue.TypedRateLimitingInterface[mcreconcile.Request]) {
	h.TypedEventHandler.Generic(ctx, evt, wrapQueue(ctx, evt.Object, wq))
}

type wrappedAdder struct {
	obj client.Object
	ctx context.Context
	workqueue.TypedRateLimitingInterface[mcreconcile.Request]
}

func wrapQueue(ctx context.Context, obj client.Object, wq workqueue.TypedRateLimitingInterface[mcreconcile.Request]) workqueue.TypedRateLimitingInterface[mcreconcile.Request] {
	return &wrappedAdder{
		obj:                        obj,
		ctx:                        ctx,
		TypedRateLimitingInterface: wq,
	}
}

func (w *wrappedAdder) Add(item mcreconcile.Request) {
	log.FromContext(w.ctx).V(log.TraceLevel).Info("[enqueue] adding reconciliation request", "request", item, "due-to", reflect.TypeOf(w.obj).String(), "name", client.ObjectKeyFromObject(w.obj))
	w.TypedRateLimitingInterface.Add(item)
}

func wrapLoggingHandler[T client.Object](_ T, h handler.TypedEventHandler[T, mcreconcile.Request]) mchandler.TypedEventHandlerFunc[T, mcreconcile.Request] {
	return func(string, cluster.Cluster) handler.TypedEventHandler[T, mcreconcile.Request] {
		return &loggingHandler[T]{TypedEventHandler: h}
	}
}

// WatchResources configures resource watching for the given cluster, including StatefulSets and other resources.
func (r *ResourceClient[T, U]) WatchResources(builder Builder, cluster client.Object, clusterNames []string) error {
	// NB: we use localcluster here because the RESTMapper and scheme should be identical across all clusters
	ctl, err := r.ctl(context.Background(), mcmanager.LocalCluster)
	if err != nil {
		return err
	}

	// set that this is for the cluster
	builder.For(cluster)

	owns := func(obj client.Object) {
		if r.traceLogging {
			for _, clusterName := range clusterNames {
				loggingHandler := wrapLoggingHandler(obj, mchandler.ForCluster(handler.EnqueueRequestForOwner(ctl.Scheme(), ctl.RESTMapper(), cluster, handler.OnlyControllerOwner()), clusterName))
				builder.Watches(obj, loggingHandler, controller.WatchOptions(clusterName)...)
			}
		} else {
			builder.Owns(obj)
		}
	}

	// set an Owns on node pool statefulsets
	owns(&appsv1.StatefulSet{})

	for _, resourceType := range r.simpleResourceRenderer.WatchedResourceTypes() {
		gvk, err := kube.GVKFor(ctl.Scheme(), resourceType)
		if err != nil {
			return err
		}

		mapping, err := ctl.ScopeOf(gvk)
		if err != nil {
			if !apimeta.IsNoMatchError(err) {
				return err
			}

			// the WARNING messages here get logged constantly and are fairly static containing the resource type itself
			// so we can just use the global debouncer which debounces by error string
			log.DebounceError(r.logger, err, "WARNING no registered value for resource type found in cluster", "resourceType", resourceType.GetObjectKind().GroupVersionKind().String())

			// we have a no match error, so just drop the watch altogether
			continue
		}

		if mapping == apimeta.RESTScopeNameNamespace {
			// we're working with a namespace scoped resource, so we can work with ownership
			owns(resourceType)
			continue
		}

		for _, clusterName := range clusterNames {
			// since resources are cluster-scoped we need to call a Watch on them with some
			// custom mappings
			if r.traceLogging {
				builder.Watches(resourceType, wrapLoggingHandler(resourceType, mchandler.ForCluster(handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
					if owner := r.ownershipResolver.OwnerForObject(o); owner != nil {
						return []reconcile.Request{{
							NamespacedName: *owner,
						}}
					}
					return nil
				}), clusterName)), controller.WatchOptions(clusterName)...)
			} else {
				builder.Watches(resourceType, mchandler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
					if owner := r.ownershipResolver.OwnerForObject(o); owner != nil {
						return []reconcile.Request{{
							NamespacedName: *owner,
						}}
					}
					return nil
				}), controller.WatchOptions(clusterName)...)
			}
		}
	}

	return nil
}

// DeleteAll deletes all resources owned by the given cluster, including node pools.
func (r *ResourceClient[T, U]) DeleteAll(ctx context.Context, owner U) (bool, error) {
	allDeleted := true

	var deleteErr error
	for _, clusterName := range r.clusterList(owner) {
		var clusterResourcesDeleted bool
		syncer, err := r.syncer(ctx, owner, clusterName)
		if err != nil {
			deleteErr = errors.Join(deleteErr, err)
		} else {
			clusterResourcesDeleted, err = syncer.DeleteAll(ctx)
			if err != nil {
				deleteErr = errors.Join(deleteErr, err)
			}
		}

		ctl, err := r.ctl(ctx, clusterName)
		if err != nil {
			return false, err
		}

		pools, err := r.fetchExistingPools(ctx, owner, clusterName)
		if err != nil {
			return false, err
		}

		for _, pool := range pools {
			if pool.set.DeletionTimestamp != nil {
				clusterResourcesDeleted = false
			}

			if err := ctl.Delete(ctx, pool.set.StatefulSet); err != nil {
				deleteErr = errors.Join(deleteErr, err)
			}
		}

		if !clusterResourcesDeleted {
			allDeleted = false
		}
	}

	return allDeleted, deleteErr
}

// fetchExistingPools fetches the existing pools (StatefulSets) for a given cluster.
func (r *ResourceClient[T, U]) fetchExistingPools(ctx context.Context, cluster U, clusterName string) ([]*poolWithOrdinals, error) {
	ctl, err := r.ctl(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	sets, err := kube.List[appsv1.StatefulSetList](ctx, ctl, cluster.GetNamespace(), client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(cluster)))
	if err != nil {
		return nil, errors.Wrapf(err, "listing StatefulSets")
	}

	i := 0
	for _, set := range sets.Items {
		isOwned := slices.ContainsFunc(set.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.UID == cluster.GetUID()
		})
		if isOwned {
			sets.Items[i] = set
			i++
		}
	}
	sets.Items = sets.Items[:i]

	existing := []*poolWithOrdinals{}
	for _, statefulSet := range sets.Items {
		if !r.nodePoolRenderer.IsNodePool(&statefulSet) {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("constructing label selector: %w", err)
		}

		// based on https://github.com/kubernetes/kubernetes/blob/c90a4b16b6aa849ed362ee40997327db09e3a62d/pkg/controller/history/controller_history.go#L222
		revisions, err := kube.List[appsv1.ControllerRevisionList](ctx, ctl, cluster.GetNamespace(), client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "listing ControllerRevisions")
		}

		ownedRevisions := []*appsv1.ControllerRevision{}
		for i := range revisions.Items {
			ref := metav1.GetControllerOfNoCopy(&revisions.Items[i])
			if ref == nil || ref.UID == statefulSet.GetUID() {
				ownedRevisions = append(ownedRevisions, &revisions.Items[i])
			}
		}

		pods, err := kube.List[corev1.PodList](ctx, ctl, cluster.GetNamespace(), client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("listing Pods: %w", err)
		}

		ownedPods := make([]*corev1.Pod, len(pods.Items))
		for i := range pods.Items {
			ownedPods[i] = &pods.Items[i]
		}

		withOrdinals, err := sortPodsByOrdinal(ownedPods...)
		if err != nil {
			return nil, fmt.Errorf("sorting Pods by ordinal: %w", err)
		}

		existing = append(existing, &poolWithOrdinals{
			set: &MulticlusterStatefulSet{
				StatefulSet: &statefulSet,
				clusterName: clusterName,
			},
			revisions: sortRevisions(ownedRevisions),
			pods:      withOrdinals,
		})
	}

	return existing, nil
}

func setConfigVersionLabels(labels map[string]string, configVersion string) map[string]string {
	if labels == nil {
		return map[string]string{
			configVersionLabel: configVersion,
		}
	}

	labels[configVersionLabel] = configVersion
	return labels
}
