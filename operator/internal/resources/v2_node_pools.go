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
	appsv1 "k8s.io/api/apps/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// V2NodePoolRenderer represents a node pool renderer for v2 clusters.
type V2NodePoolRenderer struct {
	kubeConfig clientcmdapi.Config
}

var _ NodePoolRenderer[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda] = (*V2NodePoolRenderer)(nil)

// NewV2NodePoolRenderer returns a V2NodePoolRenderer.
func NewV2NodePoolRenderer(mgr ctrl.Manager) *V2NodePoolRenderer {
	return &V2NodePoolRenderer{
		kubeConfig: kube.RestToConfig(mgr.GetConfig()),
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V2NodePoolRenderer) Render(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]*appsv1.StatefulSet, error) {
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

	resources := []*appsv1.StatefulSet{}

	// filter out non-nodepools
	for _, object := range rendered {
		if isNodePool(object) {
			resources = append(resources, object.(*appsv1.StatefulSet))
		}
	}

	return resources, nil
}

// isNodePool returns whether or not the object passed to it should be considered a node pool.
// This concrete implementation just looks for a label on a rendered object that says it's a
// node pool.
func isNodePool(object client.Object) bool {
	if labels := object.GetLabels(); labels != nil {
		if label, ok := labels["chart.redpanda.com/component"]; ok && label == "node-pool" {
			return true
		}
	}
	return false
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *V2NodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}
