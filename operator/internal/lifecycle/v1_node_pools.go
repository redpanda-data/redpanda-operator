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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V1NodePoolRenderer represents a node pool renderer for v2 clusters.
type V1NodePoolRenderer struct {
	kubeConfig *kube.RESTConfig
}

var _ NodePoolRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster] = (*V1NodePoolRenderer)(nil)

// NewV1NodePoolRenderer returns a V1NodePoolRenderer.
func NewV1NodePoolRenderer(mgr ctrl.Manager) *V1NodePoolRenderer {
	return &V1NodePoolRenderer{
		kubeConfig: mgr.GetConfig(),
	}
}

// Render returns a list of StatefulSets for the given Redpanda v1 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V1NodePoolRenderer) Render(ctx context.Context, cluster *vectorizedv1alpha1.Cluster) ([]*appsv1.StatefulSet, error) {
	return nil, nil
}

// IsNodePool returns whether the object passed to it is a node pool.
func (m *V1NodePoolRenderer) IsNodePool(object client.Object) bool {
	return false
}
