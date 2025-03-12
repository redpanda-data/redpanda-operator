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

type Cluster[T any] interface {
	client.Object
	*T
}

func NewClusterObject[T any, U Cluster[T]]() U {
	var t T
	return U(&t)
}

type ResourceClient[T any, U Cluster[T]] interface {
	ListResources(ctx context.Context, resourceType client.Object, opts ...client.ListOption) ([]client.Object, error)
	ListOwnedResources(ctx context.Context, owner U, resourceType client.Object, opts ...client.ListOption) ([]client.Object, error)
	PatchOwnedResource(ctx context.Context, owner U, object client.Object, extraLabels ...map[string]string) error
	SyncAll(ctx context.Context, cluster U) error
	DeleteAll(ctx context.Context, cluster U) (bool, error)
	WatchResources(builder *builder.Builder, cluster U) error
	FetchExistingPools(ctx context.Context, cluster U) ([]*Pool, error)
}

func NewResourceClient[T any, U Cluster[T]](mgr ctrl.Manager, resources ResourceManager[T, U]) ResourceClient[T, U] {
	return &resourceClient[T, U]{
		client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		mapper:  mgr.GetRESTMapper(),
		manager: resources,
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

type resourceClient[T any, U Cluster[T]] struct {
	client  client.Client
	scheme  *runtime.Scheme
	mapper  meta.RESTMapper
	manager ResourceManager[T, U]
}

func (c *resourceClient[T, U]) listResources(ctx context.Context, object client.Object, opts ...client.ListOption) ([]client.Object, error) {
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

func (c *resourceClient[T, U]) listMatchingResources(ctx context.Context, object client.Object, labels map[string]string, opts ...client.ListOption) ([]client.Object, error) {
	return c.listResources(ctx, object, append([]client.ListOption{client.MatchingLabels(labels)}, opts...)...)
}

func (r *resourceClient[T, U]) ListResources(ctx context.Context, resourceType client.Object, opts ...client.ListOption) ([]client.Object, error) {
	return r.listResources(ctx, resourceType, opts...)
}

func (r *resourceClient[T, U]) ListOwnedResources(ctx context.Context, owner U, resourceType client.Object, opts ...client.ListOption) ([]client.Object, error) {
	return r.listMatchingResources(ctx, resourceType, r.manager.OwnerLabels(owner), opts...)
}

func (r *resourceClient[T, U]) listAllOwnedResources(ctx context.Context, owner U) ([]client.Object, error) {
	resources := []client.Object{}
	for _, resourceType := range r.manager.OwnedResourceTypes(owner) {
		matching, err := r.listMatchingResources(ctx, resourceType, r.manager.OwnerLabels(owner))
		if err != nil {
			return nil, err
		}
		resources = append(resources, matching...)
	}
	return resources, nil
}

func (c *resourceClient[T, U]) PatchOwnedResource(ctx context.Context, owner U, object client.Object, extraLabels ...map[string]string) error {
	if err := c.normalize(object, owner, extraLabels...); err != nil {
		return err
	}
	return c.client.Patch(ctx, object, client.Apply, c.manager.GetFieldOwner(), client.ForceOwnership)
}

func (n *resourceClient[T, U]) normalize(object client.Object, owner U, extraLabels ...map[string]string) error {
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

	for name, value := range n.manager.OwnerLabels(owner) {
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

func (r *resourceClient[T, U]) SyncAll(ctx context.Context, owner U) error {
	resources, err := r.listAllOwnedResources(ctx, owner)
	if err != nil {
		return err
	}
	toDelete := map[types.NamespacedName]client.Object{}
	for _, resource := range resources {
		toDelete[client.ObjectKeyFromObject(resource)] = resource
	}

	toSync, err := r.manager.OwnedResources(ctx, owner)
	if err != nil {
		return err
	}

	// attempt to create as many resources in one pass as we can
	errs := []error{}

	for _, resource := range toSync {
		if err := r.PatchOwnedResource(ctx, owner, resource); err != nil {
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

func (r *resourceClient[T, U]) DeleteAll(ctx context.Context, owner U) (bool, error) {
	resources, err := r.listAllOwnedResources(ctx, owner)
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

func (r *resourceClient[T, U]) FetchExistingPools(ctx context.Context, cluster U) ([]*Pool, error) {
	sets, err := r.ListOwnedResources(ctx, cluster, &appsv1.StatefulSet{})
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

		revisions, err := r.ListResources(ctx, &appsv1.ControllerRevision{}, client.MatchingLabelsSelector{
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

		pods, err := r.ListResources(ctx, &corev1.Pod{}, client.MatchingLabelsSelector{
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

func (r *resourceClient[T, U]) WatchResources(builder *builder.Builder, cluster U) error {
	for _, resourceType := range r.manager.OwnedResourceTypes(cluster) {
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
			if owned, owner := r.manager.GetOwner(o); owned {
				return []reconcile.Request{{
					NamespacedName: owner,
				}}
			}
			return nil
		}))

	}

	return nil
}
