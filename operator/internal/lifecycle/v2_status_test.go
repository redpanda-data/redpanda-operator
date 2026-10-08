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

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestPrunePools(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		pools     []redpandav1alpha2.EmbeddedNodePoolStatus
		current   []PoolStatus
		wantDirty bool
		wantNames []string
	}{
		"retired pool is dropped": {
			pools:     []redpandav1alpha2.EmbeddedNodePoolStatus{{Name: "a"}, {Name: "gone"}},
			current:   []PoolStatus{{Name: "a"}},
			wantDirty: true,
			wantNames: []string{"a"},
		},
		// A pool mid-scale-down keeps its StatefulSet, so it stays in current
		// even at zero ready replicas. Pruning it would hide the drain.
		"draining pool is kept": {
			pools:     []redpandav1alpha2.EmbeddedNodePoolStatus{{Name: "a", Replicas: 1}},
			current:   []PoolStatus{{Name: "a", ReadyReplicas: 0, CondemnedReplicas: 1}},
			wantDirty: false,
			wantNames: []string{"a"},
		},
		"nothing vanished is not dirty": {
			pools:     []redpandav1alpha2.EmbeddedNodePoolStatus{{Name: "a"}, {Name: "b"}},
			current:   []PoolStatus{{Name: "b"}, {Name: "a"}},
			wantDirty: false,
			wantNames: []string{"a", "b"},
		},
		// Pools were not determined this pass. Pruning here would wipe the
		// whole status.
		"empty current prunes nothing": {
			pools:     []redpandav1alpha2.EmbeddedNodePoolStatus{{Name: "a"}, {Name: "b"}},
			current:   nil,
			wantDirty: false,
			wantNames: []string{"a", "b"},
		},
		"empty status stays empty": {
			pools:     nil,
			current:   []PoolStatus{{Name: "a"}},
			wantDirty: false,
			wantNames: nil,
		},
		// current is map-ordered, so the kept entries must hold their own order.
		"surviving entries keep their order": {
			pools:     []redpandav1alpha2.EmbeddedNodePoolStatus{{Name: "a"}, {Name: "gone"}, {Name: "b"}},
			current:   []PoolStatus{{Name: "b"}, {Name: "a"}},
			wantDirty: true,
			wantNames: []string{"a", "b"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pools := tt.pools
			require.Equal(t, tt.wantDirty, prunePools(&pools, tt.current))

			var names []string
			for _, pool := range pools {
				names = append(names, pool.Name)
			}
			require.Equal(t, tt.wantNames, names)

			// Converged input must require no further mutation, or the status
			// write retriggers reconciliation forever.
			require.False(t, prunePools(&pools, tt.current))
		})
	}
}

// TestV2ClusterStatusUpdaterPrunesRetiredPools reproduces CIAINFRA-5653: after a
// blue/green pool migration the blue entries lingered in status.nodePools, so
// metric_controller summed redpanda_ready_nodes to 4 against 3 desired.
func TestV2ClusterStatusUpdaterPrunesRetiredPools(t *testing.T) {
	t.Parallel()

	cluster := NewClusterWithPools(&redpandav1alpha2.Redpanda{})
	cluster.Status.NodePools = []redpandav1alpha2.EmbeddedNodePoolStatus{
		{Name: "redpanda-broker", Replicas: 3, DesiredReplicas: 3, ReadyReplicas: 3, RunningReplicas: 3},
		{Name: "redpanda-broker-blue-a", Replicas: 1, CondemnedReplicas: 1, ReadyReplicas: 1, RunningReplicas: 1},
		{Name: "redpanda-broker-blue-b"},
		{Name: "redpanda-broker-blue-c"},
	}

	status := NewClusterStatus()
	status.Pools = []PoolStatus{
		{Name: "redpanda-broker", Replicas: 3, DesiredReplicas: 3, ReadyReplicas: 3, RunningReplicas: 3},
		{Name: "redpanda-broker-green-a"},
		{Name: "redpanda-broker-green-b"},
		{Name: "redpanda-broker-green-c"},
	}

	updater := NewV2ClusterStatusUpdater()
	require.True(t, updater.Update(cluster, status))

	var names []string
	var desired, ready int32
	for _, pool := range cluster.Status.NodePools {
		names = append(names, pool.Name)
		desired += pool.DesiredReplicas
		ready += pool.ReadyReplicas
	}
	require.Equal(t, []string{
		"redpanda-broker",
		"redpanda-broker-green-a",
		"redpanda-broker-green-b",
		"redpanda-broker-green-c",
	}, names)
	require.Equal(t, int32(3), ready, "redpanda_ready_nodes must not exceed redpanda_desired_nodes")
	require.Equal(t, int32(3), desired)

	// Second pass on the same input must report nothing to write.
	require.False(t, updater.Update(cluster, status))
}
