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
	"testing"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
	"github.com/redpanda-data/redpanda-operator/enterprise/operator/statuses"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// K8S-891: a leadership change can leave the new leader's health probe not
// yet reporting a peer cluster reachable. FetchExistingBrokerPoolsFromAllClusters
// then silently skips that peer, so the fetched BrokerPool union is missing
// pools. unobservedClusters must surface exactly the clusters missing from
// the observed set, using canonical (not raw multicluster-runtime) names.
func TestUnobservedClusters(t *testing.T) {
	localName := func() string { return "rp-failover" }

	for name, tc := range map[string]struct {
		allClusters []string
		observed    map[string]bool
		want        []string
	}{
		"all observed": {
			allClusters: []string{"", "rp-east", "rp-west"},
			observed:    map[string]bool{"": true, "rp-east": true, "rp-west": true},
			want:        nil,
		},
		"one peer unobserved": {
			allClusters: []string{"", "rp-east", "rp-west"},
			observed:    map[string]bool{"": true, "rp-west": true},
			want:        []string{"rp-east"},
		},
		"local cluster name is canonicalized": {
			allClusters: []string{"", "rp-east"},
			observed:    map[string]bool{"rp-east": true},
			want:        []string{"rp-failover"},
		},
		"nothing observed": {
			allClusters: []string{"", "rp-east", "rp-west"},
			observed:    map[string]bool{},
			want:        []string{"rp-failover", "rp-east", "rp-west"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := unobservedClusters(tc.allClusters, tc.observed, localName)
			require.Equal(t, tc.want, got)
		})
	}
}

// K8S-891 regression: when a configured cluster's BrokerPool list wasn't
// observed this pass, checkBrokerPoolViewComplete must report incomplete and
// set ResourcesSynced to a retryable Error rather than let the caller proceed
// to render seed_servers from a partial pool view.
func TestCheckBrokerPoolViewComplete_IncompleteObservationDefers(t *testing.T) {
	r := &MulticlusterReconciler{}
	state := &stretchClusterReconciliationState{
		status:                       lifecycle.NewStretchClusterStatus(),
		unobservedBrokerPoolClusters: []string{"rp-east", "rp-west"},
	}

	incomplete, result := r.checkBrokerPoolViewComplete(context.Background(), state)
	require.True(t, incomplete, "an unobserved peer must defer resource rendering")
	require.Equal(t, requeueTimeout, result.RequeueAfter)

	sc := &redpandav1alpha2.StretchCluster{}
	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterResourcesSynced)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(statuses.StretchClusterResourcesSyncedReasonError), cond.Reason)
	require.Contains(t, cond.Message, "rp-east")
	require.Contains(t, cond.Message, "rp-west")
}

// Regression guard: once every configured cluster has been observed,
// checkBrokerPoolViewComplete must not block Phase 2 rendering.
func TestCheckBrokerPoolViewComplete_FullObservationProceeds(t *testing.T) {
	r := &MulticlusterReconciler{}
	state := &stretchClusterReconciliationState{
		status: lifecycle.NewStretchClusterStatus(),
	}

	incomplete, result := r.checkBrokerPoolViewComplete(context.Background(), state)
	require.False(t, incomplete)
	require.Zero(t, result.RequeueAfter)
}

// brokerPoolViewManager is a fake multicluster.Manager exposing distinct
// registered (GetClusterNames) and configured (GetConfiguredClusterNames)
// sets, mimicking the raft manager's bootstrap mode where a peer appears in
// the configured set from process start but is registered with the runtime
// provider only after leadership is acquired and its kubeconfig fetched.
type brokerPoolViewManager struct {
	multicluster.Manager
	local      string
	registered []string
	configured []string
}

func (m *brokerPoolViewManager) GetClusterNames() []string           { return m.registered }
func (m *brokerPoolViewManager) GetConfiguredClusterNames() []string { return m.configured }
func (m *brokerPoolViewManager) GetLocalClusterName() string         { return m.local }

func TestUnionClusterNames(t *testing.T) {
	for name, tc := range map[string]struct {
		a, b []string
		want []string
	}{
		"identical":            {a: []string{"", "rp-east"}, b: []string{"", "rp-east"}, want: []string{"", "rp-east"}},
		"configured superset":  {a: []string{""}, b: []string{"", "rp-east", "rp-west"}, want: []string{"", "rp-east", "rp-west"}},
		"dynamic extra member": {a: []string{"", "rp-dynamic"}, b: []string{"", "rp-east"}, want: []string{"", "rp-dynamic", "rp-east"}},
		"both empty":           {a: nil, b: nil, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, unionClusterNames(tc.a, tc.b))
		})
	}
}

// K8S-891 regression (PR #1683 review): Manager.GetClusterNames reflects only
// clusters currently registered with the runtime provider, and bootstrap
// peers are registered by leader routines that run only after raft
// leadership is acquired. A reconcile racing that registration sees just the
// local cluster; its own BrokerPool list succeeds, so an expected set
// derived from the registered clusters alone reports nothing unobserved and
// the gate lets a self-only seed list render. The expected set must instead
// include the manager's configured clusters, so the not-yet-registered peer
// keeps the gate closed until its registration completes and its BrokerPool
// list is actually observed.
func TestBrokerPoolViewGate_ReconcileRacingPeerRegistrationDefers(t *testing.T) {
	mgr := &brokerPoolViewManager{
		local:      "rp-central",
		registered: []string{""},            // peer registration hasn't completed
		configured: []string{"", "rp-east"}, // static raft peer topology
	}
	r := &MulticlusterReconciler{Manager: mgr}

	// Only the local cluster was registered, so only its BrokerPool list was
	// fetched and observed.
	observed := map[string]bool{"": true}

	expected := r.expectedBrokerPoolClusters(mgr.GetClusterNames())
	unobserved := unobservedClusters(expected, observed, mgr.GetLocalClusterName)
	require.Equal(t, []string{"rp-east"}, unobserved,
		"a configured-but-unregistered peer must count as unobserved")

	state := &stretchClusterReconciliationState{
		status:                       lifecycle.NewStretchClusterStatus(),
		unobservedBrokerPoolClusters: unobserved,
	}
	incomplete, result := r.checkBrokerPoolViewComplete(context.Background(), state)
	require.True(t, incomplete, "reconciliation racing peer registration must defer pool-derived rendering")
	require.Equal(t, requeueTimeout, result.RequeueAfter)

	// Once the peer's registration completes and its BrokerPool list is
	// observed, the same computation must open the gate.
	mgr.registered = []string{"", "rp-east"}
	observed["rp-east"] = true
	unobserved = unobservedClusters(r.expectedBrokerPoolClusters(mgr.GetClusterNames()), observed, mgr.GetLocalClusterName)
	require.Empty(t, unobserved, "a registered and observed peer must not defer rendering")
}
