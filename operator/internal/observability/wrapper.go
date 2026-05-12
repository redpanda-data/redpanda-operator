// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package observability

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// nowUnix is overridable in tests. The default reads wall-clock time.
var nowUnix = func() float64 { return float64(time.Now().Unix()) }

// Wrap returns a reconcile.Reconciler that delegates to inner while
// recording the result-driven Group B metrics (steady_state_total,
// requeue_after_seconds). One wrap call per controller; the controller's
// SetupWithManager passes the wrapped reconciler to ControllerManagedBy's
// Complete or Build.
//
// The generic over reconcile.TypedReconciler[R] lets the same wrapper work
// for both standard ctrl.Reconciler (R = reconcile.Request) and the
// multicluster Reconciler (R = mcreconcile.Request), without duplicating
// the body.
func Wrap[R comparable](inner reconcile.TypedReconciler[R], controller string) reconcile.TypedReconciler[R] {
	return &wrapped[R]{inner: inner, controller: controller}
}

type wrapped[R comparable] struct {
	inner      reconcile.TypedReconciler[R]
	controller string
}

func (w *wrapped[R]) Reconcile(ctx context.Context, req R) (reconcile.Result, error) {
	result, err := w.inner.Reconcile(ctx, req)
	w.record(result, err)
	return result, err
}

// record updates the wrapper's Group B counters / histograms from one
// reconcile outcome. Split out so the test stubs can call it directly.
//
// Steady-state semantics: a reconcile that returns (Result{}, nil) is
// counted. A reconcile that returns Result{Requeue: true} or
// Result{RequeueAfter: > 0} is *not* counted, regardless of error
// (controller is asking to be re-invoked). Errors are not counted —
// controller-runtime's own `reconcile_total{result="error"}` covers
// those.
func (w *wrapped[R]) record(result reconcile.Result, err error) {
	if err == nil && result.IsZero() {
		ReconcileSteadyStateTotal.WithLabelValues(w.controller).Inc()
		ReconcileLastSuccessTimestampSeconds.WithLabelValues(w.controller).Set(nowUnix())
	}
	if result.RequeueAfter > 0 {
		ReconcileRequeueAfterSeconds.WithLabelValues(w.controller).Observe(result.RequeueAfter.Seconds())
	}
}
