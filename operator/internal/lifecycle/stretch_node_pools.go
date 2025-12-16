// Copyright 2025 Redpanda Data, Inc.
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

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// StretchNodePoolRenderer represents a node pool renderer for stretch clusters.
type StretchNodePoolRenderer struct {
	kubeConfig    *kube.RESTConfig
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchNodePoolRenderer)(nil)

// NewStretchNodePoolRenderer returns a StretchNodePoolRenderer.
func NewStretchNodePoolRenderer(mgr cluster.Cluster, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *StretchNodePoolRenderer {
	return &StretchNodePoolRenderer{
		kubeConfig:    mgr.GetConfig(),
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *StretchNodePoolRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]*appsv1.StatefulSet, error) {
	//state, err := m.convertToRender(cluster)
	//if err != nil {
	//	return nil, err
	//}
	//
	//return redpanda.RenderNodePools(state)
	return []*appsv1.StatefulSet{}, nil
}

//func (m *V2NodePoolRenderer) convertToRender(cluster *StretchClusterWithPools) (*redpanda.RenderState, error) {
//	return conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaulters{
//		RedpandaImage:    defaultImage(m.redpandaImage),
//		SidecarImage:     defaultImage(m.sideCarImage),
//		ConfiguratorArgs: m.cloudSecrets.AdditionalConfiguratorArgs(),
//	}, cluster.Redpanda, cluster.NodePools)
//}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *StretchNodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}
