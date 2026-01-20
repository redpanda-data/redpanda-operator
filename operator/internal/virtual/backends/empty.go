// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package backends

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

type EmptyBackend[T client.Object] struct{}

func NewEmptyBackend[T client.Object]() *EmptyBackend[T] {
	return &EmptyBackend[T]{}
}

func (b *EmptyBackend[T]) Create(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string, o T) (T, error) {
	var t T
	return t, ErrNotImplemented
}

func (b *EmptyBackend[T]) Read(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string) (T, error) {
	var t T
	return t, ErrNotImplemented
}

func (b *EmptyBackend[T]) Update(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string, o T) (T, error) {
	var t T
	return t, ErrNotImplemented
}

func (b *EmptyBackend[T]) Delete(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string) error {
	return ErrNotImplemented
}

func (b *EmptyBackend[T]) List(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]T, error) {
	var t []T
	return t, ErrNotImplemented
}
