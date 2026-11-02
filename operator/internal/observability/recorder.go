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
//   - StretchCluster-level member reachability, broker counts,
//     replication health, and spec drift.
//   - Redpanda CR resource-state counts (v1 + v2).
//   - Broker CR counts per owning cluster and phase (both controller modes).
//
// Layout of the package:
//
//   - metrics.go — single source of truth for every metric variable
//     and the init() that registers them all with controller-runtime's
//     Prometheus registry.
//   - wrapper.go — Wrap() middleware that records the per-reconcile
//     metrics around every controller invocation.
//   - stretch_recorder.go — RecordStretchCluster* helper functions
//     for the StretchCluster gauges.
//   - recorder.go — this doc and BrokerPhaseRecorder, the per-kind
//     writer of the BrokerPhase gauge used by both metric controllers.
//
// Metric naming follows the existing `operator_<subsystem>_<name>`
// convention. All metric labels have closed vocabularies — no per-pod
// or per-object labels that would explode cardinality.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

// Values of BrokerPhase's `kind` label: the kind of cluster CR a Broker
// belongs to. A V1 Cluster and a V2 Redpanda may share name and namespace
// (the V1→V2 migration shape) while each is counted by its own metric
// controller, so the label keeps the two writers on disjoint series.
const (
	BrokerKindCluster  = "Cluster"
	BrokerKindRedpanda = "Redpanda"
)

// BrokerPhaseUnknown is the `phase` label for a Broker whose status.phase is
// still empty: not yet reconciled, or never, while the Broker controller is
// disabled.
const BrokerPhaseUnknown = "Unknown"

// brokerPhases is every value the `phase` label takes. Each cluster gets all
// of them, zero-filled, so a `> 0` alert resolves by value rather than by
// the series disappearing.
var brokerPhases = []string{
	string(redpandav1alpha2.BrokerPhasePending),
	string(redpandav1alpha2.BrokerPhaseProvisioning),
	string(redpandav1alpha2.BrokerPhaseRunning),
	string(redpandav1alpha2.BrokerPhaseDecommissioning),
	string(redpandav1alpha2.BrokerPhaseDecommissioned),
	string(redpandav1alpha2.BrokerPhaseStuck),
	string(redpandav1alpha2.BrokerPhaseDiskLost),
	BrokerPhaseUnknown,
}

// BrokerClusterKey identifies one cluster's series on BrokerPhase.
type BrokerClusterKey struct {
	Namespace, Kind, Name string
}

// BrokerPhaseRecorder owns the BrokerPhase series of one kind. It remembers
// the clusters it emitted so one whose Brokers are all gone has its series
// deleted rather than frozen at their last values.
type BrokerPhaseRecorder struct {
	kind    string
	current collections.Set[BrokerClusterKey]
}

func NewBrokerPhaseRecorder(kind string) *BrokerPhaseRecorder {
	return &BrokerPhaseRecorder{kind: kind, current: collections.NewConcurrentSet[BrokerClusterKey]()}
}

// Record replaces the recorder's series with what brokers implies. Brokers
// of another kind are ignored; Record(nil) deletes every series the recorder
// emitted.
func (r *BrokerPhaseRecorder) Record(brokers []redpandav1alpha2.Broker) {
	counts := brokerPhaseCounts(brokers, r.kind)
	for key, phases := range counts {
		for phase, n := range phases {
			BrokerPhase.WithLabelValues(key.Namespace, key.Kind, key.Name, phase).Set(n)
		}
		r.current.Add(key)
	}
	for _, key := range r.current.Values() {
		if _, ok := counts[key]; !ok {
			BrokerPhase.DeletePartialMatch(prometheus.Labels{"namespace": key.Namespace, "kind": key.Kind, "name": key.Name})
			r.current.Delete(key)
		}
	}
}

// brokerPhaseCounts groups the brokers of one kind by cluster and
// status.phase, zero-filling every phase of every cluster it sees.
func brokerPhaseCounts(brokers []redpandav1alpha2.Broker, kind string) map[BrokerClusterKey]map[string]float64 {
	counts := map[BrokerClusterKey]map[string]float64{}
	for i := range brokers {
		b := &brokers[i]
		brokerKind := BrokerKindRedpanda
		if b.Spec.ClusterRef.IsV1() {
			brokerKind = BrokerKindCluster
		}
		if brokerKind != kind {
			continue
		}
		// Same resolution as Broker.PodName: every creator stamps the
		// cluster-name label; without it the ref names the cluster unless it
		// names a NodePool.
		name := b.Labels[redpandav1alpha2.ClusterNameLabel]
		if name == "" && !b.Spec.ClusterRef.IsNodePool() {
			name = b.Spec.ClusterRef.Name
		}
		if name == "" {
			continue
		}
		key := BrokerClusterKey{Namespace: b.Namespace, Kind: kind, Name: name}
		phases, ok := counts[key]
		if !ok {
			phases = make(map[string]float64, len(brokerPhases))
			for _, p := range brokerPhases {
				phases[p] = 0
			}
			counts[key] = phases
		}
		phase := string(b.Status.Phase)
		if phase == "" {
			phase = BrokerPhaseUnknown
		}
		phases[phase]++
	}
	return counts
}
