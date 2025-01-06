// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// RedpandaFetcher fetches a Redpanda cluster via CR.
type RedpandaFetcher struct {
	client client.Client
}

var _ Fetcher = (*RedpandaFetcher)(nil)

func NewRedpandaFetcher(mgr ctrl.Manager) *RedpandaFetcher {
	return &RedpandaFetcher{client: mgr.GetClient()}
}

func (f *RedpandaFetcher) FetchLatest(ctx context.Context, name, namespace string) (any, error) {
	var cluster redpandav1alpha2.Redpanda
	if err := f.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cluster); err != nil {
		return nil, fmt.Errorf("fetching cluster: %w", err)
	}

	return cluster, nil
}
