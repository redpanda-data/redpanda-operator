// Copyright 2026 Redpanda Data, Inc.
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

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// StretchClusterSimpleResourceRenderer represents a simple resource multiclusterRenderer for stretch clusters.
type StretchClusterSimpleResourceRenderer struct {
	mgr multicluster.Manager
}

var _ SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterSimpleResourceRenderer)(nil)

// NewStretchClusterSimpleResourceRenderer returns a StretchClusterSimpleResourceRenderer.
func NewStretchClusterSimpleResourceRenderer(mgr multicluster.Manager) *StretchClusterSimpleResourceRenderer {
	return &StretchClusterSimpleResourceRenderer{
		mgr: mgr,
	}
}

// Render returns a list of simple resources for the given stretch cluster.
func (m *StretchClusterSimpleResourceRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]client.Object, error) {
	cl, err := m.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	state, err := multiclusterRenderer.NewRenderState(cl.GetConfig(), cluster.StretchCluster, cluster.NodePools, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	resources, err := multiclusterRenderer.RenderResources(state)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// TODO: re-implement per-pool service generation using new RenderState types

	return resources, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *StretchClusterSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return multiclusterRenderer.Types()
}
