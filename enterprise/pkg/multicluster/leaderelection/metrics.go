// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package leaderelection

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.etcd.io/raft/v3/raftpb"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "operator"
	metricsSubsystem = "multicluster_raft"
)

// Static recorded metrics. Package-globals registered to controller-runtime's
// metrics registry once in init() — no plumbing through LockConfiguration.
//
// Naming uses Namespace/Subsystem so the emitted full names are
// `operator_multicluster_raft_<name>`.
var (
	leaderChangesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "leader_changes_total",
		Help:      "Number of cluster-wide leader changes observed by this node.",
	})

	messagesSentTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "messages_sent_total",
		Help:      "Raft messages enqueued for send, by message type and destination peer.",
	}, []string{"msg_type", "peer"})

	messagesReceivedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "messages_received_total",
		Help:      "Raft messages received via the gRPC Send handler, by message type and source peer.",
	}, []string{"msg_type", "peer"})

	sendErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "send_errors_total",
		Help:      "Raft send RPC errors by destination peer and error class.",
	}, []string{"peer", "error_type"})

	messagesDroppedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "messages_dropped_total",
		Help:      "Raft messages dropped at EnqueueSend because the per-peer send queue was full.",
	}, []string{"peer"})

	sendDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "send_duration_seconds",
		Help:      "Latency of one DoSend RPC (cross-region tuned buckets).",
		Buckets: []float64{
			0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5,
		},
	}, []string{"peer", "result"})

	inflightRPCs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "inflight_rpcs",
		Help:      "Number of DoSend RPCs currently in flight to each peer.",
	}, []string{"peer"})

	peerReachable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "peer_reachable",
		Help:      "Whether the last DoSend / ReportUnreachable outcome for each peer was successful (1) or failed (0).",
	}, []string{"peer"})

	unreachableReportsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "unreachable_reports_total",
		Help:      "Calls to raft.Node.ReportUnreachable per peer.",
	}, []string{"peer"})

	snapshotsSentTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "snapshots_sent_total",
		Help:      "Snapshots sent to followers via the MsgSnap fallback in sendOneMessage.",
	}, []string{"peer"})

	snapshotSendErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "snapshot_send_errors_total",
		Help:      "Snapshot DoSend failures per peer.",
	}, []string{"peer"})

	followerMatchLagEntries = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "follower_match_lag_entries",
		Help:      "Leader's last log index minus each follower's Progress.Match. 0 on followers (no Progress data) and on the leader for itself.",
	}, []string{"peer"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		leaderChangesTotal,
		messagesSentTotal,
		messagesReceivedTotal,
		sendErrorsTotal,
		messagesDroppedTotal,
		sendDurationSeconds,
		inflightRPCs,
		peerReachable,
		unreachableReportsTotal,
		snapshotsSentTotal,
		snapshotSendErrorsTotal,
		followerMatchLagEntries,
	)
}

// transportCollector is the prometheus.Collector wrapping a grpcTransport so
// the gauge metrics whose source-of-truth lives in the transport's atomics
// (or in per-peer channel depths) can be read on scrape rather than recorded
// in the Ready loop hot path. Avoids races and double bookkeeping.
//
// Registered exactly once via RegisterTransport — calling RegisterTransport
// twice with the same transport (or with two transports in the same process,
// which never happens in production) would error from the registry's
// duplicate-collector check; callers must register each transport instance
// at most once.
type transportCollector struct {
	t *grpcTransport

	termDesc  *prometheus.Desc
	stateDesc *prometheus.Desc
	queueDesc *prometheus.Desc
}

// Compile-time assertion: transportCollector must implement
// prometheus.Collector. RegisterTransport wires it into
// controller-runtime's metrics registry as one, so a missing or
// signature-drifted Describe / Collect method should fail to build
// instead of failing at runtime registration.
var _ prometheus.Collector = &transportCollector{}

func newTransportCollector(t *grpcTransport) *transportCollector {
	fqName := func(name string) string {
		return prometheus.BuildFQName(metricsNamespace, metricsSubsystem, name)
	}
	return &transportCollector{
		t: t,
		termDesc: prometheus.NewDesc(
			fqName("term"),
			"Current raft term observed by this node.",
			nil, nil,
		),
		stateDesc: prometheus.NewDesc(
			fqName("state"),
			"One series per raft state; value is 1 for the currently active state and 0 otherwise. Use `state{state=\"leader\"} == 1` to identify the leader.",
			[]string{"state"}, nil,
		),
		queueDesc: prometheus.NewDesc(
			fqName("send_queue_length"),
			"Current number of raft messages buffered in each peer's send queue.",
			[]string{"peer"}, nil,
		),
	}
}

func (c *transportCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.termDesc
	ch <- c.stateDesc
	ch <- c.queueDesc
}

// raftStateLabels are the four states we explicitly model. Anything else that
// shows up in the atomic (defensively) is reported under "unknown".
var raftStateLabels = []string{"leader", "follower", "candidate", "pre_candidate", "unknown"}

func (c *transportCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(c.termDesc, prometheus.GaugeValue, float64(c.t.term.Load()))

	currentState := normaliseRaftState(c.t.raftState.Load())
	for _, s := range raftStateLabels {
		v := 0.0
		if s == currentState {
			v = 1.0
		}
		ch <- prometheus.MustNewConstMetric(c.stateDesc, prometheus.GaugeValue, v, s)
	}

	for id, p := range c.t.peers {
		ch <- prometheus.MustNewConstMetric(c.queueDesc, prometheus.GaugeValue, float64(len(p.sendCh)), peerLabel(c.t, id))
	}
}

// normaliseRaftState maps the etcd raft StateType.String() output (which
// looks like "StateLeader") to the lowercase short label used by the
// raft_state metric. Unknown values fall through to "unknown" for stable
// label cardinality.
func normaliseRaftState(v any) string {
	s, ok := v.(string)
	if !ok {
		return "unknown"
	}
	switch s {
	case "StateLeader":
		return "leader"
	case "StateFollower":
		return "follower"
	case "StateCandidate":
		return "candidate"
	case "StatePreCandidate":
		return "pre_candidate"
	default:
		return "unknown"
	}
}

// peerLabel resolves a peer ID to a stable human-readable label for the
// `peer` metric label. Falls back to "id-<N>" so the cardinality is
// well-defined even when idsToNames is missing an entry (e.g. during
// startup before the registration is complete).
func peerLabel(t *grpcTransport, id uint64) string {
	if name, ok := t.idsToNames[id]; ok && name != "" {
		return name
	}
	return fmt.Sprintf("id-%d", id)
}

// transportCollectorMu guards activeTransportCollector against the unlikely
// race where two raft instances start concurrently (only happens in tests
// that run setupLockTest more than once in the same process).
var (
	transportCollectorMu     sync.Mutex
	activeTransportCollector *transportCollector
)

// RegisterTransport wires the gauge-style metrics whose source-of-truth
// lives on the transport (state / term / send_queue_length) into the
// controller-runtime metrics registry via a prometheus.Collector closing
// over t. Must be called after the transport's atomics and peers map are
// fully populated.
//
// Idempotent across multiple calls: the previously registered collector is
// unregistered first so the latest live transport is what's scraped. In
// production exactly one transport exists per process, so the unregister is
// a no-op; the affordance is for tests that bring up multiple clusters
// sequentially in the same binary.
func RegisterTransport(t *grpcTransport) error {
	transportCollectorMu.Lock()
	defer transportCollectorMu.Unlock()

	if activeTransportCollector != nil {
		ctrlmetrics.Registry.Unregister(activeTransportCollector)
		activeTransportCollector = nil
	}
	c := newTransportCollector(t)
	if err := ctrlmetrics.Registry.Register(c); err != nil {
		return err
	}
	activeTransportCollector = c
	return nil
}

// msgTypeLabel maps a raftpb.MessageType to the short label string used by
// the raft_messages_{sent,received}_total counters. Bounded cardinality
// (~10 known types + "unknown") so the resulting series count is small.
func msgTypeLabel(t raftpb.MessageType) string {
	switch t {
	case raftpb.MsgHeartbeat:
		return "heartbeat"
	case raftpb.MsgHeartbeatResp:
		return "heartbeat_resp"
	case raftpb.MsgApp:
		return "append"
	case raftpb.MsgAppResp:
		return "append_resp"
	case raftpb.MsgVote:
		return "vote"
	case raftpb.MsgVoteResp:
		return "vote_resp"
	case raftpb.MsgSnap:
		return "snapshot"
	case raftpb.MsgPreVote:
		return "prevote"
	case raftpb.MsgPreVoteResp:
		return "prevote_resp"
	case raftpb.MsgProp:
		return "propose"
	default:
		return "unknown"
	}
}
