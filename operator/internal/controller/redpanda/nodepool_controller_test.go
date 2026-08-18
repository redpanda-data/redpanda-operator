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
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
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
		b.Labels = map[string]string{redpanda.NodePoolLabelGeneration: t.generation}
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

func TestBrokerBackedPoolStatus(t *testing.T) {
	pool := func(replicas int32) *redpandav1alpha2.NodePool {
		return &redpandav1alpha2.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
			Spec: redpandav1alpha2.NodePoolSpec{
				EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{Replicas: ptr.To(replicas)},
			},
		}
	}

	healthy := testBroker{
		generation:   "5",
		podScheduled: podScheduled(metav1.ConditionTrue, statuses.BrokerPodScheduledReasonScheduled),
		ready:        true,
		configSynced: true,
	}

	t.Run("all healthy reports Deployed", func(t *testing.T) {
		embedded, generation, reason := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), healthy.build(),
		})
		require.Equal(t, statuses.NodePoolDeployedReasonDeployed, reason)
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

		embedded, _, reason := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), rotating.build(),
		})
		require.Equal(t, statuses.NodePoolDeployedReasonScaling, reason)
		require.Equal(t, int32(2), embedded.Replicas)
		require.Equal(t, int32(2), embedded.ReadyReplicas)
		require.Equal(t, int32(2), embedded.UpToDateReplicas)
		require.Equal(t, int32(0), embedded.OutOfDateReplicas)
	})

	t.Run("unschedulable pod still counts as a replica", func(t *testing.T) {
		unschedulable := healthy
		unschedulable.podScheduled = podScheduled(metav1.ConditionFalse, statuses.BrokerPodScheduledReasonUnschedulable)
		unschedulable.ready = false

		embedded, _, reason := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), unschedulable.build(),
		})
		require.Equal(t, statuses.NodePoolDeployedReasonDeployed, reason)
		require.Equal(t, int32(3), embedded.Replicas)
		require.Equal(t, int32(2), embedded.ReadyReplicas)
	})

	t.Run("decommissioning broker is condemned and outdated pod counted", func(t *testing.T) {
		condemned := healthy
		condemned.decommission = true
		condemned.configSynced = false

		embedded, _, reason := brokerBackedPoolStatus(pool(2), []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), condemned.build(),
		})
		require.Equal(t, statuses.NodePoolDeployedReasonScaling, reason)
		require.Equal(t, int32(3), embedded.Replicas)
		require.Equal(t, int32(2), embedded.DesiredReplicas)
		require.Equal(t, int32(1), embedded.CondemnedReplicas)
		require.Equal(t, int32(1), embedded.OutOfDateReplicas)
	})

	t.Run("unreconciled brokers count nothing", func(t *testing.T) {
		unreconciled := testBroker{
			podScheduled: podScheduled(metav1.ConditionUnknown, "NotReconciled"),
		}

		embedded, generation, reason := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			unreconciled.build(), unreconciled.build(), unreconciled.build(),
		})
		require.Equal(t, statuses.NodePoolDeployedReasonScaling, reason)
		require.Equal(t, int64(-1), generation)
		require.Equal(t, int32(0), embedded.Replicas)
		require.Equal(t, int32(0), embedded.ReadyReplicas)
	})

	t.Run("deployed generation is the minimum across brokers", func(t *testing.T) {
		older := healthy
		older.generation = "4"

		_, generation, _ := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			healthy.build(), older.build(), healthy.build(),
		})
		require.Equal(t, int64(4), generation)
	})

	t.Run("disk-lost tombstones are excluded", func(t *testing.T) {
		tombstone := healthy.build()
		tombstone.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{ResourcesReleased: true}

		embedded, _, reason := brokerBackedPoolStatus(pool(3), []redpandav1alpha2.Broker{
			healthy.build(), healthy.build(), healthy.build(), tombstone,
		})
		require.Equal(t, statuses.NodePoolDeployedReasonDeployed, reason,
			"a tombstone+replacement pair at one index must not read as a scale-up")
		require.Equal(t, int32(3), embedded.Replicas)
	})

	t.Run("quiesced synthesis is idempotent", func(t *testing.T) {
		brokers := []redpandav1alpha2.Broker{healthy.build(), healthy.build(), healthy.build()}
		first, firstGen, firstReason := brokerBackedPoolStatus(pool(3), brokers)
		second, secondGen, secondReason := brokerBackedPoolStatus(pool(3), brokers)
		require.Equal(t, first, second)
		require.Equal(t, firstGen, secondGen)
		require.Equal(t, firstReason, secondReason)
	})
}
