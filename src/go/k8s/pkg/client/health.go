package client

import (
	"context"
	"slices"
	"sync"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/client/external"
)

func bothNilOrEqual[T comparable, U *T](a, b U) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

type clusterHealthFetcher struct {
	cluster *redpandav1alpha2.Redpanda
	factory *Factory
	admin   *rpadmin.AdminAPI
	mutex   sync.Mutex
}

func (c *Factory) ClusterHealthFetcher(cluster *redpandav1alpha2.Redpanda) external.ResourceFetcher[rpadmin.ClusterHealthOverview] {
	return newClusterHealthFetcher(c, cluster)
}

func newClusterHealthFetcher(factory *Factory, cluster *redpandav1alpha2.Redpanda) *clusterHealthFetcher {
	return &clusterHealthFetcher{
		cluster: cluster,
		factory: factory,
	}
}

func (c *clusterHealthFetcher) Fetch(ctx context.Context) (rpadmin.ClusterHealthOverview, error) {
	admin, err := c.lazyAdmin(ctx)
	if err != nil {
		return rpadmin.ClusterHealthOverview{}, err
	}

	return admin.GetHealthOverview(ctx)
}

func (c *clusterHealthFetcher) lazyAdmin(ctx context.Context) (*rpadmin.AdminAPI, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.admin != nil {
		return c.admin, nil
	}

	admin, err := c.factory.RedpandaAdminClient(ctx, c.cluster)
	if err != nil {
		return nil, err
	}

	c.admin = admin

	return admin, nil
}

func (c *clusterHealthFetcher) Equal(a, b rpadmin.ClusterHealthOverview) bool {
	if a.IsHealthy != b.IsHealthy {
		return false
	}
	if !slices.Equal(a.UnhealthyReasons, b.UnhealthyReasons) {
		return false
	}
	if a.ControllerID != b.ControllerID {
		return false
	}
	if !slices.Equal(a.AllNodes, b.AllNodes) {
		return false
	}
	if !slices.Equal(a.NodesDown, b.NodesDown) {
		return false
	}
	if !slices.Equal(a.NodesInRecoveryMode, b.NodesInRecoveryMode) {
		return false
	}
	if !slices.Equal(a.LeaderlessPartitions, b.LeaderlessPartitions) {
		return false
	}
	if !bothNilOrEqual(a.LeaderlessCount, b.LeaderlessCount) {
		return false
	}
	if !slices.Equal(a.UnderReplicatedPartitions, b.UnderReplicatedPartitions) {
		return false
	}
	if !bothNilOrEqual(a.UnderReplicatedCount, b.UnderReplicatedCount) {
		return false
	}
	return true
}
