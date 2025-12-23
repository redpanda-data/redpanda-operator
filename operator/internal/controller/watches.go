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
