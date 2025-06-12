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

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectList is a generic equivalent of [ObjectList].
type AddrOfObjectList[T any] interface {
	client.ObjectList
	*T
}

// AddrofObject is a helper type constraint for accepting a struct value that
// implements the Object interface.
type AddrofObject[T any] interface {
	*T
	client.Object
}

func ApplyAll[T client.Object](ctx context.Context, ctl *Ctl, objs ...T) error {
	var errs []error
	for _, obj := range objs {
		if err := ctl.Apply(ctx, obj); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func ApplyAllAndWait[T client.Object](ctx context.Context, ctl *Ctl, cond CondFn[T], objs ...T) error {
	if err := ApplyAll(ctx, ctl, objs...); err != nil {
		return err
	}

	for _, obj := range objs {
		if err := ctl.WaitFor(ctx, obj, func(o Object, err error) (bool, error) {
			return cond(T(obj), err)
		}); err != nil {
			return err
		}
	}

	return nil
}

func Apply[T client.Object](ctx context.Context, ctl *Ctl, obj T) error {
	return ctl.Apply(ctx, obj)
}

func ApplyAndWait[T client.Object](ctx context.Context, ctl *Ctl, obj T, cond CondFn[T]) error {
	if err := ctl.Apply(ctx, obj); err != nil {
		return err
	}

	return ctl.WaitFor(ctx, obj, func(o Object, err error) (bool, error) {
		return cond(T(obj), err)
	})
}

// List is a generic equivalent of [Ctl.List].
func List[T any, L AddrOfObjectList[T]](ctx context.Context, ctl *Ctl, opts ...client.ListOption) (*T, error) {
	var list T
	if err := ctl.List(ctx, L(&list), opts...); err != nil {
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
