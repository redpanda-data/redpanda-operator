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
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// WatchOptions returns the multicluster watch options scoping a watch to the
// given member cluster (local vs. provider). Copied from the OSS operator's
// internal/controller.WatchOptions, which the enterprise module cannot import.
func WatchOptions(clusterName string) []mcbuilder.WatchesOption {
	if clusterName == mcmanager.LocalCluster {
		return []mcbuilder.WatchesOption{mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(false)}
	}

	return []mcbuilder.WatchesOption{mcbuilder.WithEngageWithLocalCluster(false), mcbuilder.WithEngageWithProviderClusters(true), mcbuilder.WithClusterFilter(func(name string, _ cluster.Cluster) bool {
		return name == clusterName
	})}
}
