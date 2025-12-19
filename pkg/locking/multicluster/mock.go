package multicluster

import (
	"context"
	"sort"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"
)

type mockManager struct {
	mcmanager.Manager
	clusterProvider *clusters.Provider
}

func (m *mockManager) AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error {
	return m.clusterProvider.AddOrReplace(ctx, clusterName, cl, nil)
}

func (m *mockManager) GetClusterNames() []string {
	clusters := m.GetClusterNames()
	sort.Strings(clusters)
	return clusters
}

func (m *mockManager) GetLeader() string {
	return ""
}

func NewMockManager(config *rest.Config, cl client.Client) (Manager, error) {
	mgr, err := ctrl.NewManager(config, manager.Options{
		NewClient: func(_ *rest.Config, _ client.Options) (client.Client, error) {
			return cl, nil
		},
	})
	if err != nil {
		return nil, err
	}
	clusterProvider := clusters.New()

	mc, err := mcmanager.WithMultiCluster(mgr, clusterProvider)
	if err != nil {
		return nil, err
	}

	return &mockManager{
		Manager:         mc,
		clusterProvider: clusterProvider,
	}, nil
}
