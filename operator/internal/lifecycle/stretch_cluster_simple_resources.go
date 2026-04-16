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
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// StretchClusterSimpleResourceRenderer represents a simple resource multiclusterRenderer for stretch clusters.
type StretchClusterSimpleResourceRenderer struct {
	mgr           multicluster.Manager
	redpandaImage Image
	sideCarImage  Image
}

var _ SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterSimpleResourceRenderer)(nil)

// NewStretchClusterSimpleResourceRenderer returns a StretchClusterSimpleResourceRenderer.
func NewStretchClusterSimpleResourceRenderer(mgr multicluster.Manager, redpandaImage, sideCarImage Image) *StretchClusterSimpleResourceRenderer {
	return &StretchClusterSimpleResourceRenderer{
		mgr:           mgr,
		redpandaImage: redpandaImage,
		sideCarImage:  sideCarImage,
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
	canonicalName := CanonicalClusterName(clusterName, m.mgr.GetLocalClusterName)

	applyDefaultImage := defaultImage(m.redpandaImage)
	applyDefaultSidecar := defaultImage(m.sideCarImage)
	inCluster := cluster.GetNodePoolsForCluster(canonicalName)
	for _, pool := range inCluster {
		pool.Spec.Image = applyDefaultImage(pool.Spec.Image)
		pool.Spec.SidecarImage = applyDefaultSidecar(pool.Spec.SidecarImage)
	}
	allPools := cluster.GetAllNodePools()
	for _, pool := range allPools {
		pool.Spec.Image = applyDefaultImage(pool.Spec.Image)
		pool.Spec.SidecarImage = applyDefaultSidecar(pool.Spec.SidecarImage)
	}

	state, err := multiclusterRenderer.NewRenderState(
		cl.GetConfig(),
		cluster.StretchCluster,
		inCluster,
		allPools,
		canonicalName,
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Pass pod endpoints for flat network Endpoints/EndpointSlice rendering.
	if len(cluster.PodEndpoints) > 0 {
		renderEndpoints := make([]multiclusterRenderer.PodEndpoint, len(cluster.PodEndpoints))
		for i, ep := range cluster.PodEndpoints {
			renderEndpoints[i] = multiclusterRenderer.PodEndpoint{
				Name:    ep.Name,
				IP:      ep.IP,
				Cluster: ep.Cluster,
				Ready:   ep.Ready,
			}
		}
		state.WithPodEndpoints(renderEndpoints)
	}

	resources, err := multiclusterRenderer.RenderResources(state)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return resources, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *StretchClusterSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return multiclusterRenderer.Types()
}

func (m *StretchClusterSimpleResourceRenderer) GetAdminAPIEndpoints(cluster *StretchClusterWithPools) []string {
	var adminAPIEndpoints []string
	for _, pool := range cluster.NodePools {
		for i := int32(0); i < pool.nodePool.GetReplicas(); i++ {
			poolFullname := tplutil.CleanForK8s(cluster.Name) + pool.nodePool.Suffix()
			name := multiclusterRenderer.PerPodServiceName(poolFullname, i)
			adminAPIEndpoints = append(adminAPIEndpoints, fmt.Sprintf("%s.%s:%d", name, pool.nodePool.GetNamespace(), cluster.Spec.AdminPort()))
		}
	}
	return adminAPIEndpoints
}
