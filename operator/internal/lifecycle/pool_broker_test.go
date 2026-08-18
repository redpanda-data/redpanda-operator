// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

func brokerBackedPool(name string, specReplicas, pods int32) *poolWithOrdinals {
	pool := &poolWithOrdinals{
		set: &MulticlusterStatefulSet{
			clusterName:          mcmanager.LocalCluster,
			canonicalClusterName: "canonical-1",
			StatefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(specReplicas)},
				Status:     appsv1.StatefulSetStatus{Replicas: pods},
			},
		},
		brokerBacked: true,
	}
	for i := range pods {
		pool.pods = append(pool.pods, &podsWithOrdinals{
			ordinal: int(i),
			pod:     &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod"}},
		})
	}
	return pool
}

// TestPoolTrackerBrokerBackedPools locks in the contract of broker-backed
// pool facades: they participate in status/readiness/scale accounting but are
// invisible to every StatefulSet mutation planner and revision-based roll
// helper — their lifecycle belongs to the brokerset machinery.
func TestPoolTrackerBrokerBackedPools(t *testing.T) {
	desired := &MulticlusterStatefulSet{
		clusterName:          mcmanager.LocalCluster,
		canonicalClusterName: "canonical-1",
		StatefulSet: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
			Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(5))},
		},
	}

	t.Run("excluded from StatefulSet planners", func(t *testing.T) {
		tracker := NewPoolTracker(1, false)
		// 3 broker CRs vs 5 desired (would be a scale-up), stale labels
		// (would require an update), and 3 pods (rollable).
		tracker.addExisting(brokerBackedPool("pool-1", 3, 3))
		tracker.addDesired(desired)
		tracker.MarkClusterObserved(mcmanager.LocalCluster)

		require.Empty(t, tracker.ToCreate(), "existing broker-backed pool must not be re-created")
		require.Empty(t, tracker.ToScaleUp())
		require.Empty(t, tracker.RequiresUpdate())
		require.Empty(t, tracker.ToScaleDown())
		require.Empty(t, tracker.ToDelete())
		require.Empty(t, tracker.PodsToRoll())
		require.False(t, tracker.HasRecentlyReplacedPods())
	})

	t.Run("excluded from scale-down and deletion when pool is removed", func(t *testing.T) {
		tracker := NewPoolTracker(1, false)
		tracker.addExisting(brokerBackedPool("pool-1", 3, 3))
		tracker.MarkClusterObserved(mcmanager.LocalCluster)

		require.Empty(t, tracker.ToScaleDown())
		require.Empty(t, tracker.ToDelete())

		removed := tracker.BrokerBackedPoolsWithoutDesired()
		require.Len(t, removed, 1)
		require.Equal(t, "pool-1", removed[0].Name)
	})

	t.Run("removed pool requires cluster observation", func(t *testing.T) {
		tracker := NewPoolTracker(1, false)
		tracker.addExisting(brokerBackedPool("pool-1", 3, 3))

		require.Empty(t, tracker.BrokerBackedPoolsWithoutDesired(),
			"an unobserved cluster's missing desired state must not read as intent to drain")
	})

	t.Run("participates in scale and readiness accounting", func(t *testing.T) {
		tracker := NewPoolTracker(1, false)
		pool := brokerBackedPool("pool-1", 2, 3)
		pool.set.Status.ReadyReplicas = 2
		tracker.addExisting(pool)
		tracker.addDesired(desired)
		tracker.MarkClusterObserved(mcmanager.LocalCluster)

		require.True(t, tracker.AnyReady())
		// spec replicas (2) != pods (3): a decommission is in flight.
		require.False(t, tracker.CheckScale(t.Context()))

		statuses := tracker.PoolStatuses()
		require.Len(t, statuses, 1)
		require.Equal(t, int32(1), statuses[0].CondemnedReplicas)
	})

	t.Run("DesiredPools returns copies", func(t *testing.T) {
		tracker := NewPoolTracker(1, false)
		tracker.addDesired(desired)

		pools := tracker.DesiredPools()
		require.Len(t, pools, 1)
		pools[0].Spec.Replicas = ptr.To(int32(1))

		again := tracker.DesiredPools()
		require.Equal(t, int32(5), ptr.Deref(again[0].Spec.Replicas, 0))
	})
}
