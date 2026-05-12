// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package observability provides metric instrumentation for the operator's
// controllers. It augments the controller-runtime built-in metrics
// (controller_runtime_reconcile_*, workqueue_*) with operator-specific
// signals that flag reconcile-health problems:
//
//   - Whether a controller is reaching steady state vs spinning.
//   - The distribution of requested re-queue intervals.
//   - When each controller last reached steady state.
//
// Wrap(reconciler, controller, defaultRequeueTimeout) returns a wrapped
// reconcile.Reconciler that records the result-driven metrics around
// each Reconcile call. Used at every controller's SetupWithManager.
//
// Metric naming follows the existing `operator_<subsystem>_<name>`
// convention. All metric labels have closed vocabularies — no per-object
// labels are exposed at the prometheus level (those would explode
// cardinality on a controller that owns thousands of objects).
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "operator"
	metricsSubsystem = "controller"
)

var (
	// ReconcileSteadyStateTotal increments when a controller's Reconcile
	// returns "no work to do" — either (Result{}, nil) or a
	// (Result{RequeueAfter: defaultRequeueTimeout}, nil) matching the
	// controller's configured periodic-requeue interval. Healthy
	// controllers see this counter dominate over time once the system
	// is converged. A controller whose `reconcile_total` rate is high
	// but whose `steady_state_total` rate stays flat is spinning
	// without making progress.
	ReconcileSteadyStateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_steady_state_total",
		Help:      "Reconciles that returned no-work — either (Result{}, nil) or the configured periodic-requeue shape.",
	}, []string{"controller"})

	// ReconcileRequeueAfterSeconds observes the duration carried by a
	// non-zero Result.RequeueAfter return. A tight cluster of small values
	// (< 1s) is a strong signal a controller is in a tight retry loop —
	// e.g. waiting on an external resource that never converges. Healthy
	// controllers either return Result{} (steady) or RequeueAfter in the
	// seconds-to-minutes range.
	ReconcileRequeueAfterSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_requeue_after_seconds",
		Help:      "Distribution of Result.RequeueAfter durations across reconciles that requested a delayed re-queue.",
		// Cover sub-second loops (spinning detection) through ~1h
		// (long-poll style re-queues).
		Buckets: []float64{
			0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 1800, 3600,
		},
	}, []string{"controller"})

	// ReconcileLastSuccessTimestampSeconds is the Unix timestamp of the
	// most recent steady-state reconcile for a given controller (see
	// ReconcileSteadyStateTotal for the definition of steady state —
	// both (Result{}, nil) and the periodic-requeue shape qualify).
	// Prometheus computes "seconds since last success" at query time
	// as `time() - operator_controller_reconcile_last_success_timestamp_seconds`,
	// avoiding the goroutine / oldest-unfinished tracking that an
	// imperative "seconds elapsed" gauge would need.
	//
	// Updated by the wrapper inside the steady-state branch — no
	// controller-side wiring is required. Bounded cardinality (controller
	// label only). A flat value while reconcile_total is climbing means
	// the controller is failing or spinning.
	ReconcileLastSuccessTimestampSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_last_success_timestamp_seconds",
		Help:      "Unix timestamp of the most recent steady-state reconcile per controller. Use time() - this for seconds-since-last-success.",
	}, []string{"controller"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileSteadyStateTotal,
		ReconcileRequeueAfterSeconds,
		ReconcileLastSuccessTimestampSeconds,
	)
}
