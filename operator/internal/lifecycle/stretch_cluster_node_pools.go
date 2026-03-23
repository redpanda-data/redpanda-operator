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
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// NodePoolRenderer represents a node pool multiclusterRenderer for stretch clusters.
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

// Render returns a list of StatefulSets for the given stretch cluster.
func (m *StretchNodePoolRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]*appsv1.StatefulSet, error) {
	cl, err := m.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Use the canonical cluster name so that labels are identical regardless
	// of which operator instance (local vs remote) performs the reconciliation.
	canonicalName := canonicalClusterName(clusterName, m.mgr)

	state, err := multiclusterRenderer.NewRenderState(cl.GetConfig(), cluster.StretchCluster, cluster.GetNodePoolsForCluster(canonicalName), []string{}, canonicalName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return multiclusterRenderer.RenderNodePools(state)
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *StretchNodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}

func canonicalClusterName(clusterName string, mgr multicluster.Manager) string {
	canonicalName := clusterName
	if canonicalName == mcmanager.LocalCluster {
		canonicalName = mgr.GetLocalClusterName()
	}
	return canonicalName
}
