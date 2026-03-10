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
	corev1 "k8s.io/api/core/v1"
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

	state, err := conversion.ConvertStretchClusterToRenderState(cl.GetConfig(), &conversion.V2Defaulters{}, cluster.StretchCluster, cluster.GetNodePoolsForCluster(clusterName), clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	state.SeedServers = []string{
		"cluster-external-0.default.svc.cluster.local",
		"cluster-external-1.default.svc.cluster.local",
		"cluster-external-2.default.svc.cluster.local",
	}

	resources, err := redpanda.RenderResources(state)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	services, oldServiceName := m.renderServiceForCluster(state)

	state.Values.Service.Name = ptr.To(oldServiceName)

	for _, service := range services {
		resources = append(resources, service)
	}

	return resources, nil
}

func (m *StretchClusterSimpleResourceRenderer) renderServiceForCluster(state *redpanda.RenderState) ([]*corev1.Service, string) {
	oldServiceName := ""
	if state.Values.Service != nil {
		if state.Values.Service.Name != nil {
			oldServiceName = *state.Values.Service.Name
		}
	} else {
		state.Values.Service = &redpanda.Service{}
	}
	var services []*corev1.Service
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

			services = append(services, svc)
		}
	}
	return services, oldServiceName
}

func (m *StretchClusterSimpleResourceRenderer) RenderPoolsServices(ctx context.Context, cluster *StretchClusterWithPools) ([]*corev1.Service, error) {
	var services []*corev1.Service
	var errs []error
	for _, clusterName := range m.mgr.GetClusterNames() {
		cl, err := m.mgr.GetCluster(ctx, clusterName)
		if err != nil {
			errs = append(errs, errors.WithStack(err))
			continue
		}

		state, err := conversion.ConvertStretchClusterToRenderState(cl.GetConfig(), &conversion.V2Defaulters{}, cluster.StretchCluster, cluster.GetNodePoolsForCluster(clusterName), clusterName)
		if err != nil {
			errs = append(errs, errors.WithStack(err))
			continue
		}
		servicesInCluster, _ := m.renderServiceForCluster(state)
		services = append(services, servicesInCluster...)
	}
	if len(errs) > 0 {
		// warn here
	}
	if len(services) == 0 {
		// error here
	}
	return services, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *StretchClusterSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return redpanda.Types()
}
