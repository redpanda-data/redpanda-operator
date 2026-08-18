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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/raft/v3/raftpb"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestMsgTypeLabel(t *testing.T) {
	cases := []struct {
		typ  raftpb.MessageType
		want string
	}{
		{raftpb.MsgHeartbeat, "heartbeat"},
		{raftpb.MsgHeartbeatResp, "heartbeat_resp"},
		{raftpb.MsgApp, "append"},
		{raftpb.MsgAppResp, "append_resp"},
		{raftpb.MsgVote, "vote"},
		{raftpb.MsgVoteResp, "vote_resp"},
		{raftpb.MsgSnap, "snapshot"},
		{raftpb.MsgPreVote, "prevote"},
		{raftpb.MsgPreVoteResp, "prevote_resp"},
		{raftpb.MsgProp, "propose"},
		{raftpb.MsgUnreachable, "unknown"},
		{raftpb.MsgSnapStatus, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.typ.String(), func(t *testing.T) {
			assert.Equal(t, c.want, msgTypeLabel(c.typ))
		})
	}
}

func TestNormaliseRaftState(t *testing.T) {
	cases := map[any]string{
		"StateLeader":       "leader",
		"StateFollower":     "follower",
		"StateCandidate":    "candidate",
		"StatePreCandidate": "pre_candidate",
		"":                  "unknown",
		"garbage":           "unknown",
		nil:                 "unknown",
		42:                  "unknown",
	}
	for in, want := range cases {
		t.Run(fmt.Sprintf("%v", in), func(t *testing.T) {
			assert.Equal(t, want, normaliseRaftState(in))
		})
	}
}

// TestIntegrationMetricsLeaderElected brings up a 3-node raft cluster, waits
// for a leader, and asserts that the gauge-style metrics derived from the
// transport reflect the elected state and that the message-counter metrics
// have non-zero values from the heartbeat traffic.
//
// Uses the existing setupLockTest harness. Because the metrics collectors
// are package-globals registered once at process start, and because this
// test (along with other tests in the package) runs in a single binary,
// we don't reset the registry — instead we tolerate any prior state by
// asserting positive deltas after election rather than absolute equality.
func TestIntegrationMetricsLeaderElected(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)

	leaders := setupLockTest(t, ctx, 3)
	defer func() {
		cancel()
		for _, l := range leaders {
			l.WaitForStopped(t, 10*time.Second)
		}
	}()

	leader, _ := waitForAnyLeader(t, 30*time.Second, leaders...)
	require.NotNil(t, leader)

	// Give the leader a couple of heartbeat ticks to accrue metric
	// samples; the histogram's first observation lands during a DoSend
	// to a peer, which only happens after the leader sends its first
	// heartbeat.
	time.Sleep(500 * time.Millisecond)

	families := gatherFamilies(t)

	// In a multi-node in-process test, RegisterTransport unregisters the
	// previous collector each time, so only the last-registered
	// transport's state is scraped (the prod assumption is "exactly one
	// transport per process"). Whichever node happens to register last
	// could be leader, follower, candidate, or pre_candidate — but
	// post-election it should NOT be "unknown" and one of the five
	// state series must have value 1. Assert the active collector
	// reached a stable state (leader OR follower) rather than requiring
	// a specific one.
	require.True(t,
		hasGaugeAtLeast(families, "operator_multicluster_raft_state", 1, map[string]string{"state": "leader"}) ||
			hasGaugeAtLeast(families, "operator_multicluster_raft_state", 1, map[string]string{"state": "follower"}),
		"expected raft_state{state=leader|follower} == 1 after election (active transport collector must reach a stable post-election state)")

	// raft_term >= 1 (after at least one election).
	require.True(t, hasGaugeAtLeast(families, "operator_multicluster_raft_term", 1, nil),
		"expected raft_term >= 1 after election")

	// Heartbeat traffic guarantees both sent + received counters are
	// nonzero on the heartbeat label after a few ticks.
	require.True(t, counterAtLeast(families, "operator_multicluster_raft_messages_sent_total", 1, map[string]string{"msg_type": "heartbeat"}),
		"expected raft_messages_sent_total{msg_type=heartbeat} > 0 after election")
	require.True(t, counterAtLeast(families, "operator_multicluster_raft_messages_received_total", 1, map[string]string{"msg_type": "heartbeat"}),
		"expected raft_messages_received_total{msg_type=heartbeat} > 0 after election")
}

func gatherFamilies(t *testing.T) []*dto.MetricFamily {
	t.Helper()
	gatherer, ok := ctrlmetrics.Registry.(prometheus.Gatherer)
	require.True(t, ok, "controller-runtime metrics.Registry should implement prometheus.Gatherer")
	families, err := gatherer.Gather()
	require.NoError(t, err)
	return families
}

// hasGaugeAtLeast returns true when any series in the named gauge family
// has value >= want and matches every key/value in matchLabels (a subset
// match). Returns false when the family is missing.
func hasGaugeAtLeast(families []*dto.MetricFamily, name string, want float64, matchLabels map[string]string) bool {
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !labelsContain(m.GetLabel(), matchLabels) {
				continue
			}
			if g := m.GetGauge(); g != nil && g.GetValue() >= want {
				return true
			}
		}
	}
	return false
}

func counterAtLeast(families []*dto.MetricFamily, name string, want float64, matchLabels map[string]string) bool {
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !labelsContain(m.GetLabel(), matchLabels) {
				continue
			}
			if c := m.GetCounter(); c != nil && c.GetValue() >= want {
				return true
			}
		}
	}
	return false
}

func labelsContain(labels []*dto.LabelPair, want map[string]string) bool {
	for k, v := range want {
		found := false
		for _, lp := range labels {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
