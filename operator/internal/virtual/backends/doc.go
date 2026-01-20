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
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

var ErrNotImplemented = errors.New("not implemented")

type Backend[T client.Object] interface {
	Create(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string, o T) (T, error)
	Read(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string) (T, error)
	Update(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string, o T) (T, error)
	Delete(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string) error
	List(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]T, error)
}
