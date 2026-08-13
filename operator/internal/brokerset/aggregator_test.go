// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type staticNeedsCompletion bool

func (s staticNeedsCompletion) Report(context.Context, corev1.ConditionStatus, string, string) {}
func (s staticNeedsCompletion) NeedsCompletion(context.Context) bool                           { return bool(s) }

// TestMigrationAggregator pins the flap fix: a pool that finished migrating
// must never flip the cluster-scoped condition to Complete while another
// pool is still blocked or in progress, and partial reports (a pool errored
// before reporting) must not produce a write at all.
func TestMigrationAggregator(t *testing.T) {
	ctx := context.Background()

	report := func(agg *MigrationAggregator, pool string, reason, message string) {
		status := corev1.ConditionFalse
		if reason == MigrationReasonComplete {
			status = corev1.ConditionTrue
		}
		agg.PoolReporter(pool, staticNeedsCompletion(true)).Report(ctx, status, reason, message)
	}

	t.Run("one complete one blocked stays blocked", func(t *testing.T) {
		agg := NewMigrationAggregator()
		report(agg, "blue-b", MigrationReasonComplete, "StatefulSet removed; Broker CRs manage all pods")
		report(agg, "blue-a", MigrationReasonBlocked, "StatefulSet rollout has not completed")

		status, reason, message, ok := agg.Aggregate()
		require.True(t, ok)
		require.Equal(t, corev1.ConditionFalse, status)
		require.Equal(t, MigrationReasonBlocked, reason)
		require.Equal(t, "nodepool blue-a: StatefulSet rollout has not completed", message)
	})

	t.Run("blocked wins over in progress, pools sorted", func(t *testing.T) {
		agg := NewMigrationAggregator()
		report(agg, "blue-b", MigrationReasonBlocked, "cluster is restarting")
		report(agg, "blue-a", MigrationReasonBlocked, "decommission in progress")
		report(agg, "blue-c", MigrationReasonInProgress, "creating shadow Broker CRs")

		status, reason, message, ok := agg.Aggregate()
		require.True(t, ok)
		require.Equal(t, corev1.ConditionFalse, status)
		require.Equal(t, MigrationReasonBlocked, reason)
		require.Equal(t, "nodepool blue-a: decommission in progress; nodepool blue-b: cluster is restarting", message)
	})

	t.Run("all complete aggregates to complete", func(t *testing.T) {
		agg := NewMigrationAggregator()
		report(agg, "blue-a", MigrationReasonComplete, "StatefulSet removed; Broker CRs manage all pods")
		report(agg, "blue-b", MigrationReasonComplete, "StatefulSet removed; Broker CRs manage all pods")

		status, reason, message, ok := agg.Aggregate()
		require.True(t, ok)
		require.Equal(t, corev1.ConditionTrue, status)
		require.Equal(t, MigrationReasonComplete, reason)
		require.Equal(t, "StatefulSet removed; Broker CRs manage all pods", message)
	})

	t.Run("no reports means no write", func(t *testing.T) {
		agg := NewMigrationAggregator()
		agg.PoolReporter("blue-a", staticNeedsCompletion(false))
		agg.PoolReporter("blue-b", staticNeedsCompletion(false))

		_, _, _, ok := agg.Aggregate()
		require.False(t, ok, "quiescent pools must not dirty the condition")
	})

	t.Run("partial reports mean no write", func(t *testing.T) {
		agg := NewMigrationAggregator()
		report(agg, "blue-a", MigrationReasonInProgress, "creating shadow Broker CRs")
		agg.PoolReporter("blue-b", staticNeedsCompletion(true)) // constructed, never reported

		_, _, _, ok := agg.Aggregate()
		require.False(t, ok, "a pool that errored before reporting must not cause a flap on stale data")
	})

	t.Run("last report per pool wins", func(t *testing.T) {
		agg := NewMigrationAggregator()
		rep := agg.PoolReporter("blue-a", staticNeedsCompletion(true))
		rep.Report(ctx, corev1.ConditionFalse, MigrationReasonInProgress, "creating shadow Broker CRs")
		rep.Report(ctx, corev1.ConditionFalse, MigrationReasonBlocked, "cluster is restarting")

		_, reason, _, ok := agg.Aggregate()
		require.True(t, ok)
		require.Equal(t, MigrationReasonBlocked, reason)
	})
}
