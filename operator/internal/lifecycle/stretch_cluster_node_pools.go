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
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// NodePoolRenderer represents a node pool renderer for v2 clusters.
type StretchNodePoolRenderer struct {
	mgr           multicluster.Manager
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchNodePoolRenderer)(nil)

// NewStretchNodePoolRenderer returns a StretchNodePoolRenderer.
func NewStretchNodePoolRenderer(mgr multicluster.Manager, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *StretchNodePoolRenderer {
	return &StretchNodePoolRenderer{
		mgr:           mgr,
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *StretchNodePoolRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]*appsv1.StatefulSet, error) {
	state, err := m.convertToRender(ctx, cluster, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	sets := []*appsv1.StatefulSet{}
	for _, set := range state.Pools {
		sets = append(sets, redpanda.StatefulSet(state, set))
	}

	return sets, nil
}

func (m *StretchNodePoolRenderer) convertToRender(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) (*redpanda.RenderState, error) {
	cl, err := m.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return conversion.ConvertStretchClusterToRenderState(cl.GetConfig(), &conversion.V2Defaulters{}, cluster.StretchCluster, cluster.NodePools, clusterName)
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *StretchNodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}
