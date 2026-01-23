// Copyright 2026 Redpanda Data, Inc.
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

func consoleHasWarnings(ctx context.Context, t framework.TestingT, name string, expected int) {
	key := t.ResourceKey(name)

	t.Logf("Checking console %q has %d warning(s)", name, expected)
	require.Eventually(t, func() bool {
		var console redpandav1alpha2.Console
		if t.Get(ctx, key, &console) != nil {
			// we have an error fetching, maybe have not yet reconciled,
			// so just try again
			return false
		}

		return len(console.Spec.Warnings) == expected
	}, time.Minute, 10*time.Second)
}
