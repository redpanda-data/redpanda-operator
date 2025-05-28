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
	ctrl "sigs.k8s.io/controller-runtime"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// V2ResourceManagers is a factory function for tying together all of our v2 interfaces.
func V2ResourceManagers(image Image, cloudSecrets CloudSecretsFlags) func(mgr ctrl.Manager) (
	OwnershipResolver[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
	ClusterStatusUpdater[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
	NodePoolRenderer[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
	SimpleResourceRenderer[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
) {
	return func(mgr ctrl.Manager) (
		OwnershipResolver[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
		ClusterStatusUpdater[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
		NodePoolRenderer[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
		SimpleResourceRenderer[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools],
	) {
		return NewV2OwnershipResolver(), NewV2ClusterStatusUpdater(), NewV2NodePoolRenderer(mgr, image, cloudSecrets), NewV2SimpleResourceRenderer(mgr)
	}
}
