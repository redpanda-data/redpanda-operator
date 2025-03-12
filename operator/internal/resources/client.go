// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const defaultFieldOwner = client.FieldOwner("cluster.redpanda.com/operator")

type Cluster[T any] interface {
	client.Object
	*T
}

func NewClusterObject[T any, U Cluster[T]]() U {
	var t T
	return U(&t)
}

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

func getGroupVersionKind(scheme *runtime.Scheme, object client.Object) (*schema.GroupVersionKind, error) {
	kinds, _, err := scheme.ObjectKinds(object)
	if err != nil {
		return nil, fmt.Errorf("fetching object kind: %w", err)
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("unable to determine object kind")
	}

	gvk := kinds[0]
	return &schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}, nil
}

func sortCreation[T client.Object](objects []T) []T {
	sort.SliceStable(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		aTimestamp, bTimestamp := ptr.To(a.GetCreationTimestamp()), ptr.To(b.GetCreationTimestamp())
		if aTimestamp.Equal(bTimestamp) {
			return a.GetName() < b.GetName()
		}
		return aTimestamp.Before(bTimestamp)
	})
	return objects
}

func getResourceScope(mapper meta.RESTMapper, scheme *runtime.Scheme, object client.Object) (meta.RESTScope, error) {
	gvk, err := getGroupVersionKind(scheme, object)
	if err != nil {
		return nil, err
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("unable to get REST mapping: %w", err)
	}

	return mapping.Scope, nil
}

type ResourceClient[T any, U Cluster[T]] struct {
	client                 client.Client
	scheme                 *runtime.Scheme
	mapper                 meta.RESTMapper
	ownershipResolver      OwnershipResolver[T, U]
	statusUpdater          ClusterStatusUpdater[T, U]
	nodePoolRenderer       NodePoolRenderer[T, U]
	simpleResourceRenderer SimpleResourceRenderer[T, U]
}

func (c *ResourceClient[T, U]) listResources(ctx context.Context, object client.Object, opts ...client.ListOption) ([]client.Object, error) {
	kind, err := getGroupVersionKind(c.client.Scheme(), object)
	if err != nil {
		return nil, err
	}
	kind.Kind += "List"

	olist, err := c.client.Scheme().New(*kind)
	if err != nil {
		return nil, fmt.Errorf("initializing list: %w", err)
	}
	list, ok := olist.(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("invalid object list type: %T", object)
	}

	if err := c.client.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("listing resources: %w", err)
	}

	converted := []client.Object{}
	items := reflect.ValueOf(list).Elem().FieldByName("Items")
	if items.IsZero() {
		return nil, fmt.Errorf("unable to get items")
	}
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i).Addr().Interface().(client.Object)
		converted = append(converted, item)
	}

	return sortCreation(converted), nil
}

func (r *ResourceClient[T, U]) listAllOwnedResources(ctx context.Context, owner U, includeNodePools bool) ([]client.Object, error) {
	resources := []client.Object{}
	for _, resourceType := range r.simpleResourceRenderer.WatchedResourceTypes() {
		matching, err := r.listResources(ctx, resourceType, client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(owner)))
		if err != nil {
			return nil, err
		}
		filtered := []client.Object{}
		for i := range matching {
			// special case the node pools
			if includeNodePools || !r.nodePoolRenderer.IsNodePool(matching[i]) {
				filtered = append(filtered, matching[i])
			}
		}
		resources = append(resources, filtered...)
	}
	return resources, nil
}

func (c *ResourceClient[T, U]) patchOwnedResource(ctx context.Context, owner U, object client.Object, extraLabels ...map[string]string) error {
	if err := c.normalize(object, owner, extraLabels...); err != nil {
		return err
	}
	return c.client.Patch(ctx, object, client.Apply, defaultFieldOwner, client.ForceOwnership)
}

func (c *ResourceClient[T, U]) PatchNodePoolSet(ctx context.Context, owner U, set *appsv1.StatefulSet) error {
	return c.patchOwnedResource(ctx, owner, set)
}

func (n *ResourceClient[T, U]) normalize(object client.Object, owner U, extraLabels ...map[string]string) error {
	kind, err := getGroupVersionKind(n.scheme, object)
	if err != nil {
		return err
	}
	mapping, err := getResourceScope(n.mapper, n.scheme, object)
	if err != nil {
		return err
	}

	object.GetObjectKind().SetGroupVersionKind(*kind)

	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	for name, value := range n.ownershipResolver.GetOwnerLabels(owner) {
		labels[name] = value
	}
	for _, extra := range extraLabels {
		for name, value := range extra {
			labels[name] = value
		}
	}

	object.SetLabels(labels)

	if mapping.Name() == meta.RESTScopeNamespace.Name() {
		object.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
	}

	return nil
}

func (r *ResourceClient[T, U]) SetClusterStatus(cluster U, status ClusterStatus) bool {
	return r.statusUpdater.Update(cluster, status)
}

func (r *ResourceClient[T, U]) SyncAll(ctx context.Context, owner U) error {
	// we don't sync node pools here
	resources, err := r.listAllOwnedResources(ctx, owner, false)
	if err != nil {
		return err
	}
	toDelete := map[types.NamespacedName]client.Object{}
	for _, resource := range resources {
		toDelete[client.ObjectKeyFromObject(resource)] = resource
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
		delete(toDelete, client.ObjectKeyFromObject(resource))
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

type Pool struct {
	StatefulSet *appsv1.StatefulSet
	Pods        []*corev1.Pod
	Revisions   []*appsv1.ControllerRevision
}

func (e *Pool) Name() string {
	return client.ObjectKeyFromObject(e.StatefulSet).String()
}

func (e *Pool) PodNames() []string {
	podNames := []string{}
	for _, pod := range e.Pods {
		podNames = append(podNames, client.ObjectKeyFromObject(pod).String())
	}
	return podNames
}

func (e *Pool) ControllerRevisionNames() []string {
	revisionNames := []string{}
	for _, revision := range e.Revisions {
		revisionNames = append(revisionNames, client.ObjectKeyFromObject(revision).String())
	}
	return revisionNames
}

func (r *ResourceClient[T, U]) fetchExistingPools(ctx context.Context, cluster U) ([]*Pool, error) {
	sets, err := r.listResources(ctx, &appsv1.StatefulSet{}, client.MatchingLabels(r.ownershipResolver.GetOwnerLabels(cluster)))
	if err != nil {
		return nil, fmt.Errorf("listing StatefulSets: %w", err)
	}

	existing := []*Pool{}
	for _, set := range sets {
		statefulSet := set.(*appsv1.StatefulSet)

		selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("constructing label selector: %w", err)
		}

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

		existing = append(existing, &Pool{
			StatefulSet: statefulSet,
			Revisions:   ownedRevisions,
			Pods:        ownedPods,
		})
	}

	return existing, nil
}

func (r *ResourceClient[T, U]) FetchExistingAndDesiredPools(ctx context.Context, cluster U) (*PoolTracker, error) {
	pools := NewPoolTracker(cluster.GetGeneration())

	existingPools, err := r.fetchExistingPools(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("fetching existing pools: %w", err)
	}

	if err := pools.AddExisting(existingPools...); err != nil {
		return nil, fmt.Errorf("adding existing pools: %w", err)
	}

	desired, err := r.nodePoolRenderer.Render(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("constructing desired pools: %w", err)
	}

	pools.AddDesired(desired...)

	return pools, nil
}

func (r *ResourceClient[T, U]) WatchResources(builder *builder.Builder, cluster U) error {
	// set an Owns on node pool statefulsets
	builder = builder.Owns(&appsv1.StatefulSet{})

	for _, resourceType := range r.simpleResourceRenderer.WatchedResourceTypes() {
		mapping, err := getResourceScope(r.mapper, r.scheme, resourceType)
		if err != nil {
			return err
		}

		if mapping.Name() == meta.RESTScopeNamespace.Name() {
			// we're working with a namespace scoped resource, so we can work with ownership
			builder = builder.Owns(resourceType)
			continue
		}

		// since resources are cluster-scoped we need to call a Watch on them with some
		// custom mappings
		builder = builder.Watches(resourceType, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
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
