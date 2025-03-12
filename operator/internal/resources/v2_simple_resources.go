// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// V2SimpleResourceRenderer represents an simple resource renderer for v2 clusters.
type V2SimpleResourceRenderer struct {
	kubeConfig clientcmdapi.Config
}

var _ SimpleResourceRenderer[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda] = (*V2SimpleResourceRenderer)(nil)

// NewV2SimpleResourceRenderer returns a V2SimpleResourceRenderer.
func NewV2SimpleResourceRenderer(mgr ctrl.Manager) *V2SimpleResourceRenderer {
	return &V2SimpleResourceRenderer{
		kubeConfig: kube.RestToConfig(mgr.GetConfig()),
	}
}

// Render returns a list of simple resources for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// should be considered a node pool.
func (m *V2SimpleResourceRenderer) Render(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]client.Object, error) {
	values := cluster.Spec.ClusterSpec.DeepCopy()

	rendered, err := redpanda.Chart.Render(&m.kubeConfig, helmette.Release{
		Namespace: cluster.Namespace,
		Name:      cluster.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, values)
	if err != nil {
		return nil, err
	}

	resources := []client.Object{}

	// filter out the statefulsets
	for _, object := range rendered {
		if !isNodePool(object) {
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
