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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestIdentityCollision pins Andrew's two-request collision check: compare a
// sick broker's self (node_id, uuid) against the cluster-authoritative
// node_id->uuid map. A collision means the broker's on-disk identity was
// decommissioned (node_id gone) or superseded (node_id now maps to a different
// uuid), so its disk must be wiped before it can rejoin.
func TestIdentityCollision(t *testing.T) {
	cluster := map[int]string{
		0: "uuid-aaa",
		1: "uuid-bbb",
		2: "uuid-ccc",
	}

	cases := []struct {
		name          string
		clusterUUIDs  map[int]string
		selfNodeID    int
		selfUUID      string
		wantCollision bool
	}{
		{
			name:          "identity matches cluster - no collision",
			clusterUUIDs:  cluster,
			selfNodeID:    1,
			selfUUID:      "uuid-bbb",
			wantCollision: false,
		},
		{
			name:          "node_id absent from cluster - decommissioned, collision",
			clusterUUIDs:  cluster,
			selfNodeID:    7,
			selfUUID:      "uuid-zzz",
			wantCollision: true,
		},
		{
			name:          "node_id present but different uuid - superseded, collision",
			clusterUUIDs:  cluster,
			selfNodeID:    1,
			selfUUID:      "uuid-stale",
			wantCollision: true,
		},
		{
			name:          "empty cluster map - cannot confirm, no collision",
			clusterUUIDs:  map[int]string{},
			selfNodeID:    1,
			selfUUID:      "uuid-bbb",
			wantCollision: false,
		},
		{
			name:          "empty self uuid - cannot confirm, no collision",
			clusterUUIDs:  cluster,
			selfNodeID:    1,
			selfUUID:      "",
			wantCollision: false,
		},
		{
			// A current member whose cluster-side uuid is unknown (no
			// /v1/broker_uuids entry) must NOT be treated as a collision:
			// otherwise a live broker's disk could be wiped. Regression guard
			// for the false-positive where clusterUUID=="" fell into the
			// "superseded" branch.
			name:          "member present but cluster uuid unknown - cannot confirm, no collision",
			clusterUUIDs:  map[int]string{0: "uuid-aaa", 1: ""},
			selfNodeID:    1,
			selfUUID:      "uuid-bbb",
			wantCollision: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := identityCollision(tc.clusterUUIDs, tc.selfNodeID, tc.selfUUID)
			assert.Equal(t, tc.wantCollision, got, "reason: %s", reason)
		})
	}
}

// TestDecidePVCUnbind pins the guarded decision: only destroy a disk when a
// collision is confirmed, the pod has been not-ready past the threshold, and
// the cluster is otherwise healthy with no down nodes.
func TestDecidePVCUnbind(t *testing.T) {
	const threshold = 5 * time.Minute

	cases := []struct {
		name           string
		collision      bool
		notReadyFor    time.Duration
		clusterHealthy bool
		downNodes      int
		wantUnbind     bool
	}{
		{
			name:           "collision + past threshold + healthy + no down nodes -> unbind",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantUnbind:     true,
		},
		{
			name:           "no collision -> no unbind",
			collision:      false,
			notReadyFor:    30 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantUnbind:     false,
		},
		{
			name:           "collision but under threshold -> no unbind",
			collision:      true,
			notReadyFor:    2 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantUnbind:     false,
		},
		{
			name:           "collision but cluster unhealthy -> no unbind",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: false,
			downNodes:      0,
			wantUnbind:     false,
		},
		{
			name:           "collision but down nodes present -> no unbind",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: true,
			downNodes:      1,
			wantUnbind:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decidePVCUnbind(tc.collision, tc.notReadyFor, threshold, tc.clusterHealthy, tc.downNodes)
			assert.Equal(t, tc.wantUnbind, got, "reason: %s", reason)
		})
	}
}

// TestBrokerSelfIdentity reads a single broker's own (node_id, uuid) by
// combining /v1/node_config (self node_id) with the broker's local
// /v1/broker_uuids view (self uuid).
func TestBrokerSelfIdentity(t *testing.T) {
	ctx := t.Context()

	newBroker := func(t *testing.T, nodeID int, uuids []rpadmin.BrokerUuids) *rpadmin.AdminAPI {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": nodeID})
		})
		mux.HandleFunc("/v1/broker_uuids", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(uuids)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		t.Cleanup(client.Close)
		return client
	}

	t.Run("reads self node_id and matching uuid", func(t *testing.T) {
		client := newBroker(t, 2, []rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
			{NodeID: 2, UUID: "uuid-ccc"},
		})
		nodeID, uuid, err := brokerSelfIdentity(ctx, client)
		require.NoError(t, err)
		assert.Equal(t, 2, nodeID)
		assert.Equal(t, "uuid-ccc", uuid)
	})

	t.Run("self node_id absent from broker_uuids yields empty uuid", func(t *testing.T) {
		client := newBroker(t, 9, []rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
		})
		nodeID, uuid, err := brokerSelfIdentity(ctx, client)
		require.NoError(t, err)
		assert.Equal(t, 9, nodeID)
		assert.Equal(t, "", uuid)
	})
}

// TestClusterMemberUUIDs builds the node_id->uuid map of CURRENT members. This
// must be driven by /v1/brokers (membership), not /v1/broker_uuids alone:
// observed live (K8S-843), /v1/broker_uuids RETAINS a decommissioned node's
// node_id->uuid entry indefinitely, so a decommissioned-broker bad_rejoin would
// be missed if we trusted broker_uuids for presence. A node present in
// broker_uuids but absent from the broker list has been decommissioned and must
// be excluded.
func TestClusterMemberUUIDs(t *testing.T) {
	ctx := t.Context()
	mux := http.NewServeMux()
	// Membership: nodes 0 and 2 (node 1 was decommissioned and is gone).
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]rpadmin.Broker{
			{NodeID: 0}, {NodeID: 2},
		})
	})
	// broker_uuids still lists the decommissioned node 1.
	mux.HandleFunc("/v1/broker_uuids", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
			{NodeID: 1, UUID: "uuid-bbb"},
			{NodeID: 2, UUID: "uuid-ccc"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	got, err := clusterMemberUUIDs(ctx, client)
	require.NoError(t, err)
	// node 1 (decommissioned) must be excluded even though broker_uuids lists it.
	assert.Equal(t, map[int]string{0: "uuid-aaa", 2: "uuid-ccc"}, got)
}

// TestPodAdminEndpoint pins how a pod is matched to its admin-API endpoint. The
// per-pod Service name equals the pod name, so the endpoint's first DNS label
// identifies the pod (endpoints look like "<podName>.<ns>:<port>").
func TestPodAdminEndpoint(t *testing.T) {
	endpoints := []string{
		"redpanda-rp-east-0.redpanda:9644",
		"redpanda-rp-east-1.redpanda:9644",
		"redpanda-rp-west-0.redpanda:9644",
	}

	t.Run("matches by pod name", func(t *testing.T) {
		endpoint, ambiguous := podAdminEndpoint(endpoints, "redpanda-rp-west-0")
		assert.False(t, ambiguous)
		assert.Equal(t, "redpanda-rp-west-0.redpanda:9644", endpoint)
	})

	t.Run("no match returns empty", func(t *testing.T) {
		endpoint, ambiguous := podAdminEndpoint(endpoints, "redpanda-rp-eu-0")
		assert.False(t, ambiguous)
		assert.Equal(t, "", endpoint)
	})

	// Data-loss guard: a StretchCluster with identically-named BrokerPools
	// across member clusters yields cluster-unqualified endpoints that collide
	// on the same pod name. The endpoint must be reported ambiguous so the
	// unbinder defers instead of reading one broker's identity and wiping
	// another broker's disk.
	t.Run("duplicate pod name across clusters is ambiguous", func(t *testing.T) {
		dup := []string{
			"redpanda-0.redpanda:9644", // member cluster A
			"redpanda-0.redpanda:9644", // member cluster B, identical pool name
		}
		endpoint, ambiguous := podAdminEndpoint(dup, "redpanda-0")
		assert.True(t, ambiguous)
		assert.Equal(t, "", endpoint)
	})
}

// TestPVCUnbindThresholdAndDisable pins the threshold-tuning and kill-switch
// semantics behind the --pvc-unbind-not-ready-threshold flag: 0 uses the
// built-in default, a positive value tunes the not-ready threshold, and any
// negative value disables the destructive PVC unbinder entirely. The disable is
// the operator-facing off switch advertised in the flag help and changelog, so
// a regression flipping the comparison (e.g. to <= 0, silently disabling the
// default) must fail a test.
func TestPVCUnbindThresholdAndDisable(t *testing.T) {
	cases := []struct {
		name          string
		threshold     time.Duration
		wantDisabled  bool
		wantThreshold time.Duration // only checked when not disabled
	}{
		{name: "zero uses built-in default", threshold: 0, wantDisabled: false, wantThreshold: defaultPVCUnbindNotReadyThreshold},
		{name: "positive tunes the threshold", threshold: 12 * time.Minute, wantDisabled: false, wantThreshold: 12 * time.Minute},
		{name: "negative disables entirely", threshold: -1 * time.Second, wantDisabled: true},
		{name: "any negative disables (parsed -1ns)", threshold: -1, wantDisabled: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &MulticlusterReconciler{PVCUnbindNotReadyThreshold: tc.threshold}
			assert.Equal(t, tc.wantDisabled, r.pvcUnbindDisabled())
			if !tc.wantDisabled {
				assert.Equal(t, tc.wantThreshold, r.pvcUnbindThreshold())
			}
		})
	}
}

// TestPVCUnbindForPendingPodWithNoReadyCondition is the PVC-unbinder counterpart
// of TestClearStuckMaintenanceModeClearsPendingPodWithNoReadyCondition. It runs
// the same sequence of per-pod helpers reconcilePVCUnbinder uses — podNotReadyFor
// -> identityCollision -> decidePVCUnbind — on a REAL broker pod that is stuck
// Pending (its node was cordoned/lost) and therefore carries only a
// PodScheduled=False condition, no PodReady condition at all. This is the
// bad_rejoin recovery scenario: the on-disk node_id was decommissioned (absent
// from the cluster's member-uuid map) so the broker crashloops and its pod never
// becomes Ready.
//
// It pins that the podNotReadyFor fix (fall back to now-CreationTimestamp for a
// pod with no PodReady condition) makes decidePVCUnbind's not-ready-duration gate
// satisfiable for such a pod: before the fix podNotReadyFor pinned the duration at
// 0, so 0 < threshold held forever and the disk was never unbound — the unbinder
// could never recover the very bad_rejoin it exists for. The guard sub-cases pin
// that the real fallback duration still interacts correctly with decidePVCUnbind's
// other guards (collision, cluster health, down-nodes), so a long-Pending pod is
// NOT unbound unless every guard holds.
func TestPVCUnbindForPendingPodWithNoReadyCondition(t *testing.T) {
	const threshold = 5 * time.Minute
	now := time.Now()

	// A real never-scheduled Pending pod: only PodScheduled=False, no PodReady
	// condition, stuck for 2h (well past the threshold).
	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "redpanda-rp-east-0",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
		}}},
	}

	// The pod's on-disk node_id 0 was decommissioned: it is absent from the
	// cluster's authoritative member-uuid map, so identityCollision confirms a
	// collision (disk identity retired). Nodes 2,3,4 are the surviving members.
	clusterUUIDs := map[int]string{2: "uuid-2", 3: "uuid-3", 4: "uuid-4"}
	const selfNodeID = 0
	const selfUUID = "uuid-0-retired"

	notReadyFor, notReady := podNotReadyFor(pendingPod, now)
	require.True(t, notReady, "a Pending pod with no PodReady condition must be reported not-ready")
	require.GreaterOrEqual(t, notReadyFor, threshold,
		"podNotReadyFor must measure a never-scheduled Pending pod's not-ready duration from its CreationTimestamp, not pin it at 0")

	collision, collisionReason := identityCollision(clusterUUIDs, selfNodeID, selfUUID)
	require.True(t, collision, "decommissioned node_id absent from the cluster must be a collision: %s", collisionReason)

	t.Run("collision + past threshold + healthy + no down nodes -> unbind", func(t *testing.T) {
		unbind, reason := decidePVCUnbind(collision, notReadyFor, threshold, true, 0)
		assert.True(t, unbind, "a bad_rejoin broker stuck 2h in a never-scheduled Pending pod must be unbound: %s", reason)
	})

	t.Run("no collision -> no unbind", func(t *testing.T) {
		noCollision, _ := identityCollision(map[int]string{0: selfUUID, 2: "uuid-2"}, selfNodeID, selfUUID)
		require.False(t, noCollision)
		unbind, _ := decidePVCUnbind(noCollision, notReadyFor, threshold, true, 0)
		assert.False(t, unbind, "a legitimate member's disk must never be unbound even when its pod is long not-ready")
	})

	t.Run("cluster unhealthy -> no unbind", func(t *testing.T) {
		unbind, _ := decidePVCUnbind(collision, notReadyFor, threshold, false, 0)
		assert.False(t, unbind, "must defer the destructive unbind while the cluster is unhealthy")
	})

	t.Run("nodes down -> no unbind", func(t *testing.T) {
		unbind, _ := decidePVCUnbind(collision, notReadyFor, threshold, true, 1)
		assert.False(t, unbind, "must defer the destructive unbind while any node is down (possible partition)")
	})
}
