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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := identityCollision(tc.clusterUUIDs, tc.selfNodeID, tc.selfUUID)
			assert.Equal(t, tc.wantCollision, got, "reason: %s", reason)
		})
	}
}

// TestPodNotReadyFor checks the not-ready duration derived from the pod's Ready
// condition transition time, used to gate the destructive PVC unbind behind a
// sustained-unreadiness threshold.
func TestPodNotReadyFor(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	readyPod := func(status corev1.ConditionStatus, since time.Time) *corev1.Pod {
		return &corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{{
					Type:               corev1.PodReady,
					Status:             status,
					LastTransitionTime: metav1.NewTime(since),
				}},
			},
		}
	}

	t.Run("ready pod is not counted", func(t *testing.T) {
		_, notReady := podNotReadyFor(readyPod(corev1.ConditionTrue, now.Add(-10*time.Minute)), now)
		assert.False(t, notReady)
	})

	t.Run("not-ready pod returns duration since transition", func(t *testing.T) {
		dur, notReady := podNotReadyFor(readyPod(corev1.ConditionFalse, now.Add(-7*time.Minute)), now)
		assert.True(t, notReady)
		assert.Equal(t, 7*time.Minute, dur)
	})

	t.Run("pod with no Ready condition is treated as not-ready for zero duration", func(t *testing.T) {
		_, notReady := podNotReadyFor(&corev1.Pod{}, now)
		assert.True(t, notReady)
	})
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

// TestClusterBrokerUUIDs converts the cluster's /v1/broker_uuids list into the
// node_id->uuid map the collision check consumes.
func TestClusterBrokerUUIDs(t *testing.T) {
	ctx := t.Context()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/broker_uuids", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
			{NodeID: 1, UUID: "uuid-bbb"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer client.Close()

	got, err := clusterBrokerUUIDs(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{0: "uuid-aaa", 1: "uuid-bbb"}, got)
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
		assert.Equal(t, "redpanda-rp-west-0.redpanda:9644", podAdminEndpoint(endpoints, "redpanda-rp-west-0"))
	})

	t.Run("no match returns empty", func(t *testing.T) {
		assert.Equal(t, "", podAdminEndpoint(endpoints, "redpanda-rp-eu-0"))
	})
}
