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

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
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
