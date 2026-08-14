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
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// MigrationAggregator combines per-pool migration reports into a single
// cluster-scoped condition value. Migration runs per pool, but the condition
// is cluster-scoped: letting every pool write it directly means the last
// pool to reconcile wins, so a finished pool declares Complete while another
// is still Blocked and the condition flaps on every pass. Instead each
// pool's reporter accumulates here and the owning reconciler flushes ONE
// aggregate after all pools have reconciled.
//
// An aggregator is built per reconcile pass and is not goroutine-safe.
type MigrationAggregator struct {
	expected int
	reports  map[string]poolReport
}

type poolReport struct {
	status  corev1.ConditionStatus
	reason  string
	message string
}

func NewMigrationAggregator() *MigrationAggregator {
	return &MigrationAggregator{reports: map[string]poolReport{}}
}

// PoolReporter returns the MigrationReporter for one pool. Report calls
// accumulate (last report per pool wins); NeedsCompletion delegates to
// inner, which keeps the underlying condition as the source of truth for
// "was a migration ever recorded".
func (a *MigrationAggregator) PoolReporter(pool string, inner MigrationReporter) MigrationReporter {
	a.expected++
	return &poolReporter{agg: a, pool: pool, inner: inner}
}

// Aggregate returns the combined condition value, most severe state first:
// any pool Blocked wins over InProgress, and Complete requires every pool.
// ok is false when nothing should be written: no pool reported (quiescent —
// nothing to record), or only some pools reported (partial information, e.g.
// a pool errored before reaching its report; writing would flap the
// condition on stale data).
func (a *MigrationAggregator) Aggregate() (status corev1.ConditionStatus, reason, message string, ok bool) {
	if len(a.reports) == 0 || len(a.reports) < a.expected {
		return "", "", "", false
	}

	byReason := map[string][]string{}
	for pool := range a.reports {
		byReason[a.reports[pool].reason] = append(byReason[a.reports[pool].reason], pool)
	}

	for _, reason := range []string{MigrationReasonBlocked, MigrationReasonInProgress} {
		pools, found := byReason[reason]
		if !found {
			continue
		}
		slices.Sort(pools)
		messages := make([]string, 0, len(pools))
		for _, pool := range pools {
			messages = append(messages, fmt.Sprintf("nodepool %s: %s", pool, a.reports[pool].message))
		}
		return corev1.ConditionFalse, reason, strings.Join(messages, "; "), true
	}

	// Every pool reported and none is blocked or in progress: complete.
	// Reuse any pool's message — they all report the canonical one.
	for pool := range a.reports {
		return corev1.ConditionTrue, MigrationReasonComplete, a.reports[pool].message, true
	}
	return "", "", "", false
}

type poolReporter struct {
	agg   *MigrationAggregator
	pool  string
	inner MigrationReporter
}

var _ MigrationReporter = (*poolReporter)(nil)

func (r *poolReporter) Report(_ context.Context, status corev1.ConditionStatus, reason, message string) {
	r.agg.reports[r.pool] = poolReport{status: status, reason: reason, message: message}
}

func (r *poolReporter) NeedsCompletion(ctx context.Context) bool {
	return r.inner.NeedsCompletion(ctx)
}

func (r *poolReporter) NeedsRollback(ctx context.Context) bool {
	return r.inner.NeedsRollback(ctx)
}
