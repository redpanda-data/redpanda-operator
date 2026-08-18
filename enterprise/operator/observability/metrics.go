// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package observability holds the Prometheus metrics recorded by the
// enterprise stretch/multicluster controllers and their shared remediation
// cores. It is the enterprise counterpart of the OSS operator's
// internal/observability package: the generic reconcile-health machinery
// (wrapper, recorder) stays OSS and is injected through the ReconcilerWrapper
// seam, while the metrics only this module's code records live here.
//
// Metric names, subsystems, and label sets MUST NOT change: the operator
// chart's PrometheusRule (operator/chart/prometheusrule.go) matches them as
// strings, and both packages register into the same controller-runtime
// default registry (a renamed metric would silently break dashboards and
// alerts). The name contract is pinned by the drift test in
// operator/internal/enterprisedrift.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "operator"
	metricsSubsystem = "controller"
	stretchSubsystem = "stretchcluster"
)

// ====================================================================
// Maintenance-mode remediation counters.
//
// Incremented by the shared ClearStuckMaintenanceMode core in
// enterprise/operator/controller, which serves BOTH the StretchCluster
// MulticlusterReconciler and (via its exported entry point) the OSS
// single-cluster RedpandaReconciler's thin wrapper.
// ====================================================================

var (
	// MaintenanceModeCleared counts brokers whose stuck maintenance-mode flag
	// the operator cleared because the broker had been down (pod not-Ready)
	// past the configured threshold. A broker left in maintenance mode is
	// excluded from the partition balancer's auto-decommission, so this
	// remediation unblocks recovery. Labeled by the broker's cluster (member)
	// name (empty for the single-cluster Redpanda reconciler).
	MaintenanceModeCleared = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "maintenance_mode_cleared_total",
		Help:      "Brokers whose stuck maintenance-mode flag was cleared by the operator after being down past the threshold, labeled by cluster.",
	}, []string{"cluster"})

	// MaintenanceModeGhostCleared counts ghost brokers whose leaked
	// maintenance-mode flag the operator cleared: broker ids proven superseded
	// at their own advertised address (a pod that lost its data directory and
	// re-registered under a fresh id), left in maintenance mode by the pod's
	// preStop hook (redpanda-data/redpanda-operator#1674). Supersession is
	// proven either by a live registered broker sharing the ghost's address
	// under a different id, or — when the successor cannot register at all,
	// the leader-restart deadlock of redpanda#31057 — by the pod at that
	// address self-reporting a different node id via its local admin API. Such
	// a ghost occupies the cluster's single maintenance-mode slot and is
	// excluded from the partition balancer's auto-decommission until cleared.
	// Labeled by the broker's cluster (member) name (empty for the
	// single-cluster Redpanda reconciler).
	MaintenanceModeGhostCleared = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "maintenance_mode_ghost_cleared_total",
		Help:      "Ghost brokers (proven superseded at their own advertised address) whose leaked maintenance-mode flag was cleared by the operator, labeled by cluster.",
	}, []string{"cluster"})

	// MaintenanceModeClearSkippedAmbiguous counts pods whose long-down state
	// would otherwise gate a maintenance-mode clear, but whose pod name matched
	// more than one broker (e.g. a StretchCluster with identically-named
	// BrokerPools in two member clusters) and was therefore skipped rather than
	// guessed. A sustained non-zero rate means a broker may be permanently stuck
	// in maintenance mode because its pod name is ambiguous; the BrokerPool name
	// collision must be resolved to unblock it. Labeled by the pod's cluster
	// (member) name (empty for the single-cluster Redpanda reconciler).
	MaintenanceModeClearSkippedAmbiguous = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "maintenance_mode_clear_skipped_ambiguous_total",
		Help:      "Pods that satisfied the maintenance-mode clear threshold but whose pod name ambiguously matched more than one broker, so no clear was attempted, labeled by cluster.",
	}, []string{"cluster"})
)

// ====================================================================
// StretchCluster-level resource-state gauges.
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
	// summed across all NodePools that point at that member. A healthy
	// converged cluster has `brokers == brokers_ready` on every member.
	StretchClusterBrokers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "brokers",
		Help:      "Desired broker count per StretchCluster member, summed across NodePools.",
	}, []string{"stretchcluster", "member"})

	// StretchClusterBrokersReady is the ready broker count per member.
	// Pair with `brokers` to detect partial outages (a member where
	// brokers > brokers_ready).
	StretchClusterBrokersReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "brokers_ready",
		Help:      "Ready broker count per StretchCluster member, summed across NodePools (sum of NodePool.status.readyReplicas).",
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

func init() {
	// Register into controller-runtime's default registry — the SAME registry
	// the OSS operator's internal/observability package registers its metrics
	// into — so every metric is served from the one /metrics endpoint. These
	// metrics moved here wholesale from the OSS package (which no longer
	// defines or registers them), so each is registered exactly once.
	ctrlmetrics.Registry.MustRegister(
		MaintenanceModeCleared,
		MaintenanceModeGhostCleared,
		MaintenanceModeClearSkippedAmbiguous,

		StretchClusterMemberReachable,
		StretchClusterBrokers,
		StretchClusterBrokersReady,
		StretchClusterReplicationHealth,
		StretchClusterSpecDrift,
	)
}
