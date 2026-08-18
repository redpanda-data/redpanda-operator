// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
	"github.com/redpanda-data/redpanda-operator/enterprise/operator/observability"
)

// TestPodNotReadyFor: the not-ready duration comes from the pod's Ready
// condition transition; a pod with no Ready condition falls back to time since
// CreationTimestamp, so a long-Pending pod is not pinned at zero.
func TestPodNotReadyFor(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	pod := func(status corev1.ConditionStatus, since time.Time) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.NewTime(since),
		}}}}
	}

	t.Run("ready pod reports zero, below any positive threshold", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), podNotReadyFor(pod(corev1.ConditionTrue, now.Add(-time.Hour)), now))
	})
	t.Run("not-ready pod returns duration since transition", func(t *testing.T) {
		assert.Equal(t, 8*time.Minute, podNotReadyFor(pod(corev1.ConditionFalse, now.Add(-8*time.Minute)), now))
	})
	t.Run("freshly created pod with no Ready condition is not-ready for ~zero duration", func(t *testing.T) {
		freshPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now)}}
		assert.Equal(t, time.Duration(0), podNotReadyFor(freshPod, now))
	})
	t.Run("pod stuck Pending with no Ready condition for a long time is not-ready for that full duration", func(t *testing.T) {
		stuckPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour))},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
			}}},
		}
		assert.GreaterOrEqual(t, podNotReadyFor(stuckPod, now), 1*time.Hour, "expected notReadyFor to reflect ~2h since pod creation, not be pinned at 0")
	})
}

// TestDecideClearMaintenance: clear maintenance only when the broker is
// draining AND not-alive AND its pod has been not-Ready at least the threshold.
func TestDecideClearMaintenance(t *testing.T) {
	const threshold = 5 * time.Minute
	cases := []struct {
		name          string
		inMaintenance bool
		isAlive       bool
		notReadyFor   time.Duration
		want          bool
	}{
		{"in maintenance + down + past threshold -> clear", true, false, 6 * time.Minute, true},
		{"not in maintenance -> no clear", false, false, 30 * time.Minute, false},
		{"in maintenance but alive -> no clear", true, true, 30 * time.Minute, false},
		{"in maintenance + down but under threshold -> no clear", true, false, 2 * time.Minute, false},
		{"exactly at threshold -> clear", true, false, 5 * time.Minute, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decideClearMaintenance(tc.inMaintenance, tc.isAlive, tc.notReadyFor, threshold)
			assert.Equal(t, tc.want, got, "reason: %s", reason)
		})
	}
}

// TestBrokersByPodName maps a broker's advertised RPC address to its pod name
// (first DNS label), handling both "host" and "host:port" forms.
func TestBrokersByPodName(t *testing.T) {
	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-rp-east-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 2, InternalRPCAddress: "redpanda-rp-west-0.redpanda:33145", IsAlive: ptr.To(true)},
		{NodeID: 5, InternalRPCAddress: "10.140.1.15", IsAlive: ptr.To(true)},
	}
	m := brokersByPodName(brokers)
	require.Len(t, m["redpanda-rp-east-0"], 1)
	assert.Equal(t, 0, m["redpanda-rp-east-0"][0].NodeID)
	assert.True(t, m["redpanda-rp-east-0"][0].Maintenance.Draining)
	require.Len(t, m["redpanda-rp-west-0"], 1)
	assert.Equal(t, 2, m["redpanda-rp-west-0"][0].NodeID)
	require.Len(t, m["10.140.1.15"], 1)
	assert.Equal(t, 5, m["10.140.1.15"][0].NodeID)
	_, ok := m["nonexistent"]
	assert.False(t, ok)
}

// TestBrokersByPodNameCollision: when two brokers share a pod-name key
// (StretchCluster identically-named BrokerPools), both must be bucketed under
// it so the caller can detect the ambiguity rather than pair the wrong broker.
func TestBrokersByPodNameCollision(t *testing.T) {
	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-default-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 7, InternalRPCAddress: "redpanda-default-0.redpanda", IsAlive: ptr.To(true)},
	}
	m := brokersByPodName(brokers)
	require.Len(t, m["redpanda-default-0"], 2, "both brokers sharing the colliding pod name must be retained, not overwritten")
	nodeIDs := []int{m["redpanda-default-0"][0].NodeID, m["redpanda-default-0"][1].NodeID}
	assert.ElementsMatch(t, []int{0, 7}, nodeIDs)
}

// TestBrokersByPodNameIgnoresEmptyAddress: a broker with an empty
// InternalRPCAddress must never be indexed, or the "" key could pair it with an
// unrelated pod.
func TestBrokersByPodNameIgnoresEmptyAddress(t *testing.T) {
	brokers := []rpadmin.Broker{
		{NodeID: 3, InternalRPCAddress: "", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 4, InternalRPCAddress: "redpanda-default-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}
	m := brokersByPodName(brokers)
	_, ok := m[""]
	assert.False(t, ok, "a broker with an empty address must not be indexed at all")
	require.Len(t, m["redpanda-default-0"], 1)
	assert.Equal(t, 4, m["redpanda-default-0"][0].NodeID)
}

// TestClearStuckMaintenanceModeIntegration drives ClearStuckMaintenanceMode
// against a real rpadmin client: only the broker that is in maintenance,
// not-alive, and past the not-Ready threshold is cleared; any broker failing a
// single gate is left untouched.
func TestClearStuckMaintenanceModeIntegration(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	// Cluster brokers: node 0 is the only one that should be cleared.
	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-rp-east-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},    // clear
		{NodeID: 2, InternalRPCAddress: "redpanda-rp-west-0.redpanda", IsAlive: ptr.To(true), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},     // alive -> skip
		{NodeID: 3, InternalRPCAddress: "redpanda-rp-eu-0.redpanda", IsAlive: ptr.To(false)},                                                               // not in maintenance -> skip
		{NodeID: 4, InternalRPCAddress: "redpanda-rp-central-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}}, // under threshold -> skip
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	// Leader resolution for DisableMaintenanceMode(useLeaderNode=true): the
	// single test host is node 2 and is the controller leader, so sendToLeader
	// routes the DELETE here.
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 2})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 2})
	})
	// DELETE /v1/brokers/{id}/maintenance — record the cleared node id.
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			var id int
			if n, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				id = n
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	notReady := func(name string, dur time.Duration) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-dur)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{
		notReady("redpanda-rp-east-0", 10*time.Minute),   // node 0: clear
		notReady("redpanda-rp-west-0", 10*time.Minute),   // node 2: alive -> skip
		notReady("redpanda-rp-eu-0", 10*time.Minute),     // node 3: not in maintenance -> skip
		notReady("redpanda-rp-central-0", 2*time.Minute), // node 4: under threshold -> skip
	}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "only the in-maintenance, not-alive, long-down broker should be cleared")
}

// TestClearStuckMaintenanceModeClearsPendingPodWithNoReadyCondition: a broker
// whose pod is stuck Pending with only a PodScheduled=False condition (no
// PodReady) must still be cleared, its not-ready duration measured from
// CreationTimestamp rather than pinned at zero.
func TestClearStuckMaintenanceModeClearsPendingPodWithNoReadyCondition(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-rp-east-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			var id int
			if n, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				id = n
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	pods := []*lifecycle.MulticlusterPod{
		{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "redpanda-rp-east-0",
				CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
			}}},
		}},
	}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "broker stuck 2h in a never-scheduled Pending pod (no PodReady condition) should have maintenance mode cleared")
}

// TestClearStuckMaintenanceModeSkipsAmbiguousPodName: when two brokers each
// satisfy every clear gate but share a pod name (StretchCluster collision),
// clearing either would be a guess, so neither is cleared.
func TestClearStuckMaintenanceModeSkipsAmbiguousPodName(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-default-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 9, InternalRPCAddress: "redpanda-default-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			var id int
			if n, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				id = n
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	notReady := func(name string, dur time.Duration) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-dur)),
			}}},
		}}
	}
	// Both pods are named identically, one per (distinct) member cluster.
	pods := []*lifecycle.MulticlusterPod{
		notReady("redpanda-default-0", 10*time.Minute),
		notReady("redpanda-default-0", 10*time.Minute),
	}

	skippedBefore := testutil.ToFloat64(observability.MaintenanceModeClearSkippedAmbiguous.WithLabelValues(""))

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "an ambiguous pod-name match must not clear maintenance mode on either broker")
	skippedAfter := testutil.ToFloat64(observability.MaintenanceModeClearSkippedAmbiguous.WithLabelValues(""))
	assert.Equal(t, float64(2), skippedAfter-skippedBefore, "both ambiguous pods should increment the skipped-ambiguous metric")
}

// TestClearStuckMaintenanceModeIgnoresEmptyPodIP: a Pending pod with no PodIP
// must never pair, through the empty-string key, with a broker reporting an
// empty InternalRPCAddress.
func TestClearStuckMaintenanceModeIgnoresEmptyPodIP(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			var id int
			if n, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				id = n
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	// Pending pod: never scheduled, so it has no PodIP, and its name matches no
	// broker's InternalRPCAddress-derived key.
	pods := []*lifecycle.MulticlusterPod{
		{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-pending-0"},
			Status: corev1.PodStatus{
				PodIP: "",
				Conditions: []corev1.PodCondition{{
					Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-10 * time.Minute)),
				}},
			},
		}},
	}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "a Pending pod with no PodIP must not be paired with a broker reporting an empty address")
}

// TestGhostBrokersInMaintenance: a broker is a ghost when it is not-alive,
// draining, and its advertised host:port is also advertised by a live broker
// under a different node id (issue #1674). Bare-IP and empty addresses are
// never ghost-matched, since a reused pod IP is not a stable identity.
func TestGhostBrokersInMaintenance(t *testing.T) {
	const addr = "redpanda-1.redpanda.maint.svc.cluster.local."
	dead := func(id int, address string, port int, draining bool) rpadmin.Broker {
		b := rpadmin.Broker{NodeID: id, InternalRPCAddress: address, InternalRPCPort: port, IsAlive: ptr.To(false)}
		if draining {
			b.Maintenance = &rpadmin.MaintenanceStatus{Draining: true}
		}
		return b
	}
	alive := func(id int, address string, port int) rpadmin.Broker {
		return rpadmin.Broker{NodeID: id, InternalRPCAddress: address, InternalRPCPort: port, IsAlive: ptr.To(true)}
	}

	ghostIDs := func(brokers []rpadmin.Broker) []int {
		ids := []int{}
		for _, g := range ghostBrokersInMaintenance(brokers) {
			ids = append(ids, g.NodeID)
		}
		return ids
	}

	t.Run("dead draining broker superseded by a live broker at the same address is a ghost", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			alive(0, "redpanda-0.redpanda.maint.svc.cluster.local.", 33145),
			dead(1, addr, 33145, true),
			alive(3, addr, 33145),
		}
		assert.Equal(t, []int{1}, ghostIDs(brokers))
	})
	t.Run("multiple leaked ids on the same address are all ghosts, in node-id order", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(4, addr, 33145, true),
			dead(1, addr, 33145, true),
			alive(5, addr, 33145),
		}
		assert.Equal(t, []int{1, 4}, ghostIDs(brokers))
	})
	t.Run("no live successor (pod mid-restart) means no ghost", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			alive(0, "redpanda-0.redpanda.maint.svc.cluster.local.", 33145),
			dead(1, addr, 33145, true),
		}
		assert.Empty(t, ghostIDs(brokers))
	})
	t.Run("dead sharer not in maintenance is not cleared", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(1, addr, 33145, false),
			alive(3, addr, 33145),
		}
		assert.Empty(t, ghostIDs(brokers))
	})
	t.Run("same host but different RPC ports are distinct brokers, not ghosts", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(1, "node-a.example.com", 33145, true),
			alive(2, "node-a.example.com", 33146),
		}
		assert.Empty(t, ghostIDs(brokers))
	})
	t.Run("host:port address form is normalized against the port field", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(1, addr+":33145", 33145, true),
			alive(3, addr, 33145),
		}
		assert.Equal(t, []int{1}, ghostIDs(brokers))
	})
	t.Run("nil is_alive defaults to alive and is never a ghost", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			{NodeID: 1, InternalRPCAddress: addr, InternalRPCPort: 33145, Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
			alive(3, addr, 33145),
		}
		assert.Empty(t, ghostIDs(brokers))
	})
	t.Run("empty addresses are never bucketed together", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(1, "", 33145, true),
			alive(3, "", 33145),
		}
		assert.Empty(t, ghostIDs(brokers))
	})
	t.Run("bare-IP addresses are never ghost-matched: a reused pod IP is not a stable identity", func(t *testing.T) {
		// A draining broker's IP reused by an unrelated live pod must not make
		// it a ghost: it is still expected back under its old id.
		brokers := []rpadmin.Broker{
			dead(2, "10.1.0.5", 33145, true),
			alive(7, "10.1.0.5", 33145),
		}
		assert.Empty(t, ghostIDs(brokers))
		brokersWithPort := []rpadmin.Broker{
			dead(2, "10.1.0.5:33145", 33145, true),
			alive(7, "10.1.0.5:33145", 33145),
		}
		assert.Empty(t, ghostIDs(brokersWithPort))
	})
}

// TestClearStuckMaintenanceModeClearsGhostWithNoObservedPod: the ghost rule's
// ambiguity guard refuses only >1 observed pod for the name, never zero. A
// ghost with a live successor but no matching operator pod must still be
// cleared, since the clear targets a node id, not a pod.
func TestClearStuckMaintenanceModeClearsGhostWithNoObservedPod(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 3, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)}, // live successor at the same address
	}
	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(brokers) })
	mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": true})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			if id, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				mu.Lock()
				disabled = append(disabled, id)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	// No pod named redpanda-1 in the operator's (partial) view.
	now := time.Now()
	pods := []*lifecycle.MulticlusterPod{{Pod: &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "redpanda-9"},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour))}}},
	}}}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "a ghost with a live successor must clear even when no operator pod matches its address (0 matches != ambiguity)")
}

// TestClearStuckMaintenanceModeClearsFlatNetworkDrainer: a flat-network drainer
// advertising a bare IP (invisible to the IP-skipping ghost rule) is recovered
// by the pod-identity rule, which matches by PodIP, dials the pod, and clears
// only after the pod's broker reports a different node id.
func TestClearStuckMaintenanceModeClearsFlatNetworkDrainer(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	// Node 0 dead+draining, advertising a bare IP, no live owner -> unclaimed.
	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "10.1.0.5", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 1, InternalRPCAddress: "10.1.0.6", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 2, InternalRPCAddress: "10.1.0.7", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
	}
	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(brokers) })
	mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": true})
	})
	// leaderAdmin pins the controller leader: node 1 (a live broker at
	// 10.1.0.6 = pod redpanda-1) is the leader and the bootstrap, so it serves
	// the authoritative re-read and the write.
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 1})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			if id, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				mu.Lock()
				disabled = append(disabled, id)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	// The pod now at 10.1.0.5 runs node 6 (the replacement), advertising its
	// own IP — proof node 0 is superseded and can never rejoin.
	podMux := http.NewServeMux()
	podMux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":            6,
			"advertised_rpc_api": map[string]any{"address": "10.1.0.5", "port": 33145},
		})
	})
	podSrv := httptest.NewServer(podMux)
	defer podSrv.Close()

	now := time.Now()
	readyIP := func(name, ip string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{PodIP: ip, Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{
		{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-0"},
			Status: corev1.PodStatus{
				PodIP:      "10.1.0.5",
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Second))}},
			},
		}},
		readyIP("redpanda-1", "10.1.0.6"), // node 1, the controller leader
		readyIP("redpanda-2", "10.1.0.7"), // node 2
	}
	podIdentity := &PodIdentityGhostConfig{
		endpoints: func() []string {
			return []string{"redpanda-0.redpanda:9644", "redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644"}
		},
		dial: func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
			switch endpoint {
			case "redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644":
				return rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil) // authority
			case "redpanda-0.redpanda:9644":
				return rpadmin.NewAdminAPI([]string{podSrv.URL}, new(rpadmin.NopAuth), nil) // the drainer's pod
			default:
				t.Errorf("unexpected dial %q", endpoint)
				return nil, fmt.Errorf("unexpected endpoint %q", endpoint)
			}
		},
	}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, podIdentity, testr.New(t)))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "a flat-network drainer superseded by a different node id at its IP must be cleared via the pod-identity rule")
}

// TestClearStuckMaintenanceModeClearsGhostBroker drives the ghost path
// end-to-end (issue #1674): a dead draining broker (node 1) whose replacement
// rejoined as node 3 at the same address, with its pod Ready again — so only
// the ghost rule, not the not-Ready threshold path, can clear it.
func TestClearStuckMaintenanceModeClearsGhostBroker(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-0.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 2, InternalRPCAddress: "redpanda-2.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 3, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			var id int
			if n, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				id = n
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{
		ready("redpanda-0"),
		ready("redpanda-1"), // Ready again, running the successor broker (node 3)
		ready("redpanda-2"),
	}

	clearedBefore := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{1}, disabled, "exactly the ghost broker id superseded by the live successor should have maintenance mode cleared")
	clearedAfter := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))
	assert.Equal(t, float64(1), clearedAfter-clearedBefore, "the ghost-cleared metric should count the clear")
}

// TestClearStuckMaintenanceModeToleratesGhostClearRace: losing the ghost-clear
// race to the chart's postStart hook (a 400 "not in maintenance" on the DELETE)
// is success via the other layer and must not fail the step; a later stuck
// broker in the same pass must still be cleared.
func TestClearStuckMaintenanceModeToleratesGhostClearRace(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-0.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		// Ghost: superseded by node 3 at the same address, but the postStart
		// hook already cleared it — DELETE returns 400.
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 3, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		// Stuck broker, long-down pod: must still be cleared after the race.
		{NodeID: 5, InternalRPCAddress: "redpanda-2.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			id, err := fmtSscanBrokerID(r.URL.Path)
			if err == nil && id == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "node is not in maintenance state", "code": 400})
				return
			}
			mu.Lock()
			disabled = append(disabled, id)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
			}}},
		}}
	}
	notReady := func(name string, dur time.Duration) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-dur)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{
		ready("redpanda-0"),
		ready("redpanda-1"),
		notReady("redpanda-2", 10*time.Minute),
	}

	clearedBefore := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)),
		"losing the ghost-clear race (400 not-in-maintenance) must not fail the reconcile step")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{5}, disabled, "the stuck broker after the raced ghost must still be cleared in the same pass")
	clearedAfter := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))
	assert.Equal(t, float64(0), clearedAfter-clearedBefore, "a ghost cleared by the other layer must not be counted as cleared by the operator")
}

// TestClearStuckMaintenanceModeLowThresholdLeavesHealthyClusterAlone: even at a
// 1s threshold, a healthy cluster (all pods Ready, all brokers alive) is left
// untouched — mid-rolling-restart brokers are protected by the alive gate, not
// the threshold.
func TestClearStuckMaintenanceModeLowThresholdLeavesHealthyClusterAlone(t *testing.T) {
	ctx := t.Context()
	const threshold = time.Second

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-0.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		// Mid-rolling-restart: alive and draining (preStop just ran). The
		// alive gate alone must protect it, no matter how low the threshold.
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			if id, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				mu.Lock()
				disabled = append(disabled, id)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{ready("redpanda-0"), ready("redpanda-1")}

	// With every broker alive there are no unclaimed drainers, so the
	// pod-identity rule must neither render endpoints nor dial any pod.
	podIdentity := &PodIdentityGhostConfig{
		endpoints: func() []string {
			t.Fatal("a healthy cluster must never render per-pod admin endpoints for ghost identity checks")
			return nil
		},
		dial: func(context.Context, string) (*rpadmin.AdminAPI, error) {
			t.Fatal("a healthy cluster must never have its pods dialed for ghost identity checks")
			return nil, nil
		},
	}

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, podIdentity, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "a healthy cluster (all pods Ready, all brokers alive) must be untouched even with a 1s threshold")
}

// TestClearStuckMaintenanceModeGhostAmbiguousPodNameDefers: when a ghost's pod
// name matches pods in more than one member cluster, a broker in one could
// masquerade as another's successor, so the ghost clear must defer.
func TestClearStuckMaintenanceModeGhostAmbiguousPodNameDefers(t *testing.T) {
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-default-0.redpanda", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 1, InternalRPCAddress: "redpanda-default-0.redpanda", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 0})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 0})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			if id, err := fmtSscanBrokerID(r.URL.Path); err == nil {
				mu.Lock()
				disabled = append(disabled, id)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
			}}},
		}}
	}
	// Two member clusters, identically named pods — attribution is ambiguous.
	pods := []*lifecycle.MulticlusterPod{
		ready("redpanda-default-0"),
		ready("redpanda-default-0"),
	}

	clearedBefore := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))

	require.NoError(t, ClearStuckMaintenanceMode(ctx, client, pods, threshold, nil, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "a ghost whose address is cross-cluster ambiguous must NOT be cleared from the broker list alone")
	clearedAfter := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))
	assert.Equal(t, float64(0), clearedAfter-clearedBefore, "no ghost clear should be counted when the evidence is cross-cluster ambiguous")
}

// TestDecideGhostClearByPodIdentity: clear an unclaimed drainer only when the
// pod owning its address self-reports a different, assigned node id. Same id is
// an ordinary restart (hands off); a negative id is no identity yet (defer).
func TestDecideGhostClearByPodIdentity(t *testing.T) {
	cases := []struct {
		name          string
		drainerNodeID int
		podNodeID     int
		want          bool
	}{
		{"pod runs a different id -> superseded, clear", 0, 6, true},
		{"pod runs the drainer's own id -> ordinary restart, no clear", 5, 5, false},
		{"pod id unassigned (-1) -> defer", 5, -1, false},
		{"node id 0 is a valid identity, not a placeholder: same id -> no clear", 0, 0, false},
		{"node id 0 is a valid identity: drainer 5 superseded by pod running 0 -> clear", 5, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decideGhostClearByPodIdentity(tc.drainerNodeID, tc.podNodeID)
			assert.Equal(t, tc.want, got, "reason: %s", reason)
		})
	}
}

// TestUnclaimedDrainersInMaintenance: a candidate is a dead + draining broker
// whose address has no live owner (the complement of ghostBrokersInMaintenance).
// Bare-IP drainers participate here (the pod-identity rule verifies them);
// empty addresses never do.
func TestUnclaimedDrainersInMaintenance(t *testing.T) {
	const addr = "redpanda-0.redpanda.maint.svc.cluster.local."
	dead := func(id int, address string, port int, draining bool) rpadmin.Broker {
		b := rpadmin.Broker{NodeID: id, InternalRPCAddress: address, InternalRPCPort: port, IsAlive: ptr.To(false)}
		if draining {
			b.Maintenance = &rpadmin.MaintenanceStatus{Draining: true}
		}
		return b
	}
	alive := func(id int, address string, port int) rpadmin.Broker {
		return rpadmin.Broker{NodeID: id, InternalRPCAddress: address, InternalRPCPort: port, IsAlive: ptr.To(true)}
	}

	drainerIDs := func(brokers []rpadmin.Broker) []int {
		ids := []int{}
		for _, d := range unclaimedDrainersInMaintenance(brokers) {
			ids = append(ids, d.NodeID)
		}
		return ids
	}

	t.Run("dead draining broker alone at its address is a candidate (leader-restart: successor cannot register)", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(0, addr, 33145, true),
			alive(1, "redpanda-1.redpanda.maint.svc.cluster.local.", 33145),
			alive(2, "redpanda-2.redpanda.maint.svc.cluster.local.", 33145),
		}
		assert.Equal(t, []int{0}, drainerIDs(brokers))
	})
	t.Run("a live owner at the address disqualifies the bucket: that is the registered-successor rule's case", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(0, addr, 33145, true),
			alive(6, addr, 33145),
		}
		assert.Empty(t, drainerIDs(brokers))
	})
	t.Run("dead but not draining is not a candidate: there is no leaked flag to clear", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(0, addr, 33145, false),
		}
		assert.Empty(t, drainerIDs(brokers))
	})
	t.Run("multiple leaked ids at one unowned address are all candidates, in node-id order", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(4, addr, 33145, true),
			dead(1, addr, 33145, true),
		}
		assert.Equal(t, []int{1, 4}, drainerIDs(brokers))
	})
	t.Run("nil is_alive defaults to alive: never a candidate, and it disqualifies its bucket", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			{NodeID: 0, InternalRPCAddress: addr, InternalRPCPort: 33145, Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		}
		assert.Empty(t, drainerIDs(brokers))
	})
	t.Run("bare-IP drainers DO participate: flat-network recovery via the reuse-safe pod-identity rule", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(0, "10.1.0.5", 33145, true),
		}
		assert.Equal(t, []int{0}, drainerIDs(brokers))
	})
	t.Run("empty addresses never participate", func(t *testing.T) {
		brokers := []rpadmin.Broker{
			dead(0, "", 33145, true),
		}
		assert.Empty(t, drainerIDs(brokers))
	})
}

// leaderRestartGhostFixture drives ClearStuckMaintenanceMode against a
// leader-restart wedge (issue #31057): the old leader id (node 0, at
// redpanda-0) is dead and draining with no live owner of its address, while
// nodes 1, 2 are alive. Only the pod-identity rule can resolve it.
type leaderRestartGhostFixture struct {
	// podNodeConfigID is the node id redpanda-0's local /v1/node_config
	// self-reports (the replacement broker's identity).
	podNodeConfigID int
	// omitNodeID leaves node_id out of the /v1/node_config response entirely
	// — a degraded answer that must parse to the -1 sentinel, never to 0.
	omitNodeID bool
	// podAdvertisedHost overrides the internal RPC host the pod's broker
	// self-advertises. Defaults to redpanda-0's own address; a foreign value
	// simulates a misdirected dial answered by some other broker.
	podAdvertisedHost string
	// dialErr, when set, makes the pod dialer fail instead of answering.
	dialErr error
	// endpoints overrides the per-pod admin endpoint list when non-nil.
	endpoints []string
	// noEndpoints makes endpoint rendering resolve to an empty list (the
	// GetAdminAPIEndpoints failure mode).
	noEndpoints bool
	// ghostDeleteStatus, when non-zero, is returned for the ghost's
	// maintenance DELETE instead of 200.
	ghostDeleteStatus int
	// extraPod, when non-empty, adds a second pod with this name (stretch
	// name-collision scenarios).
	extraPod string
}

// run returns the node ids whose maintenance DELETE reached the cluster API and
// the error from ClearStuckMaintenanceMode. The redpanda-0 pod is only briefly
// not-Ready (30s, under the 5m threshold), so any clear is attributable to the
// thresholdless pod-identity rule, never the stuck path.
func (f leaderRestartGhostFixture) run(t *testing.T) ([]int, error) {
	t.Helper()
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-0.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 2, InternalRPCAddress: "redpanda-2.redpanda.maint.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
	}

	var mu sync.Mutex
	var disabled []int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 1})
	})
	mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 1})
	})
	// Serving health lets the authority probe pin a Ready pod rather than fall
	// back to the unpinned client.
	mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": true})
	})
	mux.HandleFunc("/v1/brokers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/maintenance") {
			id, err := fmtSscanBrokerID(r.URL.Path)
			if err == nil && id == 0 && f.ghostDeleteStatus != 0 {
				w.WriteHeader(f.ghostDeleteStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "node is not in maintenance state", "code": f.ghostDeleteStatus})
				return
			}
			if err == nil {
				mu.Lock()
				disabled = append(disabled, id)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// redpanda-0's pod-local admin API: the replacement broker answering
	// /v1/node_config with its own identity and advertised address (the rule
	// uses the address to attribute the answer to the pod).
	advertisedHost := f.podAdvertisedHost
	if advertisedHost == "" {
		advertisedHost = "redpanda-0.redpanda.maint.svc.cluster.local."
	}
	podMux := http.NewServeMux()
	podMux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"advertised_rpc_api": map[string]any{"address": advertisedHost, "port": 33145},
		}
		if !f.omitNodeID {
			resp["node_id"] = f.podNodeConfigID
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	podSrv := httptest.NewServer(podMux)
	defer podSrv.Close()

	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}}
	}
	notReady := func(name string, dur time.Duration) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-dur)),
			}}},
		}}
	}
	pods := []*lifecycle.MulticlusterPod{
		notReady("redpanda-0", 30*time.Second),
		ready("redpanda-1"),
		ready("redpanda-2"),
	}
	if f.extraPod != "" {
		pods = append(pods, notReady(f.extraPod, 30*time.Second))
	}

	endpoints := f.endpoints
	if endpoints == nil {
		endpoints = []string{"redpanda-0.redpanda:9644", "redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644"}
	}
	if f.noEndpoints {
		endpoints = nil
	}
	podIdentity := &PodIdentityGhostConfig{
		endpoints: func() []string { return endpoints },
		dial: func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
			switch endpoint {
			case "redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644":
				// A Ready member pod serves the pass's authoritative broker
				// list + writes (authorityAdmin picks the first Ready pod).
				return rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
			case "redpanda-0.redpanda:9644":
				// The unclaimed drainer's own pod (pod-identity rule).
				if f.dialErr != nil {
					return nil, f.dialErr
				}
				return rpadmin.NewAdminAPI([]string{podSrv.URL}, new(rpadmin.NopAuth), nil)
			default:
				t.Errorf("unexpected dial endpoint %q", endpoint)
				return nil, fmt.Errorf("unexpected endpoint %q", endpoint)
			}
		},
	}

	runErr := ClearStuckMaintenanceMode(ctx, client, pods, threshold, podIdentity, testr.New(t))

	mu.Lock()
	defer mu.Unlock()
	return append([]int{}, disabled...), runErr
}

// TestClearStuckMaintenanceModeClearsLeaderRestartGhost: a ghost with no
// registered successor at its address (invisible to ghostBrokersInMaintenance)
// whose pod self-reports a different node id must be cleared immediately by the
// pod-identity rule, without waiting for the threshold.
func TestClearStuckMaintenanceModeClearsLeaderRestartGhost(t *testing.T) {
	clearedBefore := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))

	disabled, err := leaderRestartGhostFixture{podNodeConfigID: 6}.run(t)
	require.NoError(t, err)
	assert.Equal(t, []int{0}, disabled, "the ghost superseded by the pod's local identity must be cleared, without waiting for any threshold")

	clearedAfter := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))
	assert.Equal(t, float64(1), clearedAfter-clearedBefore, "the pod-identity clear must count as a ghost clear")
}

// TestClearStuckMaintenanceModeLeaderRestartSameIdentityUntouched: a pod
// rebooting the drainer's own node id is an ordinary restart and must not be
// touched, no matter how the broker list looks.
func TestClearStuckMaintenanceModeLeaderRestartSameIdentityUntouched(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{podNodeConfigID: 0}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "a pod running the drainer's own node id is an ordinary restart; its maintenance flag must stay")
}

// TestClearStuckMaintenanceModeLeaderRestartUnassignedIdentityDefers: a pod
// whose broker has no identity yet (node_id -1) is no evidence, so the clear
// must defer.
func TestClearStuckMaintenanceModeLeaderRestartUnassignedIdentityDefers(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{podNodeConfigID: -1}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "an unassigned pod identity is no evidence; the flag must stay until the identity is readable")
}

// TestClearStuckMaintenanceModeLeaderRestartUnreadableIdentityDefers: a pod
// whose admin API cannot be dialed defers the clear without failing the
// reconcile step (the rest of the chain must still run this pass).
func TestClearStuckMaintenanceModeLeaderRestartUnreadableIdentityDefers(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{podNodeConfigID: 6, dialErr: errors.New("connection refused")}.run(t)
	require.NoError(t, err, "an unreachable pod admin API must defer, not abort the reconcile pass")
	assert.Empty(t, disabled)
}

// TestClearStuckMaintenanceModeLeaderRestartAmbiguousEndpointSkips: when the
// drainer's pod name resolves to more than one admin endpoint (StretchCluster
// name collision), reading one broker to clear another would be a guess, so the
// rule skips.
func TestClearStuckMaintenanceModeLeaderRestartAmbiguousEndpointSkips(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID: 6,
		endpoints:       []string{"redpanda-0.redpanda:9644", "redpanda-0.other:9644"},
	}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "an ambiguous admin-endpoint match must never authorize a clear")
}

// TestClearStuckMaintenanceModeLeaderRestartAmbiguousPodSkips: when the
// drainer's pod name matches more than one existing pod, no single pod's
// identity can vouch for the address, so the rule skips.
func TestClearStuckMaintenanceModeLeaderRestartAmbiguousPodSkips(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID: 6,
		extraPod:        "redpanda-0",
	}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "a pod-name collision must never authorize a clear")
}

// TestClearStuckMaintenanceModeLeaderRestartNoEndpointSkips: a pod with no
// resolvable admin endpoint defers rather than guessing an address to dial.
func TestClearStuckMaintenanceModeLeaderRestartNoEndpointSkips(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID: 6,
		endpoints:       []string{"redpanda-1.redpanda:9644"},
	}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled)
}

// TestClearStuckMaintenanceModeLeaderRestartWrongResponderDefers: the dial
// targets a DNS name and nothing guarantees who answers, so an answer whose
// self-advertised address does not belong to the pod must never be used as
// supersession evidence.
func TestClearStuckMaintenanceModeLeaderRestartWrongResponderDefers(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID:   6,
		podAdvertisedHost: "other-broker-0.elsewhere.svc.cluster.local.",
	}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "an answer not attributable to the pod must never clear the drainer")
}

// TestClearStuckMaintenanceModeLeaderRestartMissingNodeIDDefers: a node_config
// response with no node_id must decode to the sentinel and defer, never
// fabricate node id 0 from the zero value.
func TestClearStuckMaintenanceModeLeaderRestartMissingNodeIDDefers(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{omitNodeID: true}.run(t)
	require.NoError(t, err)
	assert.Empty(t, disabled, "a node_config answer without node_id is no evidence; the flag must stay")
}

// TestClearStuckMaintenanceModeLeaderRestartEmptyEndpointsSkips: an empty
// endpoint list (GetAdminAPIEndpoints surfacing a conversion error) must skip
// without dialing anything or failing the reconcile step.
func TestClearStuckMaintenanceModeLeaderRestartEmptyEndpointsSkips(t *testing.T) {
	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID: 6,
		noEndpoints:     true,
		dialErr:         errors.New("dial must never be attempted without endpoints"),
	}.run(t)
	require.NoError(t, err, "an empty endpoint list must not fail the reconcile step")
	assert.Empty(t, disabled)
}

// TestResponderMatchesPod: a DNS responder must advertise the pod's name as its
// first DNS label, a flat-network responder must advertise the pod's current
// IP, and anything unverifiable is a mismatch.
func TestResponderMatchesPod(t *testing.T) {
	cases := []struct {
		name           string
		advertisedHost string
		podName        string
		podIP          string
		want           bool
	}{
		{"pod's own FQDN matches", "redpanda-0.redpanda.ns.svc.cluster.local.", "redpanda-0", "10.0.0.5", true},
		{"host:port form is normalized", "redpanda-0.redpanda.ns.svc.cluster.local:33145", "redpanda-0", "", true},
		// First-label semantics: matching on the pod name (not the full host)
		// accepts legitimate StretchCluster forms (per-pod Service vs MCS
		// clusterset); cross-cluster same-name collisions are refused earlier by
		// the ambiguous pod/endpoint guards.
		{"same pod name under a different domain matches (first-label semantics)", "redpanda-0.other.ns2.svc.cluster.local.", "redpanda-0", "", true},
		{"different pod name mismatches", "redpanda-1.redpanda.ns.svc.cluster.local.", "redpanda-0", "", false},
		{"empty advertised host is unverifiable", "", "redpanda-0", "10.0.0.5", false},
		{"bare IP equal to pod IP matches (flat-network)", "10.0.0.5", "redpanda-0", "10.0.0.5", true},
		{"bare IP different from pod IP mismatches", "10.0.0.6", "redpanda-0", "10.0.0.5", false},
		{"bare IP with empty pod IP is unverifiable", "10.0.0.5", "redpanda-0", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, why := responderMatchesPod(tc.advertisedHost, tc.podName, tc.podIP)
			assert.Equal(t, tc.want, got, "reason: %s", why)
		})
	}
}

// TestParseNodeConfigIdentity: a missing or non-numeric node_id decodes to the
// -1 sentinel (node id 0 must never be fabricated from an absent field); a
// missing or malformed advertised_rpc_api decodes to an empty host.
func TestParseNodeConfigIdentity(t *testing.T) {
	cases := []struct {
		name     string
		raw      rpadmin.RawNodeConfig
		wantID   int
		wantHost string
	}{
		{"full response", rpadmin.RawNodeConfig{"node_id": float64(6), "advertised_rpc_api": map[string]any{"address": "redpanda-0.redpanda", "port": float64(33145)}}, 6, "redpanda-0.redpanda"},
		{"node id zero is preserved", rpadmin.RawNodeConfig{"node_id": float64(0)}, 0, ""},
		{"missing node_id is -1, not 0", rpadmin.RawNodeConfig{"advertised_rpc_api": map[string]any{"address": "redpanda-0.redpanda"}}, -1, "redpanda-0.redpanda"},
		{"non-numeric node_id is -1", rpadmin.RawNodeConfig{"node_id": "6"}, -1, ""},
		{"malformed advertised_rpc_api yields empty host", rpadmin.RawNodeConfig{"node_id": float64(6), "advertised_rpc_api": "redpanda-0"}, 6, ""},
		{"empty response", rpadmin.RawNodeConfig{}, -1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, host := parseNodeConfigIdentity(tc.raw)
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantHost, host)
		})
	}
}

// TestClearStuckMaintenanceModeLeaderRestartToleratesClearRace: on the
// pod-identity path, losing the postStart-hook race (400 not-in-maintenance on
// the DELETE) is success via the other layer and must not fail the step.
func TestClearStuckMaintenanceModeLeaderRestartToleratesClearRace(t *testing.T) {
	clearedBefore := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))

	disabled, err := leaderRestartGhostFixture{
		podNodeConfigID:   6,
		ghostDeleteStatus: http.StatusBadRequest,
	}.run(t)
	require.NoError(t, err, "losing the clear race (400 not-in-maintenance) must not fail the reconcile step")
	assert.Empty(t, disabled)

	clearedAfter := testutil.ToFloat64(observability.MaintenanceModeGhostCleared.WithLabelValues(""))
	assert.Equal(t, float64(0), clearedAfter-clearedBefore, "a ghost cleared by the other layer must not be counted as cleared by the operator")
}

// fmtSscanBrokerID extracts the broker id from a "/v1/brokers/{id}/maintenance"
// path.
func fmtSscanBrokerID(path string) (int, error) {
	var id int
	_, err := fmt.Sscanf(path, "/v1/brokers/%d/maintenance", &id)
	return id, err
}

// reconcilerStepName returns the qualified runtime name of a reconciler-chain
// step's bound method value (e.g.
// ".../redpanda.(*RedpandaReconciler).reconcileMaintenanceMode-fm"). Method
// values are not comparable with == across evaluations, so tests identify a
// step by this stable name rather than by pointer identity.
func reconcilerStepName(fn any) string {
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// indexOfReconcilerStep returns the position of the chain step whose name
// contains "methodName", or -1 if absent.
func indexOfReconcilerStep(stepNames []string, methodName string) int {
	for i, name := range stepNames {
		if strings.Contains(name, "."+methodName+"-fm") {
			return i
		}
	}
	return -1
}

// TestMulticlusterReconcilerRunsMaintenanceModeBeforeDecommission:
// reconcileMaintenanceMode must precede reconcileDecommission, or a broker
// stuck in maintenance can never be unblocked — reconcileDecommission requeues
// and aborts the chain while a decommission it started is not yet Finished, and
// the partition balancer refuses to drain a maintenance-mode node. (The OSS
// RedpandaReconciler's chain is pinned the same way in its own package.)
func TestMulticlusterReconcilerRunsMaintenanceModeBeforeDecommission(t *testing.T) {
	r := &MulticlusterReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	maintenanceIdx := indexOfReconcilerStep(names, "reconcileMaintenanceMode")
	decommissionIdx := indexOfReconcilerStep(names, "reconcileDecommission")
	require.GreaterOrEqual(t, maintenanceIdx, 0, "reconcileMaintenanceMode not found in chain: %v", names)
	require.GreaterOrEqual(t, decommissionIdx, 0, "reconcileDecommission not found in chain: %v", names)
	assert.Less(t, maintenanceIdx, decommissionIdx,
		"reconcileMaintenanceMode must run before reconcileDecommission, or a broker stuck in maintenance mode can never be unblocked")
}

// TestMulticlusterReconcilerRunsStaleDiskWipeBeforeDecommission:
// reconcileStaleDiskWipe must precede reconcileDecommission, or a bad_rejoin
// broker's stale disk can never be wiped — reconcileDecommission requeues and
// aborts the chain whenever HasRecentlyReplacedPods() is true, which a
// bad_rejoin's stuck-not-Ready pod always is (K8S-843).
func TestMulticlusterReconcilerRunsStaleDiskWipeBeforeDecommission(t *testing.T) {
	r := &MulticlusterReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	staleDiskWipeIdx := indexOfReconcilerStep(names, "reconcileStaleDiskWipe")
	decommissionIdx := indexOfReconcilerStep(names, "reconcileDecommission")
	require.GreaterOrEqual(t, staleDiskWipeIdx, 0, "reconcileStaleDiskWipe not found in chain: %v", names)
	require.GreaterOrEqual(t, decommissionIdx, 0, "reconcileDecommission not found in chain: %v", names)
	assert.Less(t, staleDiskWipeIdx, decommissionIdx,
		"reconcileStaleDiskWipe must run before reconcileDecommission, or a bad_rejoin broker's stale disk can never be wiped (reconcileDecommission requeues on not-ready recently-replaced pods and aborts the chain)")
}

// TestMulticlusterReconcilerRunsMaintenanceModeBeforeStaleDiskWipe: the
// non-destructive maintenance clear (removes a leaked flag) must be attempted
// before the destructive stale-disk wipe (deletes storage and the pod). (The
// OSS RedpandaReconciler's chain is pinned the same way in its own package.)
func TestMulticlusterReconcilerRunsMaintenanceModeBeforeStaleDiskWipe(t *testing.T) {
	r := &MulticlusterReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	maintenanceIdx := indexOfReconcilerStep(names, "reconcileMaintenanceMode")
	staleDiskWipeIdx := indexOfReconcilerStep(names, "reconcileStaleDiskWipe")
	require.GreaterOrEqual(t, maintenanceIdx, 0, "reconcileMaintenanceMode not found in chain: %v", names)
	require.GreaterOrEqual(t, staleDiskWipeIdx, 0, "reconcileStaleDiskWipe not found in chain: %v", names)
	assert.Less(t, maintenanceIdx, staleDiskWipeIdx,
		"the non-destructive maintenance clear must be attempted before the destructive stale-disk wipe")
}
