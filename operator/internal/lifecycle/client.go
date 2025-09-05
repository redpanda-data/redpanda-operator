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
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

// Cluster is a generic interface for a pointer to a Kubernetes object
// that represents a cluster.
type Cluster[T any] interface {
	client.Object
	*T
}

// NewClusterObject creates a new instance of a typed cluster object.
func NewClusterObject[T any, U Cluster[T]]() U {
	var t T
	return U(&t)
}

// NewResourceClient creates a new instance of a ResourceClient for managing resources.
func NewResourceClient[T any, U Cluster[T]](mgr ctrl.Manager, resourcesFn ResourceManagerFactory[T, U]) *ResourceClient[T, U] {
	ownershipResolver, statusUpdater, nodePoolRenderer, simpleResourceRenderer := resourcesFn(mgr)
	ctl, err := kube.FromRESTConfig(mgr.GetConfig(), kube.Options{
		Options: client.Options{
			Scheme: mgr.GetScheme(),
			Mapper: mgr.GetRESTMapper(),
		},
		FieldManager: string(DefaultFieldOwner),
	})
	if err != nil {
		// NB: This is less than ideal but it's exceptionally unlikely that
		// FromRESTConfig actually returns an error and this method is only
		// ever called at initializtion time, not runtime.
		panic(err)
	}

	return &ResourceClient[T, U]{
		ctl:                    ctl,
		logger:                 mgr.GetLogger().WithName("ResourceClient"),
		ownershipResolver:      ownershipResolver,
		statusUpdater:          statusUpdater,
		nodePoolRenderer:       nodePoolRenderer,
		simpleResourceRenderer: simpleResourceRenderer,
	}
}

// ResourceClient is a client used to manage dependent resources,
// both simple and node pools, for a given cluster type.
type ResourceClient[T any, U Cluster[T]] struct {
	ctl                    *kube.Ctl
	logger                 logr.Logger
	ownershipResolver      OwnershipResolver[T, U]
	statusUpdater          ClusterStatusUpdater[T, U]
	nodePoolRenderer       NodePoolRenderer[T, U]
	simpleResourceRenderer SimpleResourceRenderer[T, U]
}

// PatchNodePoolSet updates a StatefulSet for a specific node pool.
func (r *ResourceClient[T, U]) PatchNodePoolSet(ctx context.Context, owner U, set *appsv1.StatefulSet) error {
	if set.GetLabels() == nil {
		set.SetLabels(map[string]string{})
	}
	maps.Copy(set.GetLabels(), r.ownershipResolver.AddLabels(owner))
	set.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
	return r.ctl.Apply(ctx, set, client.ForceOwnership)
}

// SetClusterStatus sets the status of the given cluster.
func (r *ResourceClient[T, U]) SetClusterStatus(cluster U, status *ClusterStatus) bool {
	return r.statusUpdater.Update(cluster, status)
}

type renderer[T any, U Cluster[T]] struct {
	SimpleResourceRenderer[T, U]
	Cluster U
}

func (r *renderer[T, U]) Render(ctx context.Context) ([]kube.Object, error) {
	return r.SimpleResourceRenderer.Render(ctx, r.Cluster)
}

func (r *renderer[T, U]) Types() []kube.Object {
	types := r.SimpleResourceRenderer.WatchedResourceTypes()
	return slices.DeleteFunc(types, func(o kube.Object) bool {
		_, ok := o.(*appsv1.StatefulSet)
		return ok
	})
}

func (r *ResourceClient[T, U]) syncer(owner U) *kube.Syncer {
	return &kube.Syncer{
		Ctl:       r.ctl,
		Namespace: owner.GetNamespace(),
		Renderer: &renderer[T, U]{
			Cluster:                owner,
			SimpleResourceRenderer: r.simpleResourceRenderer,
		},
		OwnershipLabels: r.ownershipResolver.GetOwnerLabels(owner),
		Preprocess: func(o kube.Object) {
			if o.GetLabels() == nil {
				o.SetLabels(map[string]string{})
			}
			maps.Copy(o.GetLabels(), r.ownershipResolver.AddLabels(owner))
		},
		Owner: *metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind()),
	}
}

// SyncAll synchronizes the simple resources associated with the given cluster,
// cleaning up any resources that should no longer exist.
func (r *ResourceClient[T, U]) SyncAll(ctx context.Context, owner U) error {
	_, err := r.syncer(owner).Sync(ctx)
	return err
}

// FetchExistingAndDesiredPools fetches the existing and desired node pools for a given cluster, returning
// a tracker that can be used for determining necessary operations on the pools.
func (r *ResourceClient[T, U]) FetchExistingAndDesiredPools(ctx context.Context, cluster U, configVersion string) (*PoolTracker, error) {
	pools := NewPoolTracker(cluster.GetGeneration())

	existingPools, err := r.fetchExistingPools(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("fetching existing pools: %w", err)
	}

	desired, err := r.nodePoolRenderer.Render(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("constructing desired pools: %w", err)
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
	pools.addDesired(desired...)

	return pools, nil
}

// Builder is an interface for our used methods of *sigs.k8s.io/controller-runtime/pkg/builder.Builder
type Builder interface {
	For(object client.Object, opts ...builder.ForOption) *builder.Builder
	Owns(object client.Object, opts ...builder.OwnsOption) *builder.Builder
	Watches(object client.Object, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *builder.Builder
}

// WatchResources configures resource watching for the given cluster, including StatefulSets and other resources.
func (r *ResourceClient[T, U]) WatchResources(builder Builder, cluster client.Object) error {
	// set that this is for the cluster
	builder.For(cluster)

	// set an Owns on node pool statefulsets
	builder.Owns(&appsv1.StatefulSet{})

	for _, resourceType := range r.simpleResourceRenderer.WatchedResourceTypes() {
		gvk, err := kube.GVKFor(r.ctl.Scheme(), resourceType)
		if err != nil {
			return err
		}

		mapping, err := r.ctl.ScopeOf(gvk)
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
			builder.Owns(resourceType)
			continue
		}

		// since resources are cluster-scoped we need to call a Watch on them with some
		// custom mappings
		builder.Watches(resourceType, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			if owner := r.ownershipResolver.OwnerForObject(o); owner != nil {
				return []reconcile.Request{{
					NamespacedName: *owner,
				}}
			}
			return nil
		}))

	}

	return nil
}

// DeleteAll deletes all resources owned by the given cluster, including node pools.
func (r *ResourceClient[T, U]) DeleteAll(ctx context.Context, owner U) (bool, error) {
	errs := []error{}
	allDeleted, err := r.syncer(owner).DeleteAll(ctx)
	if err != nil {
		errs = append(errs, err)
	}

	pools, err := r.fetchExistingPools(ctx, owner)
	if err != nil {
		return false, err
	}

	for _, pool := range pools {
		if pool.set.DeletionTimestamp != nil {
			allDeleted = false
		}

		if err := r.ctl.Delete(ctx, pool.set); err != nil {
			errs = append(errs, err)
		}
	}

	return allDeleted, errors.Join(errs...)
}

// fetchExistingPools fetches the existing pools (StatefulSets) for a given cluster.
func (r *ResourceClient[T, U]) fetchExistingPools(ctx context.Context, cluster U) ([]*poolWithOrdinals, error) {
	sets, err := kube.List[appsv1.StatefulSetList](ctx, r.ctl, client.InNamespace(cluster.GetNamespace()), client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(cluster)))
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
		revisions, err := kube.List[appsv1.ControllerRevisionList](ctx, r.ctl, client.MatchingLabelsSelector{
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

		pods, err := kube.List[corev1.PodList](ctx, r.ctl, client.MatchingLabelsSelector{
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
			set:       &statefulSet,
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
