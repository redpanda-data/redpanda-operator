// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

func WatchOptions(clusterName string) []mcbuilder.WatchesOption {
	if clusterName == mcmanager.LocalCluster {
		return []mcbuilder.WatchesOption{mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(false)}
	}

	return []mcbuilder.WatchesOption{mcbuilder.WithEngageWithLocalCluster(false), mcbuilder.WithEngageWithProviderClusters(true), mcbuilder.WithClusterFilter(func(name string, _ cluster.Cluster) bool {
		return name == clusterName
	})}
}
