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

// stretchSubsystem is the prometheus subsystem for the StretchCluster-level
// resource-state gauges. The emitted full names look like
// `operator_stretchcluster_<name>`.
const stretchSubsystem = "stretchcluster"

var (
	// StretchClusterMemberReachable is 1 when the multicluster manager
	// considers a member cluster reachable, 0 otherwise. Sourced from
	// the existing background reachability probe (Manager.IsClusterReachable);
	// recording it as a gauge here avoids parsing log lines or status
	// conditions to find out which member is down right now.
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

	// StretchClusterBrokersReady is the ready (status.readyReplicas)
	// broker count per member. Pair with `brokers` to detect partial
	// outages (a member where brokers > brokers_ready).
	StretchClusterBrokersReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "brokers_ready",
		Help:      "Ready broker count per StretchCluster member, summed across NodePools (sum of NodePool.status.readyReplicas).",
	}, []string{"stretchcluster", "member"})

	// StretchClusterReplicationHealth is 1 when the admin API reports
	// the cluster as healthy (no under-replicated partitions, all
	// brokers up), 0 otherwise. Recorded after the existing health
	// check that reconcileDecommission already runs.
	StretchClusterReplicationHealth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "replication_health",
		Help:      "Cluster-wide replication health from the admin API (1 = healthy, 0 = unhealthy).",
	}, []string{"stretchcluster"})

	// StretchClusterSpecDrift is 1 when a member's local
	// StretchCluster.spec diverges from the locally-stored spec,
	// 0 otherwise. Set inside the existing checkSpecConsistency
	// routine — flipping the gauge there lets dashboards and alerts
	// detect spec drift without parsing the SpecSynced status
	// condition.
	StretchClusterSpecDrift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: stretchSubsystem,
		Name:      "spec_drift",
		Help:      "Whether each member's StretchCluster spec differs from this operator's locally-observed spec (1 = drift detected, 0 = aligned).",
	}, []string{"stretchcluster", "member"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		StretchClusterMemberReachable,
		StretchClusterBrokers,
		StretchClusterBrokersReady,
		StretchClusterReplicationHealth,
		StretchClusterSpecDrift,
	)
}

// RecordStretchClusterMemberReachable updates the per-member reachability
// gauge from a bool. Idiomatic for callers that already have the bool from
// Manager.IsClusterReachable / a Get error check.
func RecordStretchClusterMemberReachable(stretchCluster, member string, reachable bool) {
	v := 0.0
	if reachable {
		v = 1.0
	}
	StretchClusterMemberReachable.WithLabelValues(stretchCluster, member).Set(v)
}

// RecordStretchClusterBrokers sets both `brokers` (desired) and
// `brokers_ready` for a single member in one call. The two metrics are
// recorded together because the meaningful query is almost always
// `brokers - brokers_ready > 0` (partial outage) and emitting them
// in lockstep keeps that query free of staleness races.
func RecordStretchClusterBrokers(stretchCluster, member string, desired, ready int32) {
	StretchClusterBrokers.WithLabelValues(stretchCluster, member).Set(float64(desired))
	StretchClusterBrokersReady.WithLabelValues(stretchCluster, member).Set(float64(ready))
}

// RecordStretchClusterReplicationHealth updates the cluster-wide
// replication-health gauge. Called once per reconcile after the admin
// API health check returns.
func RecordStretchClusterReplicationHealth(stretchCluster string, healthy bool) {
	v := 0.0
	if healthy {
		v = 1.0
	}
	StretchClusterReplicationHealth.WithLabelValues(stretchCluster).Set(v)
}

// RecordStretchClusterSpecDrift sets the per-member spec-drift gauge.
// Called from inside checkSpecConsistency on every reconcile pass so the
// gauge always reflects what was true at the end of the most recent
// consistency check.
func RecordStretchClusterSpecDrift(stretchCluster, member string, drift bool) {
	v := 0.0
	if drift {
		v = 1.0
	}
	StretchClusterSpecDrift.WithLabelValues(stretchCluster, member).Set(v)
}
