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
	"fmt"

	"github.com/cockroachdb/errors"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Use the canonical cluster name so that labels are identical regardless
	// of which operator instance (local vs remote) performs the reconciliation.
	canonicalName := canonicalClusterName(clusterName, m.mgr)

	state, err := multiclusterRenderer.NewRenderState(
		cl.GetConfig(),
		cluster.StretchCluster,
		cluster.GetNodePoolsForCluster(canonicalName),
		SeedServersFromNodePools(cluster.StretchCluster, cluster.NodePools),
		canonicalName,
	)
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

func (m *StretchClusterSimpleResourceRenderer) RenderPoolsServices(ctx context.Context, cluster *StretchClusterWithPools) ([]*corev1.Service, error) {
	// TODO: remove
	return nil, nil
}

func SeedServersFromNodePools(cluster *redpandav1alpha2.StretchCluster, pools []*NodePoolInCluster) []string {
	var seedServers []string
	for _, pool := range pools {
		for i := int32(0); i < pool.nodePool.GetReplicas(); i++ {
			name := multiclusterRenderer.PerPodServiceName(pool.nodePool, i)
			seedServers = append(seedServers, fmt.Sprintf("%s.%s:%d", name, pool.nodePool.GetNamespace(), cluster.Spec.RPCPort()))
		}
	}
	return seedServers
}
