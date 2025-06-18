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
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// V1ResourceManagers is a factory function for tying together all of our v1 interfaces.
func V1ResourceManagers(cloudSecrets CloudSecretsFlags) func(mgr ctrl.Manager) (
	OwnershipResolver[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
	ClusterStatusUpdater[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
	NodePoolRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
	SimpleResourceRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
) {
	return func(mgr ctrl.Manager) (
		OwnershipResolver[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
		ClusterStatusUpdater[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
		NodePoolRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
		SimpleResourceRenderer[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster],
	) {
		return NewV1OwnershipResolver(), NewV1ClusterStatusUpdater(), NewV1NodePoolRenderer(mgr), NewV1SimpleResourceRenderer(mgr, cloudSecrets)
	}
}
