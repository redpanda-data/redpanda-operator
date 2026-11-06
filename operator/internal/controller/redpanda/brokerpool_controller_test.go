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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
)

func TestDifferingFields(t *testing.T) {
	podTemplate := func(node string) *redpandav1alpha2.PodTemplate {
		return &redpandav1alpha2.PodTemplate{Spec: &applycorev1.PodSpecApplyConfiguration{NodeSelector: map[string]string{"node": node}}}
	}

	for name, tc := range map[string]struct {
		nodePool   redpandav1alpha2.EmbeddedNodePoolSpec
		brokerPool redpandav1alpha2.EmbeddedBrokerPoolSpec
		want       []string
	}{
		"equal specs": {
			nodePool:   redpandav1alpha2.EmbeddedNodePoolSpec{PodTemplate: podTemplate("a")},
			brokerPool: redpandav1alpha2.EmbeddedBrokerPoolSpec{PodTemplate: podTemplate("a")},
		},
		"unset replicas equals the chart default": {
			brokerPool: redpandav1alpha2.EmbeddedBrokerPoolSpec{Replicas: ptr.To(int32(3))},
		},
		"unset replicas differs from one": {
			brokerPool: redpandav1alpha2.EmbeddedBrokerPoolSpec{Replicas: ptr.To(int32(1))},
			want:       []string{"replicas"},
		},
		"fields set on either side": {
			nodePool:   redpandav1alpha2.EmbeddedNodePoolSpec{PodTemplate: podTemplate("a")},
			brokerPool: redpandav1alpha2.EmbeddedBrokerPoolSpec{Image: &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v26.1.2")}},
			want:       []string{"image", "podTemplate"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			np := &redpandav1alpha2.NodePool{Spec: redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: tc.nodePool}}
			bp := &redpandav1alpha2.RedpandaBrokerPool{Spec: redpandav1alpha2.BrokerPoolSpec{EmbeddedBrokerPoolSpec: tc.brokerPool}}
			got, err := differingFields(np, bp)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBrokerPoolSuspension(t *testing.T) {
	nodePoolOwned := conversion.ResolvePools([]*redpandav1alpha2.NodePool{{ObjectMeta: metav1.ObjectMeta{Name: "np"}}}, nil)
	brokerPoolOwned := conversion.ResolvePools(nil, []*redpandav1alpha2.RedpandaBrokerPool{{ObjectMeta: metav1.ObjectMeta{Name: "bp"}}})
	missingFile := filepath.Join(t.TempDir(), "license")

	for name, tc := range map[string]struct {
		pools       []conversion.OwnedPool
		licensePath string
		// want is a substring of the reason; empty means not suspended.
		want string
	}{
		"no pools":                       {},
		"NodePool pools need no license": {pools: nodePoolOwned},
		"no license configured":          {pools: brokerPoolOwned, want: "RedpandaBrokerPools bp require an enterprise license: no license configured"},
		"unreadable license":             {pools: brokerPoolOwned, licensePath: missingFile, want: "reading license"},
		"superseded RedpandaBrokerPools own nothing": {pools: conversion.ResolvePools(
			[]*redpandav1alpha2.NodePool{{ObjectMeta: metav1.ObjectMeta{Name: "a"}}},
			[]*redpandav1alpha2.RedpandaBrokerPool{{ObjectMeta: metav1.ObjectMeta{Name: "a"}}},
		)},
	} {
		t.Run(name, func(t *testing.T) {
			got := brokerPoolSuspension(tc.pools, tc.licensePath)
			if tc.want == "" {
				require.Empty(t, got)
				return
			}
			require.Contains(t, got, tc.want)
		})
	}
}

// TestSuspendedPoolStepsDoNothing pins the freeze: the steps that apply the
// pools' specs return before touching the state, which here is empty and
// would panic if read.
func TestSuspendedPoolStepsDoNothing(t *testing.T) {
	r := &RedpandaReconciler{}
	for name, step := range map[string]clusterReconciliationFn{
		"reconcileResources":    r.reconcileResources,
		"reconcilePools":        r.reconcilePools,
		"reconcileDecommission": r.reconcileDecommission,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := step(t.Context(), &clusterReconciliationState{poolsSuspended: "frozen"}, nil)
			require.NoError(t, err)
			require.Zero(t, result)
		})
	}
}
