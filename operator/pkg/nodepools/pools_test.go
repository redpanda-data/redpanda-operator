// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package nodepools_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/nodepools"
)

// TestGetNodePoolsWithBrokerBacked pins the broker-mode deleted-pool
// reconstruction: a pool removed from spec.nodePools after its migration has
// no StatefulSet left to synthesize it from, so it must be reconstructed
// from its Broker CRs — otherwise the pool becomes invisible and its brokers
// are never drained.
func TestGetNodePoolsWithBrokerBacked(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: types.UID("owner-uid")},
		Spec: vectorizedv1alpha1.ClusterSpec{
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{Name: "blue", Replicas: ptr.To(int32(3))},
			},
		},
	}

	broker := func(name, pool string, owned bool) *redpandav1alpha2.Broker {
		b := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test",
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "rp",
					"app.kubernetes.io/name":      "redpanda",
					"app.kubernetes.io/component": "redpanda",
					labels.NodePoolKey:            pool,
				},
			},
		}
		if owned {
			b.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "redpanda.vectorized.io/v1alpha1",
				Kind:       "Cluster",
				Name:       "rp",
				UID:        cluster.UID,
				Controller: ptr.To(true),
			}}
		}
		return b
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		cluster,
		// Two brokers of the removed "green" pool: exactly one virtual pool.
		broker("rp-green-a", "green", true),
		broker("rp-green-b", "green", true),
		// A broker of a pool still in the spec: no extra virtual pool.
		broker("rp-blue-a", "blue", true),
		// A foreign broker with matching labels but a different owner.
		broker("other-red", "red", false),
	).Build()

	pools, err := nodepools.GetNodePoolsWithBrokerBacked(ctx, cluster, c)
	require.NoError(t, err)

	byName := map[string]*vectorizedv1alpha1.NodePoolSpecWithDeleted{}
	for _, p := range pools {
		byName[p.Name] = p
	}
	require.Len(t, pools, 2, "expected spec pool + one reconstructed deleted pool, got %v", byName)

	require.Contains(t, byName, "blue")
	require.False(t, byName["blue"].Deleted)

	require.Contains(t, byName, "green", "removed broker-backed pool must be reconstructed")
	require.True(t, byName["green"].Deleted)
	require.Equal(t, int32(0), *byName["green"].Replicas, "deleted pools drain to zero")

	require.NotContains(t, byName, "red", "brokers of a different owner must be ignored")
}
