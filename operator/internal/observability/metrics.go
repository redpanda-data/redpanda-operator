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
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// This file is the single source of truth for every Prometheus metric
// exported by the OSS operator (except the multicluster raft metrics, which
// live in `pkg/multicluster/leaderelection/metrics.go`, and the
// stretch/maintenance-remediation metrics, which live in
// `enterprise/operator/observability` — both different Go modules; all
// register into the same controller-runtime default registry). Recorder
// helper functions that set these metrics live in sibling files in this
// package.
//
// Metric naming follows the existing `operator_<subsystem>_<name>`
// convention for operator-internal metrics; the resource-state Redpanda
// metrics use shorter `redpanda_*` names that pre-date the convention
// and are kept for backward compatibility.
//
// All metric labels have closed vocabularies — no per-pod, per-namespace
// (other than where it's the natural identity of the resource being
// counted), or per-object labels that would explode cardinality.

const (
	metricsNamespace = "operator"
	metricsSubsystem = "controller"
)

// ====================================================================
// Group 1 — wrapper-emitted reconcile-health metrics.
//
// These are set automatically by observability.Wrap around every
// reconcile of every wrapped controller. No controller-side wiring
// required. Cardinality bounded by `controller` label only.
// ====================================================================

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

	// ReconcileLastSuccessTimestampSeconds is the Unix timestamp of the
	// most recent steady-state reconcile for a given controller (see
	// ReconcileSteadyStateTotal for the definition of steady state).
	// Prometheus computes "seconds since last success" at query time
	// as `time() - operator_controller_reconcile_last_success_timestamp_seconds`,
	// avoiding the goroutine / oldest-unfinished tracking that an
	// imperative "seconds elapsed" gauge would need.
	ReconcileLastSuccessTimestampSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_last_success_timestamp_seconds",
		Help:      "Unix timestamp of the most recent steady-state reconcile per controller. Use time() - this for seconds-since-last-success.",
	}, []string{"controller"})

	// PVCUnbinderGateDeferred counts reconciles where the PVCUnbinder
	// declined to act because one of its gates fired. Use this to
	// detect silent inaction — e.g., a forgotten pause annotation, a
	// stuck multi-node-event signal, or a cache-staleness hold that
	// never clears. The `gate` label values are: "pause" (Gate 1),
	// "multi-node" (Gate 2), "in-flight" (Gate 0, a previous
	// unbind for the cluster has not settled yet), "pvc-rebinding" (Gate 3, a PVC in the cluster is
	// recreated but not yet bound — except claims whose Pods are
	// PROVABLY deadlocked on a mis-pinned local-PV claim per the
	// unbinder's stuckClaimNames proof chain; other stuck Pods, e.g.
	// under soft anti-affinity or required terms the unbinder cannot
	// interpret, non-WaitForFirstConsumer
	// classes, or unresolvable PV node affinity, still increment it,
	// so do NOT exclude stuck Pods from alerts on this gate; under
	// --allow-pv-rebinding or --disable-pvc-rebinding-gate-exemption
	// the exemption is disabled entirely and every unbound claim
	// counts), and "freed-pv" (Gate 4, a PV whose
	// ClaimRef we cleared under --allow-pv-rebinding is still Available
	// with a live node — unbinding more pods could mis-pair disks).
	// Note: the "multi-node" gate also holds the known unfixed sibling
	// of the mis-pinned-claim deadlock — when two victims' PVs land on
	// two different occupied nodes, the unbinder defers there and
	// manual PVC deletion is required.
	PVCUnbinderGateDeferred = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "pvc_unbinder_gate_deferred_total",
		Help:      "PVCUnbinder reconciles that returned early because a safety gate deferred remediation, labeled by which gate fired.",
	}, []string{"gate"})

	// PVCUnbinderGateExempted counts reconciles where the pvc-rebinding
	// gate (Gate 3) was PASSED because every unbound claim was exempted
	// as a stuck-Pod claim — i.e. the unbinder overrode a safety gate
	// and proceeded to destructive remediation. This is the metric
	// counterpart of the PVCUnbinderGateExempted Event; alert or trend
	// on it to notice a mis-firing exemption even after Events age out.
	PVCUnbinderGateExempted = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "pvc_unbinder_gate_exempted_total",
		Help:      "PVCUnbinder reconciles that proceeded past the pvc-rebinding gate because all unbound claims were exempted as stuck-Pod claims.",
	})

	// NOTE: the maintenance-mode remediation counters
	// (operator_controller_maintenance_mode_*) and the StretchCluster-level
	// gauges (operator_stretchcluster_*) moved to
	// enterprise/operator/observability alongside the shared remediation
	// cores and stretch controllers that record them. They register into
	// this same controller-runtime default registry.
)

// ====================================================================
// Group 2 — Redpanda CR resource-state gauges (v2 only).
//
// Set by RedpandaMetricsReconciler in operator/internal/controller/
// redpanda/metric_controller.go. The reconciler recomputes totals from
// a fresh List on every event so the gauges stay accurate even when
// individual events are coalesced.
//
// The v1 (vectorized.redpanda.com Cluster) family is intentionally
// not consolidated here — that controller is considered legacy and is
// frozen against unrelated changes. Its metrics continue to live next
// to its reconciler in operator/internal/controller/vectorized/.
// ====================================================================

var (
	Redpandas = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "redpandas",
		Help: "Number of Redpanda clusters (cluster.redpanda.com/v1alpha2) managed by the operator.",
	})

	RedpandaDesiredNodes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redpanda_desired_nodes",
		Help: "Desired number of broker pods per Redpanda cluster, summed across all node pools.",
	}, []string{"namespace", "name"})

	RedpandaReadyNodes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redpanda_ready_nodes",
		Help: "Number of broker pods reporting Ready per Redpanda cluster, summed across all node pools.",
	}, []string{"namespace", "name"})

	RedpandaMisconfiguredClusters = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redpanda_misconfigured_clusters",
		Help: "Number of Redpanda clusters whose ConfigurationApplied condition is not True, labeled by reason.",
	}, []string{"reason"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		// Group 1 — reconcile-health (wrapper-emitted).
		ReconcileSteadyStateTotal,
		ReconcileLastSuccessTimestampSeconds,
		PVCUnbinderGateDeferred,
		PVCUnbinderGateExempted,

		// Group 2 — Redpanda CR resource-state (v2 only; v1 lives next
		// to its legacy reconciler in operator/internal/controller/vectorized/).
		Redpandas,
		RedpandaDesiredNodes,
		RedpandaReadyNodes,
		RedpandaMisconfiguredClusters,
	)
}
