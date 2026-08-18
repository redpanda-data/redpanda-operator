// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package observability

// Recorder helpers for the StretchCluster gauges defined in metrics.go.
// These exist to keep call sites short — `RecordStretchClusterMemberReachable(sc, member, bool)`
// is cheaper to read than the equivalent gauge.WithLabelValues().Set()
// dance with a bool-to-float conversion at every call site.

// RecordStretchClusterMemberReachable updates the per-member reachability
// gauge from a bool.
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
// `brokers - brokers_ready > 0` (partial outage) and emitting them in
// lockstep keeps that query free of staleness races.
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
