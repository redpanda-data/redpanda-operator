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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2NodePoolRenderer represents a node pool renderer for v2 clusters.
type V2NodePoolRenderer struct {
	kubeConfig    *kube.RESTConfig
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer[ClusterWithPools, *ClusterWithPools] = (*V2NodePoolRenderer)(nil)

// NewV2NodePoolRenderer returns a V2NodePoolRenderer.
func NewV2NodePoolRenderer(mgr ctrl.Manager, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *V2NodePoolRenderer {
	return &V2NodePoolRenderer{
		kubeConfig:    mgr.GetConfig(),
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V2NodePoolRenderer) Render(ctx context.Context, cluster *ClusterWithPools) ([]*appsv1.StatefulSet, error) {
	spec := cluster.Spec.ClusterSpec.DeepCopy()
	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	if spec.Statefulset == nil {
		spec.Statefulset = &redpandav1alpha2.Statefulset{}
	}

	if spec.Statefulset.SideCars == nil {
		spec.Statefulset.SideCars = &redpandav1alpha2.SideCars{}
	}

	state, err := conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaults{
		RedpandaImage:    defaultImage(spec.Image, m.redpandaImage),
		SidecarImage:     defaultImage(spec.Statefulset.SideCars.Image, m.sideCarImage),
		ConfiguratorArgs: m.cloudSecrets.AdditionalConfiguratorArgs(),
	}, cluster.Redpanda, cluster.NodePools)
	if err != nil {
		return nil, err
	}

	return redpanda.RenderNodePools(state)
}

func isNodePool(object client.Object) bool {
	_, ok := object.(*appsv1.StatefulSet)
	return ok
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *V2NodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}

func defaultImage(base *redpandav1alpha2.RedpandaImage, default_ Image) *redpandav1alpha2.RedpandaImage {
	if base == nil {
		return &redpandav1alpha2.RedpandaImage{
			Repository: ptr.To(default_.Repository),
			Tag:        ptr.To(default_.Tag),
		}
	}

	return &redpandav1alpha2.RedpandaImage{
		Repository: ptr.To(ptr.Deref(base.Repository, default_.Repository)),
		Tag:        ptr.To(ptr.Deref(base.Tag, default_.Tag)),
	}
}
