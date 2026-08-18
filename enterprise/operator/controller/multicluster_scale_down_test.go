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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
)

// TestScaleDownDefersOnAmbiguousBrokerMatch pins that scaleDown never falls
// through to the "not in brokerMap" path when a pod name ambiguously matches
// multiple brokers. That path assumes the broker was already fully removed
// from the cluster and proceeds straight to patching the StatefulSet — which,
// for a StretchCluster with identically-named BrokerPools across member
// clusters (a StatefulSet/pod name has no member-cluster component), would
// either decommission the wrong broker or orphan a live one instead of
// deferring to a future reconcile. The ambiguous branch returns before a real
// admin API or Kubernetes client is touched, so neither is needed here. (The
// OSS single-cluster RedpandaReconciler's identical decision is pinned by the
// same test in its own package.)
func TestScaleDownDefersOnAmbiguousBrokerMatch(t *testing.T) {
	ambiguousMap := map[string][]int{"redpanda-default-0": {0, 7}}
	set := &lifecycle.ScaleDownSet{
		LastPod:     &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "redpanda-default-0"}},
		StatefulSet: &lifecycle.MulticlusterStatefulSet{StatefulSet: &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "redpanda-default"}}},
	}

	r := &MulticlusterReconciler{}
	requeue, err := r.scaleDown(t.Context(), nil, &lifecycle.StretchClusterWithPools{}, set, ambiguousMap, map[int]bool{})
	require.NoError(t, err)
	assert.True(t, requeue, "an ambiguous match must requeue rather than proceed")
}
