// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// reconcileDeadline must fall back to defaultReconcileTimeout when the
// struct field is zero (production construction sites that don't plumb
// the CLI flag should still get a safe default) and must honour the
// field otherwise (tests and ops that want to override).
func TestReconcileDeadline(t *testing.T) {
	t.Run("zero field falls back to default", func(t *testing.T) {
		r := &MulticlusterReconciler{}
		require.Equal(t, defaultReconcileTimeout, r.reconcileDeadline())
	})

	t.Run("non-zero field wins", func(t *testing.T) {
		r := &MulticlusterReconciler{ReconcileTimeout: 42 * time.Second}
		require.Equal(t, 42*time.Second, r.reconcileDeadline())
	})
}

// Sanity bounds on defaultReconcileTimeout — both ends are regression
// guards. A value too small aborts healthy reconciles and defeats the
// point; a value too large lets a single slow dependency hold the
// reconcile worker hostage, which is the scenario this timeout exists
// to prevent.
func TestDefaultReconcileTimeoutBounds(t *testing.T) {
	require.GreaterOrEqual(t, defaultReconcileTimeout, 30*time.Second,
		"defaultReconcileTimeout must leave room for p99 healthy reconciles; %s is too aggressive",
		defaultReconcileTimeout)
	require.LessOrEqual(t, defaultReconcileTimeout, 10*time.Minute,
		"defaultReconcileTimeout must bound the worker reasonably; %s will starve other work",
		defaultReconcileTimeout)
	require.Greater(t, defaultReconcileTimeout, periodicRequeue/2,
		"defaultReconcileTimeout should exceed half the periodic requeue; otherwise back-to-back timeouts could dominate the budget")
}
