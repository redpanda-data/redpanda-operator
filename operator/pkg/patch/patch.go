// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package patch is a utility package that provides utils around patching a resource.
// It has its own package, because of a dependency conflict; pkg/utils may not
// import types/v1alpha1, types/v1alpha1 imports pkg/utils (cycle).
package patch

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// PatchStatus persforms a mutation as done by mutator, calls k8s-api with PATCH, and then returns the
// new status.
func PatchStatus(ctx context.Context, c client.Client, observedCluster *vectorizedv1alpha1.Cluster, mutator func(cluster *vectorizedv1alpha1.Cluster)) (vectorizedv1alpha1.ClusterStatus, error) {
	clusterPatch := client.MergeFrom(observedCluster.DeepCopy())
	mutator(observedCluster)

	if err := c.Status().Patch(ctx, observedCluster, clusterPatch); err != nil {
		return vectorizedv1alpha1.ClusterStatus{}, fmt.Errorf("failed to update cluster status: %w", err)
	}

	return observedCluster.Status, nil
}
