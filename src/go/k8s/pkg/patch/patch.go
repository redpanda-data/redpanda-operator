// Package patch is a utility package that provides utils around patching a resource.
// It has its own package, because of a dependency conflict; pkg/utils may not
// import types/v1alpha1, types/v1alpha1 imports pkg/utils (cycle).
package patch

import (
	"context"
	"fmt"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/vectorized/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
