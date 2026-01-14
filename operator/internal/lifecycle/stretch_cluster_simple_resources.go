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
	"strconv"

	"github.com/cockroachdb/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// StretchClusterSimpleResourceRenderer represents an simple resource renderer for v2 clusters.
type StretchClusterSimpleResourceRenderer struct {
	mgr multicluster.Manager
}

var _ SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterSimpleResourceRenderer)(nil)

// NewStretchClusterSimpleResourceRenderer returns a StretchClusterSimpleResourceRenderer.
func NewStretchClusterSimpleResourceRenderer(mgr multicluster.Manager) *StretchClusterSimpleResourceRenderer {
	// Get all clusters and store them in the struct

	return &StretchClusterSimpleResourceRenderer{
		mgr: mgr,
	}
}

// Render returns a list of simple resources for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// should be considered a node pool.
func (m *StretchClusterSimpleResourceRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]client.Object, error) {
	cl, err := m.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	state, err := conversion.ConvertStretchClusterToRenderState(cl.GetConfig(), &conversion.V2Defaulters{}, cluster.StretchCluster, cluster.NodePools, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	resources, err := redpanda.RenderResources(state)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	oldServiceName := ""
	if state.Values.Service != nil {
		if state.Values.Service.Name != nil {
			oldServiceName = *state.Values.Service.Name
		}
	} else {
		state.Values.Service = &redpanda.Service{}
	}
	for _, pool := range state.Pools {
		for i := 0; i < int(pool.Statefulset.Replicas); i++ {
			if oldServiceName != "" {
				state.Values.Service.Name = ptr.To(oldServiceName + "-" + pool.Name + "-" + strconv.Itoa(i))
			} else {
				state.Values.Service.Name = ptr.To(pool.Name + "-" + strconv.Itoa(i))
			}

			svc := redpanda.ServiceInternal(state)
			svc.Spec.ClusterIP = ""
			svc.Annotations = pool.ServiceAnnotations
			svc.Spec.Selector = redpanda.StatefulSetPodLabelsSelector(state, pool)

			resources = append(resources, svc)
		}
	}

	state.Values.Service.Name = ptr.To(oldServiceName)

	return resources, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *StretchClusterSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return redpanda.Types()
}
