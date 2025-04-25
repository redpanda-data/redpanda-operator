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
	"errors"
	"fmt"
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	return &ResourceClient[T, U]{
		client:                 mgr.GetClient(),
		scheme:                 mgr.GetScheme(),
		mapper:                 mgr.GetRESTMapper(),
		ownershipResolver:      ownershipResolver,
		statusUpdater:          statusUpdater,
		nodePoolRenderer:       nodePoolRenderer,
		simpleResourceRenderer: simpleResourceRenderer,
	}
}

// ResourceClient is a client used to manage dependent resources,
// both simple and node pools, for a given cluster type.
type ResourceClient[T any, U Cluster[T]] struct {
	client                 client.Client
	scheme                 *runtime.Scheme
	mapper                 meta.RESTMapper
	ownershipResolver      OwnershipResolver[T, U]
	statusUpdater          ClusterStatusUpdater[T, U]
	nodePoolRenderer       NodePoolRenderer[T, U]
	simpleResourceRenderer SimpleResourceRenderer[T, U]
}

// PatchNodePoolSet updates a StatefulSet for a specific node pool.
func (r *ResourceClient[T, U]) PatchNodePoolSet(ctx context.Context, owner U, set *appsv1.StatefulSet) error {
	return r.patchOwnedResource(ctx, owner, set)
}

// SetClusterStatus sets the status of the given cluster.
func (r *ResourceClient[T, U]) SetClusterStatus(cluster U, status *ClusterStatus) bool {
	return r.statusUpdater.Update(cluster, status)
}

type gvkObject struct {
	gvk schema.GroupVersionKind
	nn  types.NamespacedName
}

// SyncAll synchronizes the simple resources associated with the given cluster,
// cleaning up any resources that should no longer exist.
func (r *ResourceClient[T, U]) SyncAll(ctx context.Context, owner U) error {
	// we don't sync node pools here
	resources, err := r.listAllOwnedResources(ctx, owner, false)
	if err != nil {
		return err
	}
	toDelete := map[gvkObject]client.Object{}
	for _, resource := range resources {
		toDelete[gvkObject{
			gvk: resource.GetObjectKind().GroupVersionKind(),
			nn:  client.ObjectKeyFromObject(resource),
		}] = resource
	}

	toSync, err := r.simpleResourceRenderer.Render(ctx, owner)
	if err != nil {
		return err
	}

	// attempt to create as many resources in one pass as we can
	errs := []error{}

	for _, resource := range toSync {
		if err := r.patchOwnedResource(ctx, owner, resource); err != nil {
			errs = append(errs, err)
		}
		delete(toDelete, gvkObject{
			gvk: resource.GetObjectKind().GroupVersionKind(),
			nn:  client.ObjectKeyFromObject(resource),
		})
	}

	for _, resource := range toDelete {
		if err := r.client.Delete(ctx, resource); err != nil {
			if !k8sapierrors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
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
func (r *ResourceClient[T, U]) WatchResources(builder Builder, cluster U) error {
	// set that this is for the cluster
	builder.For(cluster)

	// set an Owns on node pool statefulsets
	builder.Owns(&appsv1.StatefulSet{})

	for _, resourceType := range r.simpleResourceRenderer.WatchedResourceTypes() {
		mapping, err := getResourceScope(r.mapper, r.scheme, resourceType)
		if err != nil {
			if !apimeta.IsNoMatchError(err) {
				return err
			}
			// we have a no match error, so just drop the watch altogether
			continue
		}

		if mapping.Name() == meta.RESTScopeNamespace.Name() {
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
	// since this is a widespread deletion, we can delete even stateful sets
	resources, err := r.listAllOwnedResources(ctx, owner, true)
	if err != nil {
		return false, err
	}

	alive := []client.Object{}
	for _, o := range resources {
		if o.GetDeletionTimestamp() == nil {
			alive = append(alive, o)
		}
	}

	// attempt to delete as many resources in one pass as we can
	errs := []error{}
	for _, resource := range alive {
		if err := r.client.Delete(ctx, resource); err != nil {
			errs = append(errs, err)
		}
	}

	return len(alive) > 0, errors.Join(errs...)
}

// listResources lists resources of a specific type and object, returning them as an array.
func (r *ResourceClient[T, U]) listResources(ctx context.Context, object client.Object, opts ...client.ListOption) ([]client.Object, error) {
	kind, err := getGroupVersionKind(r.client.Scheme(), object)
	if err != nil {
		return nil, err
	}

	olist, err := r.client.Scheme().New(schema.GroupVersionKind{
		Group:   kind.Group,
		Version: kind.Version,
		Kind:    kind.Kind + "List",
	})
	if err != nil {
		return nil, fmt.Errorf("initializing list: %w", err)
	}
	list, ok := olist.(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("invalid object list type: %T", object)
	}

	if err := r.client.List(ctx, list, opts...); err != nil {
		// no-op list on unregistered resources, this happens when we
		// don't actually have a CRD installed for some resource type
		// we're trying to list
		if !apimeta.IsNoMatchError(err) {
			return nil, fmt.Errorf("listing resources: %w", err)
		}
	}

	converted := []client.Object{}
	items := reflect.ValueOf(list).Elem().FieldByName("Items")
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i).Addr().Interface().(client.Object)
		item.GetObjectKind().SetGroupVersionKind(*kind)
		converted = append(converted, item)
	}

	return sortCreation(converted), nil
}

// listAllOwnedResources lists all resources owned by a given cluster, optionally including node pools.
func (r *ResourceClient[T, U]) listAllOwnedResources(ctx context.Context, owner U, includeNodePools bool) ([]client.Object, error) {
	resources := []client.Object{}
	for _, resourceType := range append(r.simpleResourceRenderer.WatchedResourceTypes(), &appsv1.StatefulSet{}) {
		matching, err := r.listResources(ctx, resourceType, client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(owner)))
		if err != nil {
			return nil, err
		}
		filtered := []client.Object{}
		for i := range matching {
			object := matching[i]

			// filter out unowned resources
			mapping, err := getResourceScope(r.mapper, r.scheme, object)
			if err != nil {
				if !apimeta.IsNoMatchError(err) {
					return nil, err
				}

				// we have an unknown mapping so just ignore this
				continue
			}

			// isOwner defaults to true here because we don't set
			// owner refs on ClusterScoped resources. We only check
			// for ownership if it's namespace scoped.
			isOwner := true
			if mapping.Name() == apimeta.RESTScopeNameNamespace {
				isOwner = slices.ContainsFunc(object.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
					return ref.UID == owner.GetUID()
				})
			}

			// special case the node pools
			if (includeNodePools || !r.nodePoolRenderer.IsNodePool(object)) && isOwner {
				filtered = append(filtered, object)
			}
		}
		resources = append(resources, filtered...)
	}
	return resources, nil
}

// patchOwnedResource applies a patch to a resource owned by the cluster.
func (r *ResourceClient[T, U]) patchOwnedResource(ctx context.Context, owner U, object client.Object, extraLabels ...map[string]string) error {
	if err := r.normalize(object, owner, extraLabels...); err != nil {
		return err
	}
	return r.client.Patch(ctx, object, client.Apply, defaultFieldOwner, client.ForceOwnership)
}

// normalize normalizes an object by setting its labels and owner references. Any labels passed in as `extraLabels`
// will potentially override those set by the ownership resolver.
func (r *ResourceClient[T, U]) normalize(object client.Object, owner U, extraLabels ...map[string]string) error {
	kind, err := getGroupVersionKind(r.scheme, object)
	if err != nil {
		return err
	}

	unknownMapping := false

	mapping, err := getResourceScope(r.mapper, r.scheme, object)
	if err != nil {
		if !apimeta.IsNoMatchError(err) {
			return err
		}

		// we have an unknown mapping so err on the side of not setting
		// an owner reference
		unknownMapping = true
	}

	// nil out the managed fields since with some resources that actually do
	// a fetch (i.e. secrets that are created only once), we get an error trying
	// to patch a second time
	object.SetManagedFields(nil)

	// This needs to be set explicitly in order for SSA to function properly.
	// If an initialized pointer to a concrete CR has not specified its GVK
	// explicitly, SSA will fail.
	object.GetObjectKind().SetGroupVersionKind(*kind)

	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	for name, value := range r.ownershipResolver.AddLabels(owner) {
		labels[name] = value
	}

	for _, extra := range extraLabels {
		for name, value := range extra {
			labels[name] = value
		}
	}

	object.SetLabels(labels)

	if !unknownMapping && mapping.Name() == meta.RESTScopeNamespace.Name() {
		object.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
	}

	return nil
}

// fetchExistingPools fetches the existing pools (StatefulSets) for a given cluster.
func (r *ResourceClient[T, U]) fetchExistingPools(ctx context.Context, cluster U) ([]*poolWithOrdinals, error) {
	sets, err := r.listResources(ctx, &appsv1.StatefulSet{}, client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(cluster)))
	if err != nil {
		return nil, fmt.Errorf("listing StatefulSets: %w", err)
	}

	existing := []*poolWithOrdinals{}
	for _, set := range sets {
		statefulSet := set.(*appsv1.StatefulSet)

		if !r.nodePoolRenderer.IsNodePool(statefulSet) {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("constructing label selector: %w", err)
		}

		// based on https://github.com/kubernetes/kubernetes/blob/c90a4b16b6aa849ed362ee40997327db09e3a62d/pkg/controller/history/controller_history.go#L222
		revisions, err := r.listResources(ctx, &appsv1.ControllerRevision{}, client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("listing ControllerRevisions: %w", err)
		}
		ownedRevisions := []*appsv1.ControllerRevision{}
		for i := range revisions {
			ref := metav1.GetControllerOfNoCopy(revisions[i])
			if ref == nil || ref.UID == set.GetUID() {
				ownedRevisions = append(ownedRevisions, revisions[i].(*appsv1.ControllerRevision))
			}

		}

		pods, err := r.listResources(ctx, &corev1.Pod{}, client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("listing Pods: %w", err)
		}

		ownedPods := []*corev1.Pod{}
		for i := range pods {
			ownedPods = append(ownedPods, pods[i].(*corev1.Pod))
		}

		withOrdinals, err := sortPodsByOrdinal(ownedPods...)
		if err != nil {
			return nil, fmt.Errorf("sorting Pods by ordinal: %w", err)
		}

		existing = append(existing, &poolWithOrdinals{
			set:       statefulSet,
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
