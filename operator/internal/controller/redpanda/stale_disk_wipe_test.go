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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
)

// TestIdentityCollision: a broker's (node_id, uuid) collides when its node_id
// is absent from the cluster's node_id->uuid map (decommissioned) or maps to a
// different uuid (superseded); an unconfirmable identity is never a collision.
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
			// A member whose cluster-side uuid is unknown (empty) must not
			// count as a collision, or a live broker's disk could be wiped.
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

// TestDecideStaleDiskWipe: a disk is wiped only when a collision is confirmed,
// the pod has been not-ready past the threshold, and the cluster is healthy
// with no down nodes.
func TestDecideStaleDiskWipe(t *testing.T) {
	const threshold = 5 * time.Minute

	cases := []struct {
		name           string
		collision      bool
		notReadyFor    time.Duration
		clusterHealthy bool
		downNodes      int
		wantWipe       bool
	}{
		{
			name:           "collision + past threshold + healthy + no down nodes -> wipe",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantWipe:       true,
		},
		{
			name:           "no collision -> no wipe",
			collision:      false,
			notReadyFor:    30 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantWipe:       false,
		},
		{
			name:           "collision but under threshold -> no wipe",
			collision:      true,
			notReadyFor:    2 * time.Minute,
			clusterHealthy: true,
			downNodes:      0,
			wantWipe:       false,
		},
		{
			name:           "collision but cluster unhealthy -> no wipe",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: false,
			downNodes:      0,
			wantWipe:       false,
		},
		{
			name:           "collision but down nodes present -> no wipe",
			collision:      true,
			notReadyFor:    6 * time.Minute,
			clusterHealthy: true,
			downNodes:      1,
			wantWipe:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decideStaleDiskWipe(tc.collision, tc.notReadyFor, threshold, tc.clusterHealthy, tc.downNodes)
			assert.Equal(t, tc.wantWipe, got, "reason: %s", reason)
		})
	}
}

// TestBrokerSelfIdentity: a broker's own (node_id, uuid, advertised RPC host)
// is read by combining raw /v1/node_config with its local /v1/broker_uuids.
func TestBrokerSelfIdentity(t *testing.T) {
	ctx := t.Context()

	newBroker := func(t *testing.T, nodeConfig map[string]any, uuids []rpadmin.BrokerUuids) *rpadmin.AdminAPI {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(nodeConfig)
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

	t.Run("reads self node_id, matching uuid, and advertised host", func(t *testing.T) {
		client := newBroker(t, map[string]any{
			"node_id":            2,
			"advertised_rpc_api": map[string]any{"address": "redpanda-2.redpanda", "port": 33145},
		}, []rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
			{NodeID: 2, UUID: "uuid-ccc"},
		})
		nodeID, uuid, advertisedHost, err := brokerSelfIdentity(ctx, client)
		require.NoError(t, err)
		assert.Equal(t, 2, nodeID)
		assert.Equal(t, "uuid-ccc", uuid)
		assert.Equal(t, "redpanda-2.redpanda", advertisedHost)
	})

	t.Run("self node_id absent from broker_uuids yields empty uuid", func(t *testing.T) {
		client := newBroker(t, map[string]any{"node_id": 9}, []rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
		})
		nodeID, uuid, _, err := brokerSelfIdentity(ctx, client)
		require.NoError(t, err)
		assert.Equal(t, 9, nodeID)
		assert.Equal(t, "", uuid)
	})

	t.Run("missing node_id decodes to the -1 sentinel, never to node 0", func(t *testing.T) {
		client := newBroker(t, map[string]any{}, []rpadmin.BrokerUuids{
			{NodeID: 0, UUID: "uuid-aaa"},
		})
		nodeID, uuid, _, err := brokerSelfIdentity(ctx, client)
		require.NoError(t, err)
		assert.Equal(t, -1, nodeID, "an answer without node_id must not be attributed to node 0")
		assert.Equal(t, "", uuid, "no identity, no uuid: the collision stays unconfirmable")
	})
}

// testUUID returns a syntactically valid dashed-hex uuid for broker n (the
// log-evidence parser validates the shape, so test uuids must be real-shaped).
func testUUID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

// TestClusterMemberUUIDs: the member map must come from /v1/brokers, not
// /v1/broker_uuids, which retains a decommissioned node's entry indefinitely
// (K8S-843); the uuid index still keeps every recorded identity.
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

	got, nodeByUUID, err := clusterMemberUUIDs(ctx, client)
	require.NoError(t, err)
	// node 1 (decommissioned) must be excluded even though broker_uuids lists it.
	assert.Equal(t, map[int]string{0: "uuid-aaa", 2: "uuid-ccc"}, got)
	// The uuid index keeps every recorded identity, retired ones included.
	assert.Equal(t, map[string]int{"uuid-aaa": 0, "uuid-bbb": 1, "uuid-ccc": 2}, nodeByUUID)
}

// TestPodAdminEndpoint: a pod matches the admin endpoint whose first DNS label
// equals the pod name; a pod name resolving to more than one endpoint is
// ambiguous (StretchCluster duplicate pool names) and matches none.
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

// TestStaleDiskWipeThresholdAndDisable: a zero or negative --wipe-stale-disk-after
// disables the destructive wipe entirely (never falling back to an internal
// default); a positive value tunes the not-ready threshold.
func TestStaleDiskWipeThresholdAndDisable(t *testing.T) {
	cases := []struct {
		name          string
		threshold     time.Duration
		wantDisabled  bool
		wantThreshold time.Duration // only checked when not disabled
	}{
		{name: "zero disables (explicit operator off switch)", threshold: 0, wantDisabled: true},
		{name: "positive tunes the threshold", threshold: 12 * time.Minute, wantDisabled: false, wantThreshold: 12 * time.Minute},
		{name: "negative disables entirely", threshold: -1 * time.Second, wantDisabled: true},
		{name: "any negative disables (parsed -1ns)", threshold: -1, wantDisabled: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &MulticlusterReconciler{StaleDiskWipeNotReadyThreshold: tc.threshold}
			assert.Equal(t, tc.wantDisabled, r.staleDiskWipeDisabled())
			if !tc.wantDisabled {
				assert.Equal(t, tc.wantThreshold, r.staleDiskWipeThreshold())
			}
		})
	}
}

// TestStaleDiskWipeForPendingPodWithNoReadyCondition: a bad_rejoin broker stuck
// Pending with only a PodScheduled=False condition (no PodReady) must have its
// not-ready duration measured from CreationTimestamp, so the wipe's duration
// gate is satisfiable — and only when every other guard also holds.
func TestStaleDiskWipeForPendingPodWithNoReadyCondition(t *testing.T) {
	const threshold = 5 * time.Minute
	now := time.Now()

	// A never-scheduled Pending pod: only PodScheduled=False, no PodReady
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

	// The pod's on-disk node_id 0 is absent from the member-uuid map
	// (decommissioned), so identityCollision confirms a collision. Nodes 2,3,4
	// are the surviving members.
	clusterUUIDs := map[int]string{2: "uuid-2", 3: "uuid-3", 4: "uuid-4"}
	const selfNodeID = 0
	const selfUUID = "uuid-0-retired"

	notReadyFor := podNotReadyFor(pendingPod, now)
	require.GreaterOrEqual(t, notReadyFor, threshold,
		"podNotReadyFor must measure a never-scheduled Pending pod's not-ready duration from its CreationTimestamp, not pin it at 0")

	collision, collisionReason := identityCollision(clusterUUIDs, selfNodeID, selfUUID)
	require.True(t, collision, "decommissioned node_id absent from the cluster must be a collision: %s", collisionReason)

	t.Run("collision + past threshold + healthy + no down nodes -> wipe", func(t *testing.T) {
		wipe, reason := decideStaleDiskWipe(collision, notReadyFor, threshold, true, 0)
		assert.True(t, wipe, "a bad_rejoin broker stuck 2h in a never-scheduled Pending pod must have its stale disk wiped: %s", reason)
	})

	t.Run("no collision -> no wipe", func(t *testing.T) {
		noCollision, _ := identityCollision(map[int]string{0: selfUUID, 2: "uuid-2"}, selfNodeID, selfUUID)
		require.False(t, noCollision)
		wipe, _ := decideStaleDiskWipe(noCollision, notReadyFor, threshold, true, 0)
		assert.False(t, wipe, "a legitimate member's disk must never be wiped even when its pod is long not-ready")
	})

	t.Run("cluster unhealthy -> no wipe", func(t *testing.T) {
		wipe, _ := decideStaleDiskWipe(collision, notReadyFor, threshold, false, 0)
		assert.False(t, wipe, "must defer the destructive wipe while the cluster is unhealthy")
	})

	t.Run("nodes down -> no wipe", func(t *testing.T) {
		wipe, _ := decideStaleDiskWipe(collision, notReadyFor, threshold, true, 1)
		assert.False(t, wipe, "must defer the destructive wipe while any node is down (possible partition)")
	})
}

// TestRedpandaStaleDiskWipeThresholdAndDisable pins the same
// --wipe-stale-disk-after disable/tune semantics on the RedpandaReconciler.
func TestRedpandaStaleDiskWipeThresholdAndDisable(t *testing.T) {
	cases := []struct {
		name          string
		threshold     time.Duration
		wantDisabled  bool
		wantThreshold time.Duration // only checked when not disabled
	}{
		{name: "zero disables (explicit operator off switch)", threshold: 0, wantDisabled: true},
		{name: "positive tunes the threshold", threshold: 12 * time.Minute, wantDisabled: false, wantThreshold: 12 * time.Minute},
		{name: "negative disables entirely", threshold: -1 * time.Second, wantDisabled: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &RedpandaReconciler{StaleDiskWipeNotReadyThreshold: tc.threshold}
			assert.Equal(t, tc.wantDisabled, r.staleDiskWipeDisabled())
			if !tc.wantDisabled {
				assert.Equal(t, tc.wantThreshold, r.staleDiskWipeThreshold())
			}
		})
	}
}

// fakePodDeleter records the destructive lifecycle calls staleDiskWipe makes,
// in order.
type fakePodDeleter struct {
	mu    sync.Mutex
	calls []string
	// livePods overrides what GetLivePod returns per pod name; an absent name
	// echoes the snapshot pod back unchanged.
	livePods map[string]*corev1.Pod
}

func (f *fakePodDeleter) GetLivePod(_ context.Context, pod *lifecycle.MulticlusterPod) (*corev1.Pod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.livePods != nil {
		if p, ok := f.livePods[pod.GetName()]; ok {
			return p, nil
		}
	}
	return pod.Pod, nil
}

func (f *fakePodDeleter) DeletePVCsForPod(_ context.Context, pod *lifecycle.MulticlusterPod) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "pvcs:"+pod.GetName())
	return nil
}

func (f *fakePodDeleter) DeletePod(_ context.Context, pod *lifecycle.MulticlusterPod) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "pod:"+pod.GetName())
	return nil
}

func (f *fakePodDeleter) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

// staleDiskWipeFixture drives the shared staleDiskWipe core against a
// bad_rejoin candidate (node 6) whose retired identity lives in its pod's
// emptyDir, alongside healthy members 1, 2 and 3. /v1/broker_uuids retains the
// retired {6, uuid-6} entry, so membership must come from /v1/brokers.
type staleDiskWipeFixture struct {
	podNodeID int
	podUUID   string
	// podAdvertisedHost overrides the address the pod's broker
	// self-advertises; defaults to the pod's own FQDN. A foreign value
	// simulates a misdirected dial answered by some other broker.
	podAdvertisedHost string
	dialErr           error
	health            rpadmin.ClusterHealthOverview
	// recheckHealth, when set, is served by the cluster's health endpoint for
	// the pre-destruction re-check (defaults to the same view as health).
	recheckHealth *rpadmin.ClusterHealthOverview
	notReadyFor   time.Duration
	// podLogs, when non-empty, enables the bad_rejoin log-evidence fallback
	// and is returned for every log read. logsErr makes reads fail instead.
	podLogs string
	logsErr error
	// confirmInterval overrides the debounce confirmation window (default 0:
	// an identity re-observed on the very next pass confirms immediately).
	confirmInterval time.Duration
	// podDeleting marks the candidate pod as already being deleted.
	podDeleting bool
	// noAuthorityPod removes the Ready member pod that normally serves the
	// pass's authoritative cluster reads, leaving only NotReady pods.
	noAuthorityPod bool
	// podReplaced makes GetLivePod report a DIFFERENT UID for the candidate
	// than the snapshot, simulating a delete+recreate by another actor between
	// evaluation and the wipe.
	podReplaced bool
	// hostPathDataDir renders the candidate's datadir as a hostPath volume
	// (survives pod deletion) instead of the default emptyDir.
	hostPathDataDir bool
	// overrideUUIDs seeds a staged node_id_overrides migration set; a candidate
	// whose on-disk uuid is in it must be deferred (never wiped).
	overrideUUIDs map[string]struct{}
}

// wipePass is the cumulative deleter state and result after one staleDiskWipe
// pass.
type wipePass struct {
	calls  []string
	result ctrl.Result
	err    error
}

// run drives staleDiskWipe for the given number of passes against one shared
// wipeDebounce and returns the per-pass snapshots.
func (f staleDiskWipeFixture) run(t *testing.T, passes int) []wipePass {
	t.Helper()
	ctx := t.Context()
	const threshold = 5 * time.Minute

	brokers := []rpadmin.Broker{
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.wipe.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 2, InternalRPCAddress: "redpanda-2.redpanda.wipe.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 3, InternalRPCAddress: "redpanda-3.redpanda.wipe.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
	}
	clusterUUIDs := []rpadmin.BrokerUuids{
		{NodeID: 1, UUID: testUUID(1)},
		{NodeID: 2, UUID: testUUID(2)},
		{NodeID: 3, UUID: testUUID(3)},
		// Retired entry lingers after decommission; must NOT count as membership.
		{NodeID: 6, UUID: testUUID(6)},
	}

	recheck := f.health
	if f.recheckHealth != nil {
		recheck = *f.recheckHealth
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(brokers)
	})
	mux.HandleFunc("/v1/broker_uuids", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(clusterUUIDs)
	})
	// The first health request serves the pass-start view; later ones (the
	// pre-destruction re-check) serve the recheck view (defaults to the same).
	var healthCalls atomic.Int64
	mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
		h := f.health
		if healthCalls.Add(1) > 1 {
			h = recheck
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": h.IsHealthy, "nodes_down": h.NodesDown})
	})
	// leader_id 1 -> broker 1 -> pod "redpanda-1" (the bootstrap pod), so all
	// authoritative reads come from this server.
	mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": 1})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The bad_rejoin pod's local admin API: it self-reports the retired
	// identity persisted in its emptyDir, advertising its own address.
	advertisedHost := f.podAdvertisedHost
	if advertisedHost == "" {
		advertisedHost = "redpanda-0.redpanda.wipe.svc.cluster.local."
	}
	podMux := http.NewServeMux()
	podMux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":            f.podNodeID,
			"advertised_rpc_api": map[string]any{"address": advertisedHost, "port": 33145},
		})
	})
	podMux.HandleFunc("/v1/broker_uuids", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]rpadmin.BrokerUuids{{NodeID: f.podNodeID, UUID: f.podUUID}})
	})
	podSrv := httptest.NewServer(podMux)
	defer podSrv.Close()

	now := time.Now()
	var deletionTimestamp *metav1.Time
	if f.podDeleting {
		deletionTimestamp = ptr.To(metav1.NewTime(now))
	}
	pods := []*lifecycle.MulticlusterPod{
		{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-0", UID: "uid-redpanda-0", DeletionTimestamp: deletionTimestamp},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: "datadir",
				VolumeSource: func() corev1.VolumeSource {
					if f.hostPathDataDir {
						return corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/redpanda/data"}}
					}
					return corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
				}(),
			}}},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-f.notReadyFor)),
			}}},
		}},
	}
	if !f.noAuthorityPod {
		// A Ready member pod: the bootstrap for leaderAdmin, and (leader_id 1)
		// the controller leader itself, so it serves the authoritative reads.
		pods = append(pods, &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-1"},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}})
	}

	dial := func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		switch endpoint {
		case "redpanda-1.redpanda:9644":
			// The authority (Ready member) answers with the cluster view.
			return rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
		case "redpanda-0.redpanda:9644":
			// The candidate's own admin API.
			if f.dialErr != nil {
				return nil, f.dialErr
			}
			return rpadmin.NewAdminAPI([]string{podSrv.URL}, new(rpadmin.NopAuth), nil)
		default:
			t.Errorf("unexpected dial endpoint %q", endpoint)
			return nil, fmt.Errorf("unexpected endpoint %q", endpoint)
		}
	}

	var logs podLogsReader
	if f.podLogs != "" || f.logsErr != nil {
		logs = func(_ context.Context, pod *lifecycle.MulticlusterPod, opts *corev1.PodLogOptions) (string, error) {
			assert.Equal(t, "redpanda-0", pod.GetName(), "only the candidate pod's logs may be read")
			assert.Equal(t, "redpanda", opts.Container)
			if f.logsErr != nil {
				return "", f.logsErr
			}
			return f.podLogs, nil
		}
	}

	deleter := &fakePodDeleter{}
	if f.podReplaced {
		deleter.livePods = map[string]*corev1.Pod{
			"redpanda-0": {ObjectMeta: metav1.ObjectMeta{Name: "redpanda-0", UID: "uid-redpanda-0-REPLACED"}},
		}
	}
	debounce := &wipeDebounce{}
	results := make([]wipePass, 0, passes)
	for range passes {
		result, runErr := staleDiskWipe(ctx, staleDiskWipeParams{
			pods: pods,
			endpoints: func() []string {
				return []string{"redpanda-0.redpanda:9644", "redpanda-1.redpanda:9644"}
			},
			dial:            dial,
			deleter:         deleter,
			threshold:       threshold,
			logs:            logs,
			debounce:        debounce,
			confirmInterval: f.confirmInterval,
			overrideUUIDs:   f.overrideUUIDs,
		}, testr.New(t))
		results = append(results, wipePass{calls: deleter.recorded(), result: result, err: runErr})
	}
	return results
}

// TestStaleDiskWipeDeletesBadRejoinPodWithoutPVCs: when the stale identity
// lives only in an emptyDir, the PVC deletion is a no-op and deleting the pod
// is what discards the identity. The first pass only starts the debounce; the
// second deletes PVCs-then-pod in order and requeues for the replacement.
func TestStaleDiskWipeDeletesBadRejoinPodWithoutPVCs(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[0].err)
	assert.Empty(t, passes[0].calls, "the first observation of a retired identity must only start the confirmation window, never destroy")
	require.NoError(t, passes[1].err)
	assert.Equal(t, []string{"pvcs:redpanda-0", "pod:redpanda-0"}, passes[1].calls,
		"a re-confirmed retired-identity bad_rejoin on a healthy cluster must have its pod's storage (a no-op without PVCs) and pod deleted, in that order")
	assert.Equal(t, requeueTimeout, passes[1].result.RequeueAfter, "one wipe per pass: must requeue for the replacement")
}

// TestStaleDiskWipeDefersWhileNodesDown: a bad_rejoin candidate must never be
// wiped while any node is reported down (possible partition), across passes.
func TestStaleDiskWipeDefersWhileNodesDown(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: false, NodesDown: []int{0}},
		notReadyFor: 10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[1].err)
	assert.Empty(t, passes[1].calls, "no destructive action while the cluster is unhealthy / a node is down")
	assert.Zero(t, passes[1].result.RequeueAfter)
}

// TestStaleDiskWipeDefersOnUnreadableIdentity: a pod whose broker cannot answer
// its own admin API must defer rather than wipe on circumstantial evidence, and
// must not fail the reconcile pass.
func TestStaleDiskWipeDefersOnUnreadableIdentity(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		dialErr:     errors.New("connection refused"),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[1].err)
	assert.Empty(t, passes[1].calls)
	assert.Zero(t, passes[1].result.RequeueAfter)
}

// TestStaleDiskWipeLeavesLegitimateMemberAlone: a not-Ready pod whose broker
// self-reports an identity matching the cluster's view must never be wiped, no
// matter how long it has been down.
func TestStaleDiskWipeLeavesLegitimateMemberAlone(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   1,
		podUUID:     testUUID(1),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Hour,
	}.run(t, 2)
	require.NoError(t, passes[1].err)
	assert.Empty(t, passes[1].calls, "a legitimate member's storage must never be destroyed")
	assert.Zero(t, passes[1].result.RequeueAfter)
}

// TestStaleDiskWipeWrongResponderDefers: an answer whose self-advertised
// address does not belong to the pod (a misdirected dial) must never confirm a
// collision, however retired the reported identity looks.
func TestStaleDiskWipeWrongResponderDefers(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:         6,
		podUUID:           testUUID(6),
		podAdvertisedHost: "other-broker-0.elsewhere.svc.cluster.local.",
		health:            rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor:       10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[1].err)
	assert.Empty(t, passes[1].calls, "an answer not attributable to the pod must never authorize destruction")
}

// TestStaleDiskWipeHealthRecheckDefers: a health regression between the
// pass-start snapshot and the pre-destruction re-check must defer the wipe (the
// fresh view wins).
func TestStaleDiskWipeHealthRecheckDefers(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:     6,
		podUUID:       testUUID(6),
		health:        rpadmin.ClusterHealthOverview{IsHealthy: true},
		recheckHealth: &rpadmin.ClusterHealthOverview{IsHealthy: false, NodesDown: []int{2}},
		notReadyFor:   10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[1].err)
	assert.Empty(t, passes[1].calls, "a health regression between pass start and destruction must defer the wipe")
	assert.Zero(t, passes[1].result.RequeueAfter)
}

// badRejoinLogFixture builds realistic redpanda log content: the boot
// identity line (application_bootstrap.cc) followed by live bad_rejoin join
// retries (members_manager.cc + the join status string from cluster/types.h).
func badRejoinLogFixture(uuid string) string {
	return "INFO  2026-07-30 02:05:12,838 [shard 0:main] main - application_bootstrap.cc:237 - Loaded existing UUID for node: " + uuid + "\n" +
		"WARN  2026-07-30 02:16:40,385 [shard 0:main] cluster - members_manager.cc:1139 - Error joining cluster using {host: redpanda-0.redpanda.redpanda.svc.cluster.local., port: 33145} seed server (bad_rejoin: trying to rejoin with same ID and UUID as a decommissioned node)\n" +
		"WARN  2026-07-30 02:16:47,770 [shard 0:main] cluster - members_manager.cc:1139 - Error joining cluster using {host: redpanda-1.redpanda.redpanda.svc.cluster.local., port: 33145} seed server (bad_rejoin: trying to rejoin with same ID and UUID as a decommissioned node)\n"
}

// TestParseBootUUID: the newest boot line wins, the uuid must be dashed-hex
// shaped, and anything malformed or absent parses to "" (never a fabricated
// identity).
func TestParseBootUUID(t *testing.T) {
	u6, u7 := testUUID(6), testUUID(7)
	cases := []struct {
		name string
		logs string
		want string
	}{
		{"real line parses", badRejoinLogFixture(u6), u6},
		{"newest boot line wins across restart cycles", badRejoinLogFixture(u6) + badRejoinLogFixture(u7), u7},
		{"absent line yields empty", "WARN some other log content\n", ""},
		{"truncated uuid yields empty", "x - Loaded existing UUID for node: 0ec1d0c3-48f2\n", ""},
		{"non-uuid garbage yields empty", "x - Loaded existing UUID for node: not-a-uuid-just-noise-here-padding\n", ""},
		{"uuid at end of content without newline", "x - Loaded existing UUID for node: " + u6, u6},
		{"empty logs", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseBootUUID(tc.logs))
		})
	}
}

// TestReadBadRejoinEvidence: evidence holds only when a fresh bad_rejoin retry
// is present AND the boot identity parses; otherwise defer with a reason.
func TestReadBadRejoinEvidence(t *testing.T) {
	ctx := t.Context()
	pod := &lifecycle.MulticlusterPod{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "redpanda-0"}}}
	u6 := testUUID(6)

	reader := func(content string, err error) podLogsReader {
		return func(_ context.Context, _ *lifecycle.MulticlusterPod, opts *corev1.PodLogOptions) (string, error) {
			// The freshness read must be server-side bounded.
			if opts.SinceSeconds != nil {
				assert.Positive(t, *opts.SinceSeconds)
			}
			return content, err
		}
	}

	t.Run("live bad_rejoin with parseable identity", func(t *testing.T) {
		uuid, reason, ok := readBadRejoinEvidence(ctx, reader(badRejoinLogFixture(u6), nil), pod)
		require.True(t, ok, reason)
		assert.Equal(t, u6, uuid)
	})
	t.Run("no bad_rejoin lines defers", func(t *testing.T) {
		_, reason, ok := readBadRejoinEvidence(ctx, reader("INFO healthy broker chatter\n", nil), pod)
		assert.False(t, ok)
		assert.Contains(t, reason, "no recent bad_rejoin")
	})
	t.Run("bad_rejoin without identity line defers", func(t *testing.T) {
		_, reason, ok := readBadRejoinEvidence(ctx, reader("... (bad_rejoin: trying to rejoin ...)\n", nil), pod)
		assert.False(t, ok)
		assert.Contains(t, reason, "no parseable boot identity")
	})
	t.Run("unreadable logs defer", func(t *testing.T) {
		_, reason, ok := readBadRejoinEvidence(ctx, reader("", errors.New("kubelet unavailable")), pod)
		assert.False(t, ok)
		assert.Contains(t, reason, "unreadable")
	})
	t.Run("node_id_overrides migration in progress defers", func(t *testing.T) {
		// A broker recovered-with-data via node_id_overrides logs the OLD uuid
		// then "Overriding UUID"; the OLD uuid must not authorize a wipe.
		logs := badRejoinLogFixture(u6) +
			"WARN  2026-07-30 02:16:50,001 [shard 0:main] main - application_bootstrap.cc:251 - Overriding UUID for node: " + u6 + " -> " + testUUID(7) + "\n"
		_, reason, ok := readBadRejoinEvidence(ctx, reader(logs, nil), pod)
		assert.False(t, ok)
		assert.Contains(t, reason, "node_id_overrides")
	})
	t.Run("override from a PRIOR boot does not block a later clean bad_rejoin", func(t *testing.T) {
		// An override before the newest boot-identity line is a prior boot, so
		// evidence still holds.
		logs := "WARN app - Overriding UUID for node: " + testUUID(1) + " -> " + testUUID(2) + "\n" +
			badRejoinLogFixture(u6)
		uuid, reason, ok := readBadRejoinEvidence(ctx, reader(logs, nil), pod)
		require.True(t, ok, reason)
		assert.Equal(t, u6, uuid)
	})
}

// TestBootIdentityOverridden: an override line after the newest boot-identity
// line means the identity is being reassigned (in transition).
func TestBootIdentityOverridden(t *testing.T) {
	u6, u7 := testUUID(6), testUUID(7)
	assert.False(t, bootIdentityOverridden(badRejoinLogFixture(u6)), "plain bad_rejoin: no override")
	assert.True(t, bootIdentityOverridden(badRejoinLogFixture(u6)+"x Overriding UUID for node: "+u6+" -> "+u7+"\n"))
	assert.True(t, bootIdentityOverridden(badRejoinLogFixture(u6)+"x Overriding node ID: 6 -> 7 [ignore_existing_node_id? false]\n"))
	assert.False(t, bootIdentityOverridden("x Overriding UUID for node: a -> b\n"+badRejoinLogFixture(u6)),
		"override BEFORE the newest boot-identity line is a prior boot; must not block")
	assert.False(t, bootIdentityOverridden(""), "no boot line at all")
}

// TestStaleDiskWipeBadRejoinLogFallback: a bad_rejoin broker never binds its
// admin API, so when the dial fails the log-evidence fallback must supply the
// identity and complete the wipe (debounce first pass, delete on the second).
func TestStaleDiskWipeBadRejoinLogFallback(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6, // unused by the log path (dial never succeeds)
		podUUID:     testUUID(6),
		dialErr:     errors.New("connect: connection refused"),
		podLogs:     badRejoinLogFixture(testUUID(6)),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
	}.run(t, 2)
	require.NoError(t, passes[0].err)
	assert.Empty(t, passes[0].calls, "first log-evidence observation must only start the debounce window")
	require.NoError(t, passes[1].err)
	assert.Equal(t, []string{"pvcs:redpanda-0", "pod:redpanda-0"}, passes[1].calls,
		"a re-confirmed log-evidenced bad_rejoin on a healthy cluster must be wiped")
	assert.Equal(t, requeueTimeout, passes[1].result.RequeueAfter)
}

// TestStaleDiskWipeLogFallbackGuards: the log path authorizes only a live
// bad_rejoin loop with an identity the cluster knows and has retired; anything
// else (including a health regression) defers.
func TestStaleDiskWipeLogFallbackGuards(t *testing.T) {
	base := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		dialErr:     errors.New("connect: connection refused"),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
	}

	t.Run("no bad_rejoin in logs", func(t *testing.T) {
		f := base
		f.podLogs = "INFO ordinary startup chatter\nLoaded existing UUID for node: " + testUUID(6) + "\n"
		passes := f.run(t, 2)
		require.NoError(t, passes[1].err)
		assert.Empty(t, passes[1].calls, "without a live bad_rejoin loop, log evidence must not authorize")
	})
	t.Run("identity maps to a CURRENT member", func(t *testing.T) {
		f := base
		f.podLogs = badRejoinLogFixture(testUUID(1)) // node 1 is an active member
		passes := f.run(t, 2)
		require.NoError(t, passes[1].err)
		assert.Empty(t, passes[1].calls, "a current member's identity must never be treated as retired")
	})
	t.Run("identity unknown to the cluster", func(t *testing.T) {
		f := base
		f.podLogs = badRejoinLogFixture(testUUID(99))
		passes := f.run(t, 2)
		require.NoError(t, passes[1].err)
		assert.Empty(t, passes[1].calls, "an unknown uuid could be a fresh unreplicated identity; defer")
	})
	t.Run("log read errors defer", func(t *testing.T) {
		f := base
		f.logsErr = errors.New("kubelet unavailable")
		passes := f.run(t, 2)
		require.NoError(t, passes[1].err, "unreadable logs must not fail the reconcile step")
		assert.Empty(t, passes[1].calls)
	})
	t.Run("health gate still applies to log-evidenced wipes", func(t *testing.T) {
		f := base
		f.podLogs = badRejoinLogFixture(testUUID(6))
		f.health = rpadmin.ClusterHealthOverview{IsHealthy: false, NodesDown: []int{2}}
		passes := f.run(t, 2)
		require.NoError(t, passes[1].err)
		assert.Empty(t, passes[1].calls, "log evidence never bypasses the health gates")
	})
}

// TestStaleDiskWipePendingConfirmationRequeues: an identity waiting out its
// confirmation window has no watch event to re-trigger it, so the pass must
// requeue at the confirmation interval rather than wait for the periodic sync.
func TestStaleDiskWipePendingConfirmationRequeues(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:       6,
		podUUID:         testUUID(6),
		health:          rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor:     10 * time.Minute,
		confirmInterval: 30 * time.Second,
	}.run(t, 1)
	require.NoError(t, passes[0].err)
	assert.Empty(t, passes[0].calls, "first observation must not destroy anything")
	assert.Equal(t, 30*time.Second, passes[0].result.RequeueAfter,
		"a pending confirmation must requeue at the confirmation interval instead of waiting for the periodic sync")
}

// TestStaleDiskWipeDefersStagedOverride: a candidate whose on-disk uuid is the
// target of a staged node_id_overrides migration must be preserved (reassigned
// with its data), even when every other guard would authorize the wipe.
func TestStaleDiskWipeDefersStagedOverride(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:     6,
		podUUID:       testUUID(6),
		health:        rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor:   10 * time.Minute,
		overrideUUIDs: map[string]struct{}{canonicalUUID(testUUID(6)): {}},
	}.run(t, 3)
	for i, pass := range passes {
		require.NoError(t, pass.err)
		assert.Empty(t, pass.calls, "pass %d: a migration-target identity must never be wiped", i)
	}
}

// TestStaleDiskWipeSkipsReplacedPod: if a candidate's pod was deleted and
// recreated (same name, new UID) between evaluation and the wipe, the by-name
// deletes would hit the innocent replacement, so a UID mismatch aborts.
func TestStaleDiskWipeSkipsReplacedPod(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
		podReplaced: true,
	}.run(t, 2)
	for i, pass := range passes {
		require.NoError(t, pass.err)
		assert.Empty(t, pass.calls, "pass %d: a pod replaced since evaluation must never be wiped", i)
	}
}

// TestStaleDiskWipeDefersHostPathDataDir: a candidate whose datadir is a
// hostPath (survives pod deletion) must never be wiped, or deleting the pod
// would reschedule it onto the same stale disk forever.
func TestStaleDiskWipeDefersHostPathDataDir(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:       6,
		podUUID:         testUUID(6),
		health:          rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor:     10 * time.Minute,
		hostPathDataDir: true,
	}.run(t, 3)
	for i, pass := range passes {
		require.NoError(t, pass.err)
		assert.Empty(t, pass.calls, "pass %d: a hostPath datadir cannot be reset by the wipe, so it must never be deleted", i)
	}
}

// TestStagedOverrideUUIDs: collects the canonical uuids named by
// node_id_overrides (in any core-accepted form) so the wipe can defer exactly
// the migration-target identity.
func TestStagedOverrideUUIDs(t *testing.T) {
	u6, u7 := testUUID(6), testUUID(7)
	assert.Empty(t, stagedOverrideUUIDs(nil), "no node config")
	assert.Empty(t, stagedOverrideUUIDs([]byte(`{}`)), "empty node config")
	assert.Empty(t, stagedOverrideUUIDs([]byte(`{"node_id_overrides": []}`)), "empty override list")
	assert.Empty(t, stagedOverrideUUIDs([]byte(`{"developer_mode": true}`)), "unrelated node config")
	// The real core schema: {current_uuid, new_uuid, new_id}. Both uuids are
	// collected (canonical dash-less form); new_id (an int) is ignored.
	got := stagedOverrideUUIDs([]byte(`{"node_id_overrides": [{"current_uuid": "` + u6 + `", "new_uuid": "` + u7 + `", "new_id": 7}]}`))
	assert.Contains(t, got, canonicalUUID(u6), "the pre-migration (on-disk) uuid must be collected")
	assert.Contains(t, got, canonicalUUID(u7))
	assert.Len(t, got, 2)
	assert.Empty(t, stagedOverrideUUIDs([]byte(`not json`)), "unparseable config collects nothing")

	// An override uuid in a non-canonical form (uppercase / brace-wrapped /
	// dash-less), all accepted by core, must still canonicalize to match the
	// lowercase-hyphenated on-disk uuid.
	lower := "0ec1d0c3-48f2-4a5b-9c3d-1234567890ab"
	canon := canonicalUUID(lower)
	require.NotEmpty(t, canon)
	for _, form := range []string{
		strings.ToUpper(lower),             // uppercase
		"{" + lower + "}",                  // brace-wrapped
		strings.ReplaceAll(lower, "-", ""), // dash-less
		"{" + strings.ToUpper(strings.ReplaceAll(lower, "-", "")) + "}", // all three
	} {
		got := stagedOverrideUUIDs([]byte(`{"node_id_overrides": [{"current_uuid": "` + form + `"}]}`))
		assert.Contains(t, got, canon, "override uuid %q must canonicalize to %q", form, canon)
	}
}

// TestUnwipeableDataDir: classifies which datadir volumes the wipe can reset by
// deleting the pod (emptyDir, StatefulSet-managed PVC) versus those it cannot
// (hostPath, foreign PVC, or no datadir at all).
func TestUnwipeableDataDir(t *testing.T) {
	mk := func(src corev1.VolumeSource, name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-0"},
			Spec:       corev1.PodSpec{Volumes: []corev1.Volume{{Name: name, VolumeSource: src}}},
		}}
	}
	assert.Empty(t, unwipeableDataDir(mk(corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}, "datadir")),
		"emptyDir datadir is cleared by pod deletion")
	assert.Empty(t, unwipeableDataDir(mk(corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-redpanda-0"}}, "datadir")),
		"a StatefulSet-managed datadir claim is deleted by DeletePVCsForPod")
	assert.NotEmpty(t, unwipeableDataDir(mk(corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}, "datadir")),
		"a hostPath datadir survives pod deletion")
	assert.NotEmpty(t, unwipeableDataDir(mk(corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "some-shared-claim"}}, "datadir")),
		"a non-StatefulSet datadir claim is not deleted")
	assert.NotEmpty(t, unwipeableDataDir(mk(corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}, "other")),
		"no datadir volume -> cannot reset")
}

// TestStaleDiskWipeSkipsDeletingPod: a pod whose deletion is already in flight
// must never be re-wiped, or the wipe re-fires every debounce window for the
// whole termination grace period.
func TestStaleDiskWipeSkipsDeletingPod(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:   6,
		podUUID:     testUUID(6),
		health:      rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor: 10 * time.Minute,
		podDeleting: true,
	}.run(t, 2)
	for i, pass := range passes {
		require.NoError(t, pass.err)
		assert.Empty(t, pass.calls, "pass %d: a pod already being deleted must never be re-wiped", i)
		assert.Zero(t, pass.result.RequeueAfter, "pass %d: no confirmation is pending for a deleting pod", i)
	}
}

// TestStaleDiskWipeDefersWithoutAuthority: the cluster reads that authorize
// destruction must come from a Ready pod (a NotReady ghost can serve a frozen
// members table), so with no Ready pod even a perfect retired identity defers.
func TestStaleDiskWipeDefersWithoutAuthority(t *testing.T) {
	passes := staleDiskWipeFixture{
		podNodeID:      6,
		podUUID:        testUUID(6),
		health:         rpadmin.ClusterHealthOverview{IsHealthy: true},
		notReadyFor:    10 * time.Minute,
		noAuthorityPod: true,
	}.run(t, 2)
	for i, pass := range passes {
		require.NoError(t, pass.err)
		assert.Empty(t, pass.calls, "pass %d: no destruction without a Ready-pod authority for the cluster reads", i)
	}
}

// TestAuthorityAdminSelection: the first Ready pod in name order that actually
// answers the reachability probe wins; NotReady, deleting, endpointless,
// dial-failing, and unresponsive-Ready pods are all skipped.
func TestAuthorityAdminSelection(t *testing.T) {
	now := time.Now()
	mkPod := func(name string, ready bool, deleting bool) *lifecycle.MulticlusterPod {
		status := corev1.ConditionFalse
		if ready {
			status = corev1.ConditionTrue
		}
		var dt *metav1.Time
		if deleting {
			dt = ptr.To(metav1.NewTime(now))
		}
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, DeletionTimestamp: dt},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}}
	}

	// A working admin server for the pod that should win the probe.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	endpoints := []string{"redpanda-0.redpanda:9644", "redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644", "redpanda-3.redpanda:9644"}
	var dialed []string
	dial := func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		dialed = append(dialed, endpoint)
		switch endpoint {
		case "redpanda-1.redpanda:9644":
			return nil, errors.New("connection refused") // dial fails outright
		case "redpanda-3.redpanda:9644":
			return rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil) // answers the probe
		default:
			return rpadmin.NewAdminAPI([]string{"http://127.0.0.1:1"}, new(rpadmin.NopAuth), nil) // constructs but unreachable
		}
	}

	// redpanda-0 NotReady (ghost-suspect), redpanda-1 Ready but dial-errors,
	// redpanda-2 Ready+deleting, redpanda-3 Ready + reachable -> the authority.
	pods := []*lifecycle.MulticlusterPod{
		mkPod("redpanda-3", true, false),
		mkPod("redpanda-0", false, false),
		mkPod("redpanda-1", true, false),
		mkPod("redpanda-2", true, true),
	}
	admin, podName, ok := authorityAdmin(t.Context(), pods, endpoints, dial, now, testr.New(t))
	require.True(t, ok)
	defer admin.Close()
	assert.Equal(t, "redpanda-3", podName)
	assert.Equal(t, []string{"redpanda-1.redpanda:9644", "redpanda-3.redpanda:9644"}, dialed,
		"NotReady and deleting pods must never be dialed for the bootstrap; name order decides")

	// A Ready pod whose admin API does not answer the probe must be rotated
	// past; with only such a pod there is no authority.
	var reachDialed []string
	unreachableDial := func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		reachDialed = append(reachDialed, endpoint)
		return rpadmin.NewAdminAPI([]string{"http://127.0.0.1:1"}, new(rpadmin.NopAuth), nil)
	}
	_, _, ok = authorityAdmin(t.Context(), []*lifecycle.MulticlusterPod{mkPod("redpanda-0", true, false)},
		[]string{"redpanda-0.redpanda:9644"}, unreachableDial, now, testr.New(t))
	assert.False(t, ok, "a Ready pod whose admin API does not answer the probe must not become the bootstrap")
	assert.Equal(t, []string{"redpanda-0.redpanda:9644"}, reachDialed, "the unreachable Ready pod was probed then rotated past")

	_, _, ok = authorityAdmin(t.Context(), []*lifecycle.MulticlusterPod{mkPod("redpanda-0", false, false)}, endpoints, dial, now, testr.New(t))
	assert.False(t, ok, "a cluster with no Ready pod has no bootstrap")
}

// TestLeaderAdmin: authoritative reads come from the controller leader's pod
// (bootstrap off a Ready pod, read the leader id, dial the leader) even when
// the bootstrap pod is not the leader; no elected leader defers.
func TestLeaderAdmin(t *testing.T) {
	now := time.Now()
	ready := func(name string) *lifecycle.MulticlusterPod {
		return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour)),
			}}},
		}}
	}
	brokers := []rpadmin.Broker{
		{NodeID: 1, InternalRPCAddress: "redpanda-1.redpanda.wipe.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
		{NodeID: 2, InternalRPCAddress: "redpanda-2.redpanda.wipe.svc.cluster.local.", InternalRPCPort: 33145, IsAlive: ptr.To(true)},
	}
	endpoints := []string{"redpanda-1.redpanda:9644", "redpanda-2.redpanda:9644"}
	pods := []*lifecycle.MulticlusterPod{ready("redpanda-1"), ready("redpanda-2")}

	// leaderID is served by whichever server the bootstrap dials; the leader's
	// own server tags its identity so we can assert which one answered.
	mkServer := func(leaderID int, tag string) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/cluster/health_overview", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"is_healthy": true})
		})
		mux.HandleFunc("/v1/partitions/redpanda/controller/0", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"leader_id": leaderID})
		})
		mux.HandleFunc("/v1/brokers", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(brokers)
		})
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": leaderID, "rpc_tag": tag})
		})
		return httptest.NewServer(mux)
	}

	t.Run("dials the leader even when it is not the bootstrap pod", func(t *testing.T) {
		// Bootstrap is redpanda-1 (first by name), but the leader is node 2.
		srv1, srv2 := mkServer(2, "one"), mkServer(2, "two")
		defer srv1.Close()
		defer srv2.Close()
		var dialed []string
		dial := func(_ context.Context, ep string) (*rpadmin.AdminAPI, error) {
			dialed = append(dialed, ep)
			if ep == "redpanda-2.redpanda:9644" {
				return rpadmin.NewAdminAPI([]string{srv2.URL}, new(rpadmin.NopAuth), nil)
			}
			return rpadmin.NewAdminAPI([]string{srv1.URL}, new(rpadmin.NopAuth), nil)
		}
		admin, leaderPod, ok := leaderAdmin(t.Context(), pods, endpoints, dial, now, testr.New(t))
		require.True(t, ok)
		defer admin.Close()
		assert.Equal(t, "redpanda-2", leaderPod, "the authority is the controller leader's pod")
		assert.Contains(t, dialed, "redpanda-2.redpanda:9644", "the leader's pod must be dialed")
	})

	t.Run("no elected leader defers", func(t *testing.T) {
		srv := mkServer(-1, "none")
		defer srv.Close()
		dial := func(_ context.Context, _ string) (*rpadmin.AdminAPI, error) {
			return rpadmin.NewAdminAPI([]string{srv.URL}, new(rpadmin.NopAuth), nil)
		}
		_, _, ok := leaderAdmin(t.Context(), pods, endpoints, dial, now, testr.New(t))
		assert.False(t, ok, "no elected controller leader must defer authoritative reads")
	})
}

// TestWipeDebounceConfirm: a retired identity confirms only when observed
// twice, unchanged, at least the confirmation interval apart; a changed
// identity restarts the window (per pod, and regardless of the elapsed gap).
func TestWipeDebounceConfirm(t *testing.T) {
	const interval = 30 * time.Second
	t0 := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	d := &wipeDebounce{}

	ok, why := d.confirm("ns/redpanda-0", 6, "uuid-6", t0, interval)
	assert.False(t, ok, "first observation must never confirm: %s", why)

	ok, why = d.confirm("ns/redpanda-0", 6, "uuid-6", t0.Add(5*time.Second), interval)
	assert.False(t, ok, "re-observation inside the interval must not confirm: %s", why)

	ok, why = d.confirm("ns/redpanda-0", 7, "uuid-7", t0.Add(40*time.Second), interval)
	assert.False(t, ok, "a different identity must restart the window: %s", why)

	ok, why = d.confirm("ns/redpanda-0", 7, "uuid-7", t0.Add(80*time.Second), interval)
	assert.True(t, ok, "the same identity re-observed after the interval must confirm: %s", why)

	d.forget("ns/redpanda-0")
	ok, why = d.confirm("ns/redpanda-0", 7, "uuid-7", t0.Add(90*time.Second), interval)
	assert.False(t, ok, "forget must restart the confirmation window: %s", why)

	ok, _ = d.confirm("ns/other-0", 6, "uuid-6", t0.Add(200*time.Second), interval)
	assert.False(t, ok, "observations are per pod, never shared across keys")

	// A long gap between sightings must not restart the debounce (inter-pass
	// time is unbounded), so the same still-retired identity confirms once its
	// age passes the interval; recovery is detected by content, not elapsed time.
	d2 := &wipeDebounce{}
	ok, _ = d2.confirm("ns/redpanda-0", 6, "uuid-6", t0, interval)
	require.False(t, ok)
	ok, why = d2.confirm("ns/redpanda-0", 6, "uuid-6", t0.Add(20*time.Minute), interval)
	assert.True(t, ok, "a still-retired identity re-observed after a long gap must confirm, not restart: %s", why)

	// A change of identity still restarts the window even across a long gap.
	d3 := &wipeDebounce{}
	ok, _ = d3.confirm("ns/redpanda-0", 6, "uuid-6", t0, interval)
	require.False(t, ok)
	ok, why = d3.confirm("ns/redpanda-0", 7, "uuid-7", t0.Add(20*time.Minute), interval)
	assert.False(t, ok, "a different identity restarts the window regardless of elapsed time: %s", why)
}
