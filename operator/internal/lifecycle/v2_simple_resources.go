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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2SimpleResourceRenderer represents an simple resource renderer for v2 clusters.
type V2SimpleResourceRenderer struct {
	kubeConfig *kube.RESTConfig
}

var _ SimpleResourceRenderer[ClusterWithPools, *ClusterWithPools] = (*V2SimpleResourceRenderer)(nil)

// NewV2SimpleResourceRenderer returns a V2SimpleResourceRenderer.
func NewV2SimpleResourceRenderer(mgr ctrl.Manager) *V2SimpleResourceRenderer {
	return &V2SimpleResourceRenderer{
		kubeConfig: mgr.GetConfig(),
	}
}

// Render returns a list of simple resources for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// should be considered a node pool.
func (m *V2SimpleResourceRenderer) Render(ctx context.Context, cluster *ClusterWithPools) ([]client.Object, error) {
	spec := cluster.Spec.ClusterSpec.DeepCopy()

	if spec != nil {
		// normalize the spec by removing the connectors stanza which is deprecated
		spec.Connectors = nil
	}

	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	state, err := conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaults{}, cluster.Redpanda, cluster.NodePools)
	if err != nil {
		return nil, err
	}

	rendered, err := redpanda.RenderResources(state)
	if err != nil {
		return nil, err
	}

	resources := []client.Object{}

	// filter out the hooks
	for _, object := range rendered {
		isHook := false
		annotations := object.GetAnnotations()
		if annotations != nil {
			_, isHook = annotations["helm.sh/hook"]
		}

		if !isHook {
			resources = append(resources, object)
		}
	}

	return resources, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *V2SimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return redpanda.Types()
}
