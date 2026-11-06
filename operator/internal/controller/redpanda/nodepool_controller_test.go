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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

type testBroker struct {
	generation   string
	podScheduled metav1.Condition
	ready        bool
	configSynced bool
	decommission bool
}

func (t testBroker) build() redpandav1alpha2.Broker {
	b := redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{},
	}
	if t.generation != "" {
		b.Labels = map[string]string{redpandachart.NodePoolLabelGeneration: t.generation}
	}
	b.Spec.Decommission = t.decommission

	conditionStatus := func(v bool) metav1.ConditionStatus {
		if v {
			return metav1.ConditionTrue
		}
		return metav1.ConditionFalse
	}
	b.Status.Conditions = []metav1.Condition{
		t.podScheduled,
		{Type: statuses.BrokerReady, Status: conditionStatus(t.ready), Reason: string(statuses.BrokerReadyReasonReady)},
		{Type: statuses.BrokerConfigSynced, Status: conditionStatus(t.configSynced), Reason: string(statuses.BrokerConfigSyncedReasonSynced)},
	}
	return b
}

func podScheduled(status metav1.ConditionStatus, reason statuses.BrokerPodScheduledCondition) metav1.Condition {
	return metav1.Condition{Type: statuses.BrokerPodScheduled, Status: status, Reason: string(reason)}
}

// TestBrokersForPool pins the filters behind the status a pool derives from
// Broker CRs: the pool-name label alone must not attribute a same-named V1
// Cluster node pool's Brokers, or another Redpanda cluster's, to this pool.
func TestBrokersForPool(t *testing.T) {
	broker := func(name, cluster string, ref redpandav1alpha2.ClusterRef) redpandav1alpha2.Broker {
		return redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{
				APIVersion: redpandav1alpha2.GroupVersion.String(),
				Kind:       redpandav1alpha2.RedpandaKind,
				Name:       cluster,
				Controller: ptr.To(true),
			}}},
			Spec: redpandav1alpha2.BrokerSpec{ClusterRef: ref},
		}
	}
	nodePoolRef := func(pool string) redpandav1alpha2.ClusterRef {
		return redpandav1alpha2.ClusterRef{Name: pool, Kind: ptr.To(redpandav1alpha2.NodePoolKind)}
	}

	mine := broker("pool-1-broker-0", "rp", nodePoolRef("pool-1"))
	v1Twin := broker("pool-1-broker-1", "rp", redpandav1alpha2.ClusterRef{
		Name: "pool-1", Group: ptr.To("redpanda.vectorized.io"), Kind: ptr.To("Cluster"),
	})
	otherPool := broker("pool-2-broker-0", "rp", nodePoolRef("pool-2"))
	otherCluster := broker("pool-1-broker-2", "rp-2", nodePoolRef("pool-1"))
	brokerPoolTwin := broker("pool-1-broker-3", "rp", redpandav1alpha2.ClusterRef{
		Name: "pool-1", Kind: ptr.To(redpandav1alpha2.BrokerPoolKind),
	})

	all := []redpandav1alpha2.Broker{mine, v1Twin, otherPool, otherCluster, brokerPoolTwin}
	require.Equal(t, []redpandav1alpha2.Broker{mine}, brokersForPool(all, "rp", "pool-1", (*redpandav1alpha2.ClusterRef).IsNodePool),
		"only Brokers of this cluster whose clusterRef names this NodePool belong to it; label matches alone do not")
	require.Equal(t, []redpandav1alpha2.Broker{brokerPoolTwin}, brokersForPool(all, "rp", "pool-1", (*redpandav1alpha2.ClusterRef).IsBrokerPool),
		"after a handoff the Brokers name the RedpandaBrokerPool instead")
}

func TestBrokerPoolPendingInCache(t *testing.T) {
	now := metav1.Now()
	nodePool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "ns"},
		Spec:       redpandav1alpha2.NodePoolSpec{ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp"}},
	}
	brokerPool := func(mutate ...func(*redpandav1alpha2.RedpandaBrokerPool)) *redpandav1alpha2.RedpandaBrokerPool {
		bp := &redpandav1alpha2.RedpandaBrokerPool{
			ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "ns"},
			Spec:       redpandav1alpha2.BrokerPoolSpec{ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp"}},
		}
		for _, m := range mutate {
			m(bp)
		}
		return bp
	}

	for name, tc := range map[string]struct {
		live, cached *redpandav1alpha2.RedpandaBrokerPool
		want         bool
	}{
		"no RedpandaBrokerPool deletes the pool": {},
		"one not yet cached holds the NodePool":  {live: brokerPool(), want: true},
		"one cached releases the NodePool":       {live: brokerPool(), cached: brokerPool()},
		"one of another cluster takes nothing": {live: brokerPool(func(bp *redpandav1alpha2.RedpandaBrokerPool) {
			bp.Spec.ClusterRef.Name = "rp-2"
		})},
		"one of a stretch cluster takes nothing": {live: brokerPool(func(bp *redpandav1alpha2.RedpandaBrokerPool) {
			bp.Spec.ClusterRef.Kind = ptr.To(redpandav1alpha2.StretchClusterRefKind)
		})},
		"one being deleted takes nothing": {live: brokerPool(func(bp *redpandav1alpha2.RedpandaBrokerPool) {
			bp.DeletionTimestamp = &now
			bp.Finalizers = []string{FinalizerKey}
		})},
	} {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, redpandav1alpha2.Install(scheme))
			build := func(bp *redpandav1alpha2.RedpandaBrokerPool) client.Client {
				builder := fake.NewClientBuilder().WithScheme(scheme)
				if bp != nil {
					builder = builder.WithObjects(bp)
				}
				return builder.Build()
			}

			got, err := brokerPoolPendingInCache(t.Context(), build(tc.live), build(tc.cached), nodePool)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBrokerBackedPoolStatus(t *testing.T) {
	healthy := testBroker{
		generation:   "5",
		podScheduled: podScheduled(metav1.ConditionTrue, statuses.BrokerPodScheduledReasonScheduled),
		ready:        true,
		configSynced: true,
	}

	t.Run("all healthy reports Deployed", func(t *testing.T) {
		embedded, generation, reason := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), healthy.build(),
		})
		require.Equal(t, poolDeployed, reason)
		require.Equal(t, int64(5), generation)
		require.Equal(t, redpandav1alpha2.EmbeddedNodePoolStatus{
			Name:             "pool-1",
			Replicas:         3,
			DesiredReplicas:  3,
			ReadyReplicas:    3,
			RunningReplicas:  3,
			UpToDateReplicas: 3,
		}, embedded)
	})

	t.Run("mid-rotation pod gap reports Scaling", func(t *testing.T) {
		rotating := healthy
		rotating.podScheduled = podScheduled(metav1.ConditionFalse, statuses.BrokerPodScheduledReasonPodMissing)
		rotating.ready = false
		rotating.configSynced = false

		embedded, _, reason := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), rotating.build(),
		})
		require.Equal(t, poolScaling, reason)
		require.Equal(t, int32(2), embedded.Replicas)
		require.Equal(t, int32(2), embedded.ReadyReplicas)
		require.Equal(t, int32(2), embedded.UpToDateReplicas)
		require.Equal(t, int32(0), embedded.OutOfDateReplicas)
	})

	t.Run("unschedulable pod still counts as a replica", func(t *testing.T) {
		unschedulable := healthy
		unschedulable.podScheduled = podScheduled(metav1.ConditionFalse, statuses.BrokerPodScheduledReasonUnschedulable)
		unschedulable.ready = false

		embedded, _, reason := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), unschedulable.build(),
		})
		require.Equal(t, poolDeployed, reason)
		require.Equal(t, int32(3), embedded.Replicas)
		require.Equal(t, int32(2), embedded.ReadyReplicas)
	})

	t.Run("decommissioning broker is condemned and outdated pod counted", func(t *testing.T) {
		condemned := healthy
		condemned.decommission = true
		condemned.configSynced = false

		embedded, _, reason := brokerBackedPoolStatus("pool-1", 2, []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), condemned.build(),
		})
		require.Equal(t, poolScaling, reason)
		require.Equal(t, int32(3), embedded.Replicas)
		require.Equal(t, int32(2), embedded.DesiredReplicas)
		require.Equal(t, int32(1), embedded.CondemnedReplicas)
		require.Equal(t, int32(1), embedded.OutOfDateReplicas)
	})

	t.Run("unreconciled brokers count nothing", func(t *testing.T) {
		unreconciled := testBroker{
			podScheduled: podScheduled(metav1.ConditionUnknown, "NotReconciled"),
		}

		embedded, generation, reason := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			unreconciled.build(), unreconciled.build(), unreconciled.build(),
		})
		require.Equal(t, poolScaling, reason)
		require.Equal(t, int64(-1), generation)
		require.Equal(t, int32(0), embedded.Replicas)
		require.Equal(t, int32(0), embedded.ReadyReplicas)
	})

	t.Run("deployed generation is the minimum across brokers", func(t *testing.T) {
		older := healthy
		older.generation = "4"

		_, generation, _ := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			healthy.build(), older.build(), healthy.build(),
		})
		require.Equal(t, int64(4), generation)
	})

	t.Run("disk-lost tombstones are excluded", func(t *testing.T) {
		tombstone := healthy.build()
		tombstone.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{ResourcesReleased: true}

		embedded, _, reason := brokerBackedPoolStatus("pool-1", 3, []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), healthy.build(), tombstone,
		})
		require.Equal(t, poolDeployed, reason,
			"a tombstone+replacement pair at one index must not read as a scale-up")
		require.Equal(t, int32(3), embedded.Replicas)
	})

	t.Run("quiesced synthesis is idempotent", func(t *testing.T) {
		brokers := []redpandav1alpha2.Broker{healthy.build(), healthy.build(), healthy.build()}
		first, firstGen, firstReason := brokerBackedPoolStatus("pool-1", 3, brokers)
		second, secondGen, secondReason := brokerBackedPoolStatus("pool-1", 3, brokers)
		require.Equal(t, first, second)
		require.Equal(t, firstGen, secondGen)
		require.Equal(t, firstReason, secondReason)
	})
}
