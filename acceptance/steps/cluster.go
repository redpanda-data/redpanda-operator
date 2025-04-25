// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func checkClusterAvailability(ctx context.Context, t framework.TestingT, clusterName string) {
	var cluster redpandav1alpha2.Redpanda

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q is ready", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasCondition := t.HasCondition(metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		}, cluster.Status.Conditions)

		t.Logf(`Checking cluster resource conditions contains "Ready"? %v`, hasCondition)
		return hasCondition
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained the condition reason "Ready", final Conditions: %+v`, key.String(), cluster.Status.Conditions)
	}))
	t.Logf("Cluster %q is ready!", clusterName)
}

func redpandaClusterIsHealthy(ctx context.Context, t framework.TestingT, cluster string) {
	clients := clientsForCluster(ctx, cluster)
	var health rpadmin.ClusterHealthOverview
	var err error

	c := clients.RedpandaAdmin(ctx)

	require.Eventually(t, func() bool {
		health, err = c.GetHealthOverview(ctx)
		require.NoError(t, err)

		t.Logf("Cluster health: %v", health.IsHealthy)
		return health.IsHealthy
	}, 5*time.Minute, 5*time.Second, `Cluster %q never become healthy: %+v`, cluster, health)
}
