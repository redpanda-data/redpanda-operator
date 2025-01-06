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
	"time"

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
			Reason: "RedpandaClusterDeployed",
		}, cluster.Status.Conditions)

		t.Logf(`Checking cluster resource conditions contains "RedpandaClusterDeployed"? %v`, hasCondition)
		return hasCondition
	}, 5*time.Minute, 5*time.Second, `Cluster %q never contained the condition reason "RedpandaClusterDeployed", final Conditions: %+v`, key.String(), cluster.Status.Conditions)
	t.Logf("Cluster %q is ready!", clusterName)
}
