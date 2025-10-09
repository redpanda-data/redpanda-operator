// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"maps"
	"reflect"
	"slices"

	"github.com/cockroachdb/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

type Renderer interface {
	Render(context.Context) ([]Object, error)
	Types() []Object
}

// Syncer synchronizes a set of [Object]s into the Kubernetes API. Objects will
// be upated via SSA and deleted when they are no longer returned from
// [Renderer].
type Syncer struct {
	Ctl      *Ctl
	Renderer Renderer

	// Namespace is the namespace [Syncer] will use for listing [Object]s. If
	// Renderer returns an Object in a namespace other than this one, it WILL
	// NOT be gabage collected.
	Namespace string

	// Owner is the [metav1.OwnerReference] that will be set on all **Namespace
	// scoped** Objects returned by Renderer.
	// It is additionally used to filter **Namespace scoped** objects.
	//
	// Owner CAN NOT be changed without abandoning objects.
	Owner metav1.OwnerReference

	// OwnershipLabels functions similar to Owner. They're applied to all
	// objects and used for filtering. In the case of cluster wide objects,
	// OwnershipLabels is the sole method of determining ownership.
	//
	// OwnershipLabels CAN NOT be changed without abandoning objects.
	OwnershipLabels map[string]string

	// Preprocess, if provided, is run ahead of applying Objects. It may be
	// used to add additional labels, annotation, etc uniformly.
	Preprocess func(Object)
}

func (s *Syncer) Sync(ctx context.Context) ([]Object, error) {
	logger := log.FromContext(ctx)

	toSync, err := s.toSync(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := s.listInPurview(ctx)
	if err != nil {
		return nil, err
	}

	// Diff toSync and existing to create a list of Objects that should be GC'd.
	toDelete := make(map[gvkObject]Object, len(existing))
	for _, resource := range existing {
		gvk, err := GVKFor(s.Ctl.Scheme(), resource)
		if err != nil {
			return nil, err
		}

		toDelete[gvkObject{
			gvk: gvk,
			key: AsKey(resource),
		}] = resource
	}

	for _, resource := range toSync {
		gvk, err := GVKFor(s.Ctl.Scheme(), resource)
		if err != nil {
			return nil, err
		}

		delete(toDelete, gvkObject{
			gvk: gvk,
			key: AsKey(resource),
		})
	}

	for _, obj := range toSync {
		if err := s.Ctl.Apply(ctx, obj, client.ForceOwnership); err != nil {
			// Similarly to our list function, ignore unregistered values and log a warning.
			if meta.IsNoMatchError(err) {
				gvk, err := GVKFor(s.Ctl.Scheme(), obj)
				if err != nil {
					return nil, err
				}

				// the WARNING messages here get logged constantly and are fairly static containing the resource type itself
				// so we can just use the global debouncer which debounces by error string
				log.DebounceError(logger, err, "WARNING no registered value for resource type", "gvk", gvk.String(), "key", AsKey(obj))
				continue
			}
			return nil, err
		}
	}

	for key, obj := range toDelete {
		log.Info(ctx, "GC'ing object", "key", key.key.String(), "gvk", key.gvk.String())
		if err := s.Ctl.Delete(ctx, obj); err != nil {
			return nil, err
		}
	}

	// Return the applied objects. They're mutated in place by ApplyAll
	// which will allow callers to extract information from their
	// statuses and the like.
	return toSync, nil
}

func (s *Syncer) DeleteAll(ctx context.Context) (bool, error) {
	toDelete, err := s.listInPurview(ctx)
	if err != nil {
		return true, err
	}

	alive := 0
	for _, obj := range toDelete {
		if obj.GetDeletionTimestamp() == nil {
			alive++
		}

		if err := s.Ctl.Delete(ctx, obj); err != nil {
			return true, err
		}
	}

	return alive > 0, nil
}

func (s *Syncer) listInPurview(ctx context.Context) ([]Object, error) {
	logger := log.FromContext(ctx)

	var objects []Object
	for _, t := range s.Renderer.Types() {
		gvk, err := GVKFor(s.Ctl.Scheme(), t)
		if err != nil {
			return nil, err
		}

		scope, err := s.Ctl.ScopeOf(gvk)
		if err != nil {
			// If we encounter an unknown type, e.g. someone hasn't installed
			// cert-manager, don't block the entire sync process. Instead we'll
			// log a warning and move on.
			if meta.IsNoMatchError(err) {
				// the WARNING messages here get logged constantly and are fairly static containing the resource type itself
				// so we can just use the global debouncer which debounces by error string
				log.DebounceError(logger, err, "WARNING no registered value for resource type", "gvk", gvk.String())
				continue
			}
			return nil, err
		}

		list, err := ListFor(s.Ctl.client.Scheme(), t)
		if err != nil {
			return nil, err
		}

		if err := s.Ctl.List(ctx, s.Namespace, list, client.MatchingLabels(s.OwnershipLabels)); err != nil {
			return nil, err
		}

		items, err := Items[Object](list)
		if err != nil {
			return nil, err
		}

		// If resources are Namespace scoped, we additionally filter on whether
		// or not OwnerRef is set correctly.
		if scope == meta.RESTScopeNameNamespace {
			i := 0
			for _, obj := range items {
				owned := slices.ContainsFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
					return ref.UID == s.Owner.UID
				})

				if owned {
					items[i] = obj
					i++
				}

			}

			items = items[:i]
		}

		objects = append(objects, items...)
	}

	return objects, nil
}

func (s *Syncer) toSync(ctx context.Context) ([]Object, error) {
	objs, err := s.Renderer.Render(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	types := s.Renderer.Types()
	expectedTypes := make(map[reflect.Type]struct{}, len(types))
	for _, t := range types {
		expectedTypes[reflect.TypeOf(t)] = struct{}{}
	}

	for _, obj := range objs {
		// Ensure that all types returned are present in s.Types. If they aren't
		// we'd potentially "leak" objects.
		if _, ok := expectedTypes[reflect.TypeOf(obj)]; !ok {
			return nil, errors.Newf(".Render returned %T which isn't present in .Types", obj)
		}

		// Run Preprocessors, if any.
		if s.Preprocess != nil {
			s.Preprocess(obj)
		}

		// Additionally apply Owners (if non-namespace scoped) and OwnershipLabels
		s.applyOwnerLabels(obj)
		if err := s.applyOwnerReferences(obj); err != nil {
			return nil, err
		}
	}

	return objs, nil
}

func (s *Syncer) applyOwnerLabels(obj Object) {
	if obj.GetLabels() == nil {
		obj.SetLabels(map[string]string{})
	}
	maps.Copy(obj.GetLabels(), s.OwnershipLabels)
}

func (s *Syncer) applyOwnerReferences(obj Object) error {
	gvk, err := GVKFor(s.Ctl.Scheme(), obj)
	if err != nil {
		return err
	}

	scope, err := s.Ctl.ScopeOf(gvk)
	if err != nil {
		// Ignore no match errors that stem from ScopeOf. We'll handle them
		// elsewhere. There's no risk of applying the object with a missing
		// ownerreference as the API server won't accept objects of this type.
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}

	// no owners on namespace scoped items.
	if scope == meta.RESTScopeNameRoot {
		// Cluster scoped objects should NOT have a namespace specified. If they
		// do, they'll be incorrectly deleted due to their ObjectKey containing a
		// Namespace and the Kubernetes version not containing a Namespace.
		// Validate there here to prevent confusing and unexpected behavior.
		if obj.GetNamespace() != "" {
			return errors.Newf("cluster scoped object incorrectly has namespace set: %T %s", obj, AsKey(obj))
		}
		return nil
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{s.Owner})

	return nil
}

type gvkObject struct {
	gvk schema.GroupVersionKind
	key ObjectKey
}
