// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	pkglabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// TestControllerEnabledDecommissionV2 documents that decommissionV2 is opt-in
// only: it is NOT enabled by "all" (it conflicts with the "decommission"
// controller), but IS enabled when listed explicitly. The legacy controllers
// remain enabled by "all".
func TestControllerEnabledDecommissionV2(t *testing.T) {
	cases := []struct {
		name        string
		controllers []string
		decommV2    bool
		oldDecomm   bool
		nodeWatcher bool
	}{
		{name: "none", controllers: []string{""}, decommV2: false, oldDecomm: false, nodeWatcher: false},
		{name: "all excludes decommissionV2", controllers: []string{"all"}, decommV2: false, oldDecomm: true, nodeWatcher: true},
		{name: "explicit decommissionV2", controllers: []string{"decommissionV2"}, decommV2: true, oldDecomm: false, nodeWatcher: false},
		{name: "explicit decommission", controllers: []string{"decommission"}, decommV2: false, oldDecomm: true, nodeWatcher: false},
		{name: "all plus decommissionV2", controllers: []string{"all", "decommissionV2"}, decommV2: true, oldDecomm: true, nodeWatcher: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &RunOptions{additionalControllers: tc.controllers}
			assert.Equal(t, tc.decommV2, o.ControllerEnabled(DecommissionController), "decommissionV2")
			assert.Equal(t, tc.oldDecomm, o.ControllerEnabled(OldDecommissionController), "decommission")
			assert.Equal(t, tc.nodeWatcher, o.ControllerEnabled(NodeWatcherController), "nodeWatcher")
		})
	}
}

func redpandaSTS(name, namespace, instance string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				pkglabels.NameKey:     "redpanda",
				pkglabels.InstanceKey: instance,
			},
		},
		Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(replicas)},
	}
}

func readyRedpanda(name, namespace string) *redpandav1alpha2.Redpanda {
	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Generation: 1},
		Status: redpandav1alpha2.RedpandaStatus{
			DeprecatedObservedGeneration: 1,
			Conditions: []metav1.Condition{
				{Type: redpandav1alpha2.ReadyCondition, Status: metav1.ConditionTrue, Reason: "Ready"},
			},
		},
	}
	return rp
}

func TestRedpandaDecommissionerAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("desiredReplicas sums every NodePool StatefulSet", func(t *testing.T) {
		rp := readyRedpanda("redpanda", "ns")
		base := redpandaSTS("redpanda", "ns", "redpanda", 3)
		poolA := redpandaSTS("redpanda-pool-a", "ns", "redpanda", 2)
		// A StatefulSet belonging to a different cluster must not be counted.
		other := redpandaSTS("other", "ns", "other", 5)

		c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).
			WithObjects(rp, base, poolA, other).Build()
		adapter := &redpandaDecommissionerAdapter{client: c}

		got, err := adapter.desiredReplicas(ctx, base)
		require.NoError(t, err)
		assert.Equal(t, int32(5), got, "should sum base(3) + pool-a(2), excluding other cluster")
	})

	t.Run("getRedpanda returns nil for non-Redpanda StatefulSet", func(t *testing.T) {
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}}
		c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).Build()
		adapter := &redpandaDecommissionerAdapter{client: c}

		rp, err := adapter.getRedpanda(ctx, sts)
		require.NoError(t, err)
		assert.Nil(t, rp)
	})

	t.Run("getRedpanda returns nil when the Redpanda CR is absent", func(t *testing.T) {
		sts := redpandaSTS("redpanda", "ns", "redpanda", 3)
		c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(sts).Build()
		adapter := &redpandaDecommissionerAdapter{client: c}

		rp, err := adapter.getRedpanda(ctx, sts)
		require.NoError(t, err)
		assert.Nil(t, rp)
	})

	t.Run("filter", func(t *testing.T) {
		sts := redpandaSTS("redpanda", "ns", "redpanda", 3)

		t.Run("accepts a Ready, reconciled cluster", func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).
				WithObjects(readyRedpanda("redpanda", "ns"), sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.True(t, ok)
		})

		t.Run("skips a cluster that is not Ready", func(t *testing.T) {
			rp := readyRedpanda("redpanda", "ns")
			rp.Status.Conditions[0].Status = metav1.ConditionFalse
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(rp, sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.False(t, ok)
		})

		t.Run("skips a cluster whose generation is not observed", func(t *testing.T) {
			rp := readyRedpanda("redpanda", "ns")
			rp.Generation = 2 // status still observes generation 1
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(rp, sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.False(t, ok)
		})

		t.Run("skips a cluster that is being deleted", func(t *testing.T) {
			rp := readyRedpanda("redpanda", "ns")
			now := metav1.Now()
			rp.DeletionTimestamp = &now
			rp.Finalizers = []string{"test/finalizer"}
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(rp, sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.False(t, ok)
		})

		t.Run("skips a StatefulSet with no resolvable Redpanda", func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.False(t, ok)
		})
	})
}
