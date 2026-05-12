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
//   - Generation drift between a resource's spec and its observed status.
//   - The "spec hash changed without generation bump" non-determinism
//     signal — a controller wrote to its own resource without the API
//     server seeing a meaningful spec change, almost always a bug.
//
// The package is consumed in two ways:
//
//   - Wrap(reconciler, controller) returns a wrapped reconcile.Reconciler
//     that records the result-driven metrics (steady state, requeue-after)
//     around each Reconcile call. Used at every controller's SetupWithManager.
//
//   - The package-level RecordObservedGeneration helper is called by
//     individual controllers from inside their own Reconcile after they
//     fetch the object, so generation-vs-observedGeneration drift can be
//     computed without a redundant Get from the wrapper.
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

	// ReconcileObservedGenerationDrift is the delta between an object's
	// `metadata.generation` and its `status.observedGeneration` at the
	// end of a reconcile, per (controller, kind). A non-zero value means
	// the controller saw a newer spec than its last successful
	// reconciliation produced status for — sustained non-zero drift
	// across reconciles is a stuck controller.
	//
	// Recorded by controllers via RecordObservedGeneration after they
	// fetch the object. Not recorded by the wrapper (which has no typed
	// object factory).
	ReconcileObservedGenerationDrift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_observed_generation_drift",
		Help:      "metadata.generation minus status.observedGeneration at the end of a reconcile, per controller and resource kind.",
	}, []string{"controller", "kind"})

	// SpecHashChangedWithoutGenerationTotal counts the case where a
	// controller wrote to the spec of its own resource without the API
	// server bumping `metadata.generation` — almost always a sign of
	// non-determinism in the reconciler's spec-rendering code (timestamp
	// included, map iteration order leaked, etc.). Healthy controllers
	// don't increment this counter.
	//
	// Recorded by controllers (or by a status-write helper) when a
	// no-op spec update is suppressed by the API server's
	// equality check.
	SpecHashChangedWithoutGenerationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_spec_hash_changed_without_generation_total",
		Help:      "Reconciles where the rendered spec hash differed from the previous run but metadata.generation did not advance.",
	}, []string{"controller", "kind"})

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

	// ReconcileSelfTriggeredTotal counts reconciles whose only observable
	// effect was a write to the same object that re-enqueued the
	// reconcile — a self-triggering loop. Distinct from
	// SpecHashChangedWithoutGenerationTotal (which is about spec
	// non-determinism with an unchanged generation); self-triggering can
	// happen on either spec or status writes.
	//
	// Recorded by controllers (passive recorder pattern). The wrapper
	// deliberately does not increment this — detection requires knowing
	// "the write came from us and re-enqueued us", which only the
	// controller's own write helpers can determine without redundant
	// Gets.
	ReconcileSelfTriggeredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_self_triggered_total",
		Help:      "Reconciles whose only effect was a write to the same object that re-enqueued the reconcile.",
	}, []string{"controller", "kind"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileSteadyStateTotal,
		ReconcileRequeueAfterSeconds,
		ReconcileObservedGenerationDrift,
		SpecHashChangedWithoutGenerationTotal,
		ReconcileLastSuccessTimestampSeconds,
		ReconcileSelfTriggeredTotal,
	)
}

// RecordObservedGeneration records the (generation - observedGeneration)
// drift for one object at the end of a reconcile. Callers pass the values
// they already read from the object — the package doesn't fetch anything
// itself, so callers control the cost.
//
// Callers should invoke this once per reconcile, regardless of whether the
// reconcile succeeded. The gauge always reflects "what was true at the end
// of the most recent reconcile."
//
// Negative drifts (observedGeneration > generation, which can happen
// briefly during a generation bump that races with the controller) are
// clamped to 0 so the gauge stays interpretable as "how far behind is
// the controller."
func RecordObservedGeneration(controller, kind string, generation, observedGeneration int64) {
	drift := generation - observedGeneration
	if drift < 0 {
		drift = 0
	}
	ReconcileObservedGenerationDrift.WithLabelValues(controller, kind).Set(float64(drift))
}

// RecordSpecHashChangedWithoutGeneration increments the non-determinism
// counter. Call from spec-update helpers when an API server reports the
// update was a no-op (generation unchanged) but the rendered spec hash
// differed from the previous run's hash for the same object. This pattern
// detects timestamps, map iteration order, and similar instability in
// rendered output.
func RecordSpecHashChangedWithoutGeneration(controller, kind string) {
	SpecHashChangedWithoutGenerationTotal.WithLabelValues(controller, kind).Inc()
}

// RecordSelfTriggered increments the self-triggered-reconcile counter.
// Call from a controller's write helpers when the controller has detected
// that its own write to the object will re-enqueue the same reconcile
// without any other observable effect — the canonical infinite-reconcile
// shape. Detection requires comparing pre- and post-write state, which
// the controller already does inside its own write path; the wrapper
// does not increment this counter because that would require a redundant
// Get.
func RecordSelfTriggered(controller, kind string) {
	ReconcileSelfTriggeredTotal.WithLabelValues(controller, kind).Inc()
}
