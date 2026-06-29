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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// specConsistencyCluster is a fake cluster.Cluster whose only useful behaviour
// is returning a pre-seeded client. Everything else is left nil — the spec
// consistency check only ever calls GetClient.
type specConsistencyCluster struct {
	cluster.Cluster
	client client.Client
}

func (c *specConsistencyCluster) GetClient() client.Client { return c.client }

// specConsistencyManager is a fake multicluster.Manager that returns a fixed
// set of peer clusters, each with its own reachability and client. Only the
// four methods checkSpecConsistency touches are implemented; the embedded
// interface satisfies the rest (and panics if anything unexpected is called).
type specConsistencyManager struct {
	multicluster.Manager
	local     string
	names     []string
	reachable map[string]bool
	clients   map[string]client.Client
}

func (m *specConsistencyManager) GetClusterNames() []string   { return m.names }
func (m *specConsistencyManager) GetLocalClusterName() string { return m.local }

func (m *specConsistencyManager) IsClusterReachable(name string) bool {
	reachable, ok := m.reachable[name]
	return !ok || reachable // default: reachable
}

func (m *specConsistencyManager) GetCluster(_ context.Context, name string) (cluster.Cluster, error) {
	return &specConsistencyCluster{client: m.clients[name]}, nil
}

func specConsistencyScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.Install(scheme))
	return scheme
}

func localStretchCluster() *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "redpanda", Namespace: "redpanda"},
		Spec: redpandav1alpha2.StretchClusterSpec{
			CommonLabels: map[string]string{"team": "redpanda"},
		},
	}
}

// readSpecSynced runs UpdateConditions onto a throwaway object and returns the
// resulting SpecSynced condition.
func readSpecSynced(t *testing.T, state *stretchClusterReconciliationState) *metav1.Condition {
	t.Helper()
	sc := &redpandav1alpha2.StretchCluster{}
	state.status.StretchClusterStatus.UpdateConditions(sc)
	return apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
}

// K8S-883: a peer that is registered in the manager but whose StretchCluster CR
// has been removed is *reachable* — its CR is simply NotFound. The spec
// consistency check must NOT report this as ClusterUnreachable (which is
// misleading and, by leaving SpecSynced perpetually non-True, blocks the
// cluster from ever reaching Stable). With every reachable, participating peer
// in agreement, SpecSynced must resolve to True.
func TestCheckSpecConsistency_ReachablePeerMissingStretchClusterIsNotUnreachable(t *testing.T) {
	scheme := specConsistencyScheme(t)
	sc := localStretchCluster()

	// peer-b is reachable but has no StretchCluster CR (empty client).
	mgr := &specConsistencyManager{
		local: "local",
		names: []string{"local", "peer-b"},
		clients: map[string]client.Client{
			"peer-b": fake.NewClientBuilder().WithScheme(scheme).Build(),
		},
	}

	r := &MulticlusterReconciler{Manager: mgr}
	state := &stretchClusterReconciliationState{status: lifecycle.NewStretchClusterStatus()}

	drifted, _ := r.checkSpecConsistency(context.Background(), state, sc, "local")
	require.False(t, drifted, "a missing peer CR must not be treated as drift")

	cond := readSpecSynced(t, state)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status,
		"reachable peer with no StretchCluster must not block SpecSynced from reaching True")
	require.Equal(t, string(statuses.StretchClusterSpecSyncedReasonSynced), cond.Reason,
		"a reachable peer missing its CR must not be reported as ClusterUnreachable")
	require.Contains(t, cond.Message, "peer-b",
		"the message should clearly name the peer missing its StretchCluster")
}

// Regression guard: a genuinely unreachable peer (background probe down) must
// still surface as ClusterUnreachable so operators can distinguish a real
// connectivity problem from an intentionally-absent peer.
func TestCheckSpecConsistency_UnreachablePeerReportsClusterUnreachable(t *testing.T) {
	scheme := specConsistencyScheme(t)
	sc := localStretchCluster()

	mgr := &specConsistencyManager{
		local:     "local",
		names:     []string{"local", "peer-b"},
		reachable: map[string]bool{"peer-b": false},
		clients:   map[string]client.Client{"peer-b": fake.NewClientBuilder().WithScheme(scheme).Build()},
	}

	r := &MulticlusterReconciler{Manager: mgr}
	state := &stretchClusterReconciliationState{status: lifecycle.NewStretchClusterStatus()}

	drifted, _ := r.checkSpecConsistency(context.Background(), state, sc, "local")
	require.False(t, drifted)

	cond := readSpecSynced(t, state)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(statuses.StretchClusterSpecSyncedReasonClusterUnreachable), cond.Reason)
}

// Regression guard: when every peer is reachable and has an identical spec the
// condition resolves to Synced=True.
func TestCheckSpecConsistency_AllPeersAlignedIsSynced(t *testing.T) {
	scheme := specConsistencyScheme(t)
	sc := localStretchCluster()

	peerSC := localStretchCluster() // identical spec
	mgr := &specConsistencyManager{
		local: "local",
		names: []string{"local", "peer-b"},
		clients: map[string]client.Client{
			"peer-b": fake.NewClientBuilder().WithScheme(scheme).WithObjects(peerSC).Build(),
		},
	}

	r := &MulticlusterReconciler{Manager: mgr}
	state := &stretchClusterReconciliationState{status: lifecycle.NewStretchClusterStatus()}

	drifted, _ := r.checkSpecConsistency(context.Background(), state, sc, "local")
	require.False(t, drifted)

	cond := readSpecSynced(t, state)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, string(statuses.StretchClusterSpecSyncedReasonSynced), cond.Reason)
}
