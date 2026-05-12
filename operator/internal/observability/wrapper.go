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
// requeue_after_seconds, last_success_timestamp_seconds). One wrap call
// per controller; the controller's SetupWithManager passes the wrapped
// reconciler to ControllerManagedBy's Complete or Build.
//
// defaultRequeueTimeout is the controller's periodic-requeue interval
// (the value the controller returns in Result.RequeueAfter when it has
// no work to do but wants to wake up periodically — e.g. the
// MulticlusterReconciler's `defer { result.RequeueAfter = periodicRequeue }`
// pattern). Returns matching that exact duration are treated as steady
// state, and are filtered out of the requeue-after histogram so the
// histogram surfaces only the unusual requeue values (tight retry
// loops, finalizer requeues, etc.) instead of being dominated by the
// periodic-wake signal. Pass 0 for controllers that don't use the
// periodic-requeue pattern; under that setting only Result{} counts as
// steady state.
//
// The generic over reconcile.TypedReconciler[R] lets the same wrapper work
// for both standard ctrl.Reconciler (R = reconcile.Request) and the
// multicluster Reconciler (R = mcreconcile.Request), without duplicating
// the body.
func Wrap[R comparable](inner reconcile.TypedReconciler[R], controller string, defaultRequeueTimeout time.Duration) reconcile.TypedReconciler[R] {
	return &wrapped[R]{inner: inner, controller: controller, defaultRequeueTimeout: defaultRequeueTimeout}
}

type wrapped[R comparable] struct {
	inner                 reconcile.TypedReconciler[R]
	controller            string
	defaultRequeueTimeout time.Duration
}

func (w *wrapped[R]) Reconcile(ctx context.Context, req R) (reconcile.Result, error) {
	result, err := w.inner.Reconcile(ctx, req)
	w.record(result, err)
	return result, err
}

// record updates the wrapper's Group B counters / histograms from one
// reconcile outcome. Split out so the test stubs can call it directly.
//
// Steady-state semantics: a reconcile that returns (Result{}, nil) OR
// (Result{RequeueAfter: defaultRequeueTimeout}, nil) is counted as
// steady state — both shapes mean "controller has no work to do." The
// second shape supports the periodic-requeue pattern (e.g.
// MulticlusterReconciler always returns RequeueAfter = periodicRequeue
// via a defer), which would otherwise never enter the steady-state
// branch. A reconcile that returns Result{Requeue: true} or a
// RequeueAfter that doesn't match defaultRequeueTimeout is *not*
// counted (controller is asking to come back for a real reason).
// Errors are not counted — controller-runtime's own
// `reconcile_total{result="error"}` covers those.
//
// The requeue-after histogram observes only non-periodic RequeueAfter
// values so the periodic-wake signal doesn't dominate the histogram
// and bury the tight-retry-loop signal the histogram exists to
// surface.
func (w *wrapped[R]) record(result reconcile.Result, err error) {
	if err == nil && (result.IsZero() || w.isPeriodicRequeue(result)) {
		ReconcileSteadyStateTotal.WithLabelValues(w.controller).Inc()
		ReconcileLastSuccessTimestampSeconds.WithLabelValues(w.controller).Set(nowUnix())
	}
	if result.RequeueAfter > 0 && !w.isPeriodicRequeue(result) {
		ReconcileRequeueAfterSeconds.WithLabelValues(w.controller).Observe(result.RequeueAfter.Seconds())
	}
}

// isPeriodicRequeue reports whether result matches the controller's
// configured "I have nothing to do, wake me periodically" shape — a
// result whose RequeueAfter equals defaultRequeueTimeout exactly.
// Used both to count steady state and to filter the requeue histogram.
//
// defaultRequeueTimeout == 0 means the controller does not use the
// periodic-requeue pattern; this predicate always returns false in
// that case so a Result{Requeue: true} (which has RequeueAfter == 0)
// doesn't accidentally register as periodic-steady. Plain
// (Result{}, nil) is still counted as steady state via the
// result.IsZero() branch in record().
func (w *wrapped[R]) isPeriodicRequeue(result reconcile.Result) bool {
	if w.defaultRequeueTimeout == 0 {
		return false
	}
	return result.RequeueAfter == w.defaultRequeueTimeout
}
