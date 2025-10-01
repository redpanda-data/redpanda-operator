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

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectList is a generic equivalent of [ObjectList].
type AddrOfObjectList[T any] interface {
	client.ObjectList
	*T
}

// AddrOfObject is a helper type constraint for accepting a struct value that
// implements the Object interface.
type AddrOfObject[T any] interface {
	*T
	client.Object
}

// AddrofObject is a copy of AddrofObject for backwards compatibility as
// generic type aliases are currently experimental.
type AddrofObject[T any] interface {
	*T
	client.Object
}

// ApplyAll is the generic equivalent of [Ctl.ApplyAll].
func ApplyAll[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, objs ...PT) error {
	return ctl.ApplyAll(ctx, AsObjects(objs...)...)
}

// ApplyAllAndWait is the generic equivalent of [Ctl.ApplyAllAndWait].
func ApplyAllAndWait[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, cond CondFn[PT], objs ...PT) error {
	return ctl.ApplyAllAndWait(ctx, func(obj Object, err error) (bool, error) {
		return cond(obj.(PT), err)
	}, AsObjects(objs...)...)
}

// Apply is the generic equivalent of [Ctl.Apply].
func Apply[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, obj PT) error {
	return ctl.Apply(ctx, obj)
}

// ApplyAndWait is the generic equivalent of [Ctl.ApplyAndWait]
func ApplyAndWait[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, obj PT, cond CondFn[PT]) error {
	return ctl.ApplyAndWait(ctx, obj, func(obj Object, err error) (bool, error) {
		return cond(obj.(PT), err)
	})
}

// List is a generic equivalent of [Ctl.List].
func List[T any, L AddrOfObjectList[T]](ctx context.Context, ctl *Ctl, namespace string, opts ...client.ListOption) (*T, error) {
	var list T
	if err := ctl.List(ctx, namespace, L(&list), opts...); err != nil {
		return nil, err
	}
	return &list, nil
}

// Get is a generic equivalent of [Ctl.Get].
//
//	ns, err := Get[corev1.Namespace](ctx, ctl, ObjectKey{Name: "my-namespace"})
//	pod, err := Get[corev1.Pod](ctx, ctl, ObjectKey{Namespace: "my-namespace", Name: "my-pod"})
func Get[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, key ObjectKey) (*T, error) {
	var obj T
	if err := ctl.client.Get(ctx, key, PT(&obj)); err != nil {
		return nil, err
	}
	return &obj, nil
}

// Get is a generic equivalent of [Ctl.Create].
func Create[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, obj T) (*T, error) {
	if err := ctl.Create(ctx, PT(&obj)); err != nil {
		return nil, err
	}
	return &obj, nil
}

// Get is a generic equivalent of [Ctl.Delete].
func Delete[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, key ObjectKey) error {
	obj := PT(new(T))
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	return ctl.client.Delete(ctx, obj)
}

// WaitFor is the generic equivalent of [Ctl.WaitFor].
func WaitFor[T any, PT AddrOfObject[T]](ctx context.Context, ctl *Ctl, obj PT, cond CondFn[PT]) error {
	return ctl.WaitFor(ctx, obj, func(obj Object, err error) (bool, error) {
		return cond(obj.(PT), err)
	})
}

func AsKey(obj Object) ObjectKey {
	return ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

// AsObjects converts a slice of a type that implements [Object] into a slice
// of [Object].
func AsObjects[T any, PT AddrOfObject[T]](in ...PT) []Object {
	out := make([]Object, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}
