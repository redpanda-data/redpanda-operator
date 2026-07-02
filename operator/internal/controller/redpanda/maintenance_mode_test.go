// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
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

	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
)

// TestPodNotReadyFor pins the "down duration" derived from the pod's Ready
// condition transition time — the restart-surviving signal used to gate the
// maintenance-mode clear behind a sustained-down threshold.
func TestPodNotReadyFor(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	pod := func(status corev1.ConditionStatus, since time.Time) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.NewTime(since),
		}}}}
	}

	t.Run("ready pod is not counted", func(t *testing.T) {
		_, notReady := podNotReadyFor(pod(corev1.ConditionTrue, now.Add(-time.Hour)), now)
		assert.False(t, notReady)
	})
	t.Run("not-ready pod returns duration since transition", func(t *testing.T) {
		d, notReady := podNotReadyFor(pod(corev1.ConditionFalse, now.Add(-8*time.Minute)), now)
		assert.True(t, notReady)
		assert.Equal(t, 8*time.Minute, d)
	})
	t.Run("freshly created pod with no Ready condition is not-ready for ~zero duration", func(t *testing.T) {
		freshPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now)}}
		d, notReady := podNotReadyFor(freshPod, now)
		assert.True(t, notReady)
		assert.Equal(t, time.Duration(0), d)
	})
	t.Run("pod stuck Pending with no Ready condition for a long time is not-ready for that full duration", func(t *testing.T) {
		stuckPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour))},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
			}}},
		}
		d, notReady := podNotReadyFor(stuckPod, now)
		assert.True(t, notReady)
		assert.GreaterOrEqual(t, d, 1*time.Hour, "expected notReadyFor to reflect ~2h since pod creation, not be pinned at 0")
	})
}

// TestDecideClearMaintenance pins the guard: only clear maintenance when the
// broker is in maintenance (draining) AND reported not-alive AND its pod has
// been not-Ready at least the threshold.
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

// TestBrokersByPodName maps a broker's advertised RPC address to the pod name
// (first DNS label), handling both "host" and "host:port" forms, so a
// persistently-down broker's pod can be located from the cluster broker list.
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

// TestBrokersByPodNameCollision pins the safety behavior for a StretchCluster
// where two member clusters have identically-named BrokerPools: since a
// StatefulSet/pod name has no member-cluster component, both brokers report
// the same pod-name key. brokersByPodName must bucket both under that key
// (rather than the last-write-wins overwrite the map used to do), so the
// caller can detect the ambiguity instead of silently pairing one broker's
// liveness state with another broker's pod.
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

// TestBrokersByPodNameIgnoresEmptyAddress pins that a broker reporting an
// empty InternalRPCAddress (malformed/unexpected admin API response) is never
// indexed. Without this, it would be keyed under "", which is exactly the key
// clearStuckMaintenanceMode's PodIP fallback produces for an unscheduled pod
// with no PodIP — silently pairing an unrelated broker with an unrelated pod.
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

// TestClearStuckMaintenanceModeIntegration drives clearStuckMaintenanceMode
// end-to-end against a real rpadmin client backed by an httptest server that
// emulates the cluster admin API. It asserts that exactly the broker which is
// (a) in maintenance, (b) not-alive, and (c) whose pod has been not-Ready past
// the threshold has its maintenance mode cleared via
// DELETE /v1/brokers/{id}/maintenance — and that brokers failing any single
// gate are left untouched.
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

	require.NoError(t, clearStuckMaintenanceMode(ctx, client, pods, threshold, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "only the in-maintenance, not-alive, long-down broker should be cleared")
}

// TestClearStuckMaintenanceModeClearsPendingPodWithNoReadyCondition is the
// regression test for a pod stuck Pending before it was ever scheduled (e.g.
// its node was cordoned or lost): such a pod has no PodReady condition at all,
// only a PodScheduled=False condition. Before the fix, podNotReadyFor's
// no-Ready-condition fallback was pinned at a fixed zero duration, so
// notReadyFor never reached the threshold no matter how long the pod had
// actually been stuck — exactly the scenario this reconciler exists to
// unblock. The broker here independently satisfies every other clear gate (in
// maintenance, not-alive); only the pod's not-ready duration was ever in
// question.
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

	require.NoError(t, clearStuckMaintenanceMode(ctx, client, pods, threshold, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{0}, disabled, "broker stuck 2h in a never-scheduled Pending pod (no PodReady condition) should have maintenance mode cleared")
}

// TestClearStuckMaintenanceModeSkipsAmbiguousPodName is the regression test for
// the StretchCluster pod-name collision: a StatefulSet/pod name has no
// member-cluster component, so two member clusters with an identically-named
// BrokerPool produce pods with the same name, and both member clusters' broker
// records are reported by the (shared) admin API under that same
// InternalRPCAddress. Both node 0 and node 9 independently satisfy every clear
// gate (in maintenance, not-alive, pod not-Ready past threshold), but because
// the pod name "redpanda-default-0" maps to both of them, clearing either would
// be a guess. Before the fix, the last-write-wins map silently resolved every
// lookup to whichever broker happened to be inserted last, clearing that one
// broker's maintenance mode redundantly while never clearing the other. The
// fix must refuse to act on either.
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

	require.NoError(t, clearStuckMaintenanceMode(ctx, client, pods, threshold, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "an ambiguous pod-name match must not clear maintenance mode on either broker")
	skippedAfter := testutil.ToFloat64(observability.MaintenanceModeClearSkippedAmbiguous.WithLabelValues(""))
	assert.Equal(t, float64(2), skippedAfter-skippedBefore, "both ambiguous pods should increment the skipped-ambiguous metric")
}

// TestClearStuckMaintenanceModeIgnoresEmptyPodIP is the regression test for a
// pod with no PodIP (unscheduled/Pending) never being matched, via the empty
// string, to a broker reporting an empty InternalRPCAddress (a malformed or
// unexpected admin API response). Both node 0 (empty address, otherwise
// eligible) and the Pending pod (empty name-key match, no IP) are individually
// unresolvable; before the fix they could pair through the shared "" key.
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

	require.NoError(t, clearStuckMaintenanceMode(ctx, client, pods, threshold, testr.New(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, disabled, "a Pending pod with no PodIP must not be paired with a broker reporting an empty address")
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
// ".../redpanda.(*RedpandaReconciler).reconcileMaintenanceMode-fm"). Go
// re-creates a method value's closure per evaluation, so two separately
// obtained values for the same method are not comparable with ==; the
// underlying function's name is stable and lets tests identify a step
// without relying on pointer identity.
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

// TestRedpandaReconcilerRunsMaintenanceModeBeforeDecommission is the
// regression test for the reconciler-ordering defect flagged by adversarial
// review: reconcileDecommission requeues — aborting the rest of the chain for
// that reconcile pass (see the loop in Reconcile) — for as long as a
// decommission it started is not yet Finished. That is exactly the state a
// broker stuck in maintenance mode sits in forever, since Redpanda's
// partition balancer refuses to move data off a maintenance-mode node. If
// reconcileMaintenanceMode were ordered after reconcileDecommission in the
// chain, the clear meant to unblock that exact deadlock would never run.
func TestRedpandaReconcilerRunsMaintenanceModeBeforeDecommission(t *testing.T) {
	r := &RedpandaReconciler{}
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

// TestMulticlusterReconcilerRunsMaintenanceModeBeforeDecommission is the
// StretchCluster counterpart of
// TestRedpandaReconcilerRunsMaintenanceModeBeforeDecommission — see its
// doc comment for the failure mode this pins.
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
