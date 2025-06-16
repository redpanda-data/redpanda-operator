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
	"reflect"

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectList is a generic equivalent of [ObjectList].
type ObjectList[T any] interface {
	client.ObjectList
	*T
}

// AddrofObject is a helper type constraint for accepting a struct value that
// implements the Object interface.
type AddrofObject[T any] interface {
	*T
	client.Object
}

// List is a generic equivalent of [Ctl.List].
func List[T any, L ObjectList[T]](ctx context.Context, ctl *Ctl, opts ...client.ListOption) (*T, error) {
	var list T
	if err := ctl.client.List(ctx, L(&list), opts...); err != nil {
		return nil, err
	}
	return &list, nil
}

// Get is a generic equivalent of [Ctl.Get].
func Get[T any, PT AddrofObject[T]](ctx context.Context, ctl *Ctl, key ObjectKey) (*T, error) {
	var obj T
	if err := ctl.client.Get(ctx, key, PT(&obj)); err != nil {
		return nil, err
	}
	return &obj, nil
}

// Get is a generic equivalent of [Ctl.Create].
func Create[T any, PT AddrofObject[T]](ctx context.Context, ctl *Ctl, obj T) (*T, error) {
	if err := ctl.Create(ctx, PT(&obj)); err != nil {
		return nil, err
	}
	return &obj, nil
}

// Get is a generic equivalent of [Ctl.Delete].
func Delete[T any, PT AddrofObject[T]](ctx context.Context, ctl *Ctl, key ObjectKey) error {
	obj := PT(new(T))
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	return ctl.client.Delete(ctx, obj)
}

func AsKey(obj Object) ObjectKey {
	return ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

// Items is a generic aware accessor for [client.ObjectList] that handles
// non-standard list implementations.
func Items[T Object](list client.ObjectList) ([]T, error) {
	items := reflect.ValueOf(list).Elem().FieldByName("Items")

	out := make([]T, items.Len())
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		if item.Kind() != reflect.Pointer {
			item = item.Addr()
		}

		converted, ok := item.Interface().(T)
		if !ok {
			to := reflect.TypeFor[T]()
			from := items.Index(i).Type()
			return nil, errors.Newf("can't convert %v to %v", from, to)
		}

		out[i] = converted
	}

	return out, nil
}
