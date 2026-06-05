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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	pkglabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// TestControllerEnabledDecommission documents the re-pointed controller
// selection: "decommission" (and its alias "decommissionV2", and "all") now
// select the new NodePool-aware StatefulSetDecommissioner, while the deprecated
// old reconciler is opt-in only via "legacy-decommission" and excluded from
// "all". When both are selected, Run() lets legacy-decommission win (asserted in
// TestDecommissionControllerPrecedence).
func TestControllerEnabledDecommission(t *testing.T) {
	cases := []struct {
		name        string
		controllers []string
		decomm      bool
		legacy      bool
	}{
		{name: "none", controllers: []string{""}, decomm: false, legacy: false},
		{name: "all selects new decommission, not legacy", controllers: []string{"all"}, decomm: true, legacy: false},
		{name: "explicit decommission", controllers: []string{"decommission"}, decomm: true, legacy: false},
		{name: "decommissionV2 alias", controllers: []string{"decommissionV2"}, decomm: true, legacy: false},
		{name: "explicit legacy-decommission", controllers: []string{"legacy-decommission"}, decomm: false, legacy: true},
		{name: "all plus legacy-decommission", controllers: []string{"all", "legacy-decommission"}, decomm: true, legacy: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &RunOptions{additionalControllers: tc.controllers}
			assert.Equal(t, tc.decomm, o.ControllerEnabled(DecommissionController), "decommission")
			assert.Equal(t, tc.legacy, o.ControllerEnabled(LegacyDecommissionController), "legacy-decommission")
		})
	}
}

// TestDecommissionControllerPrecedence asserts the escape-hatch precedence used
// in Run(): when legacy-decommission is explicitly selected it wins, and the new
// controller does not also run, so the two never double-process a cluster.
func TestDecommissionControllerPrecedence(t *testing.T) {
	cases := []struct {
		name        string
		controllers []string
		runNew      bool
		runLegacy   bool
	}{
		{name: "decommission runs new only", controllers: []string{"decommission"}, runNew: true, runLegacy: false},
		{name: "all runs new only", controllers: []string{"all"}, runNew: true, runLegacy: false},
		{name: "legacy runs legacy only", controllers: []string{"legacy-decommission"}, runNew: false, runLegacy: true},
		{name: "both selected: legacy wins", controllers: []string{"decommission", "legacy-decommission"}, runNew: false, runLegacy: true},
		{name: "all plus legacy: legacy wins", controllers: []string{"all", "legacy-decommission"}, runNew: false, runLegacy: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &RunOptions{additionalControllers: tc.controllers}
			// Mirrors the resolution in Run().
			runLegacy := o.ControllerEnabled(LegacyDecommissionController)
			runNew := !runLegacy && o.ControllerEnabled(DecommissionController)
			assert.Equal(t, tc.runNew, runNew, "runDecommission")
			assert.Equal(t, tc.runLegacy, runLegacy, "runLegacyDecommission")
		})
	}
}

func redpandaSTS(name, namespace, instance string, replicas int32) *appsv1.StatefulSet {
	return redpandaSTSWithName(name, namespace, instance, "redpanda", replicas)
}

// redpandaSTSWithName builds a StatefulSet whose app.kubernetes.io/name label is
// nameLabel, modeling the chart's behavior of stamping spec.clusterSpec.nameOverride
// into that label. Ownership is always carried by the stable instance label.
func redpandaSTSWithName(name, namespace, instance, nameLabel string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				pkglabels.NameKey:     nameLabel,
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

// TestRedpandaDecommissionerSelector locks in the fix for the nameOverride
// regression: the operator-wide decommissioner must select StatefulSets and
// PVCs by the stable app.kubernetes.io/instance label, never by
// app.kubernetes.io/name. A cluster that sets spec.clusterSpec.nameOverride has
// app.kubernetes.io/name=<override>, so a name-based selector would silently
// exclude it from decommissioning entirely.
func TestRedpandaDecommissionerSelector(t *testing.T) {
	selector, err := redpandaDecommissionerSelector()
	require.NoError(t, err)

	cases := []struct {
		name   string
		labels map[string]string
		match  bool
	}{
		{
			name:   "default name label",
			labels: map[string]string{pkglabels.NameKey: "redpanda", pkglabels.InstanceKey: "redpanda"},
			match:  true,
		},
		{
			name:   "nameOverride name label still matches",
			labels: map[string]string{pkglabels.NameKey: "my-override", pkglabels.InstanceKey: "my-release"},
			match:  true,
		},
		{
			name:   "instance label present without name label",
			labels: map[string]string{pkglabels.InstanceKey: "my-release"},
			match:  true,
		},
		{
			name:   "no instance label is excluded",
			labels: map[string]string{pkglabels.NameKey: "redpanda"},
			match:  false,
		},
		{
			name:   "no labels at all is excluded",
			labels: map[string]string{},
			match:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.match, selector.Matches(labels.Set(tc.labels)))
		})
	}
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

	// Regression for the nameOverride finding: a cluster that sets
	// spec.clusterSpec.nameOverride has app.kubernetes.io/name=<override> on its
	// StatefulSets, but the instance label (and thus ownership resolution) is
	// unaffected. The adapter must still resolve it, sum its pools, and pass the
	// filter.
	t.Run("resolves a cluster that sets nameOverride", func(t *testing.T) {
		rp := readyRedpanda("my-release", "ns")
		base := redpandaSTSWithName("my-release", "ns", "my-release", "my-override", 3)
		poolA := redpandaSTSWithName("my-release-pool-a", "ns", "my-release", "my-override", 2)

		c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).
			WithObjects(rp, base, poolA).Build()
		adapter := &redpandaDecommissionerAdapter{client: c}

		got, err := adapter.getRedpanda(ctx, base)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "my-release", got.Name)

		replicas, err := adapter.desiredReplicas(ctx, base)
		require.NoError(t, err)
		assert.Equal(t, int32(5), replicas, "should sum base(3) + pool-a(2) regardless of nameOverride")

		ok, err := adapter.filter(ctx, base)
		require.NoError(t, err)
		assert.True(t, ok)
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

		// The decommissioner must NOT gate on readiness/observed-generation: a
		// scale-down keeps the cluster non-Ready (generation unobserved) until
		// the excess broker is decommissioned, so gating there would deadlock
		// the decommission. The decommissioner's own admin-API health check
		// guards against premature action.
		t.Run("does not gate on a not-Ready cluster", func(t *testing.T) {
			rp := readyRedpanda("redpanda", "ns")
			rp.Status.Conditions[0].Status = metav1.ConditionFalse
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(rp, sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.True(t, ok)
		})

		t.Run("does not gate on an unobserved generation", func(t *testing.T) {
			rp := readyRedpanda("redpanda", "ns")
			rp.Generation = 2 // status still observes generation 1 (mid scale-down)
			c := fake.NewClientBuilder().WithScheme(controller.V2Scheme).WithObjects(rp, sts).Build()
			adapter := &redpandaDecommissionerAdapter{client: c}

			ok, err := adapter.filter(ctx, sts)
			require.NoError(t, err)
			assert.True(t, ok)
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
