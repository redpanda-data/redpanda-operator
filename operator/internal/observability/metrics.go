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
// exported by the operator (except the multicluster raft metrics, which
// live in `pkg/multicluster/leaderelection/metrics.go` — a different Go
// module). Recorder helper functions that set these metrics live in
// sibling files in this package.
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
	stretchSubsystem = "stretchcluster"
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
)

// ====================================================================
// Group 2 — StretchCluster-level resource-state gauges.
//
// Set by the MulticlusterReconciler. Helper functions for these gauges
// live in stretch_recorder.go.
// ====================================================================

var (
	// StretchClusterMemberReachable is 1 when the multicluster manager
	// considers a member cluster reachable, 0 otherwise.
	StretchClusterMemberReachable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "member_reachable",
		Help:      "Whether each StretchCluster member cluster is reachable from this operator (1 = reachable, 0 = unreachable).",
	}, []string{"stretchcluster", "member"})

	// StretchClusterBrokers is the desired broker count per member,
	// summed across all RedpandaBrokerPools that point at that member. A
	// healthy converged cluster has `brokers == brokers_ready` on every
	// member.
	StretchClusterBrokers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "brokers",
		Help:      "Desired broker count per StretchCluster member, summed across RedpandaBrokerPools.",
	}, []string{"stretchcluster", "member"})

	// StretchClusterBrokersReady is the ready broker count per member.
	// Pair with `brokers` to detect partial outages (a member where
	// brokers > brokers_ready).
	StretchClusterBrokersReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "brokers_ready",
		Help:      "Ready broker count per StretchCluster member, summed across RedpandaBrokerPools (sum of RedpandaBrokerPool.status.readyReplicas).",
	}, []string{"stretchcluster", "member"})

	// StretchClusterReplicationHealth is 1 when the admin API reports
	// the cluster as healthy, 0 otherwise. Recorded after the existing
	// health check that reconcileDecommission already runs.
	StretchClusterReplicationHealth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "replication_health",
		Help:      "Cluster-wide replication health from the admin API (1 = healthy, 0 = unhealthy).",
	}, []string{"stretchcluster"})

	// StretchClusterSpecDrift is 1 when a member's local
	// StretchCluster.spec diverges from this operator's locally-observed
	// spec, 0 otherwise. Set inside the existing checkSpecConsistency
	// routine.
	StretchClusterSpecDrift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "spec_drift",
		Help:      "Whether each member's StretchCluster spec differs from this operator's locally-observed spec (1 = drift detected, 0 = aligned).",
	}, []string{"stretchcluster", "member"})
)

// ====================================================================
// Group 3 — Redpanda CR resource-state gauges (v2 only).
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

		// Group 2 — StretchCluster member status.
		StretchClusterMemberReachable,
		StretchClusterBrokers,
		StretchClusterBrokersReady,
		StretchClusterReplicationHealth,
		StretchClusterSpecDrift,

		// Group 3 — Redpanda CR resource-state (v2 only; v1 lives next
		// to its legacy reconciler in operator/internal/controller/vectorized/).
		Redpandas,
		RedpandaDesiredNodes,
		RedpandaReadyNodes,
		RedpandaMisconfiguredClusters,
	)
}
