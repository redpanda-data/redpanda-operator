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

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func consoleIsHealthy(ctx context.Context, t framework.TestingT, name string) {
	key := t.ResourceKey(name)

	t.Logf("Checking console %q is healthy", name)
	require.Eventually(t, func() bool {
		var console redpandav1alpha2.Console
		require.NoError(t, t.Get(ctx, key, &console))

		upToDate := console.Generation == console.Status.ObservedGeneration
		hasHealthyReplicas := console.Status.ReadyReplicas == console.Status.Replicas

		return upToDate && hasHealthyReplicas
	}, time.Minute, 10*time.Second)
}
