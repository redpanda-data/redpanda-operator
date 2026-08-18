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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestNodePoolsToBrokerPools(t *testing.T) {
	np := testNodePool()

	converted, err := nodePoolsToBrokerPools([]*redpandav1alpha2.NodePool{np})
	require.NoError(t, err)
	require.Len(t, converted, 1)

	bp := converted[0]

	// ObjectMeta carries over verbatim: Name and Generation drive the rendered
	// StatefulSet name, identity labels, and pool-scoped affinity selectors.
	require.Equal(t, np.ObjectMeta, bp.ObjectMeta)
	require.Equal(t, np.Spec.ClusterRef, bp.Spec.ClusterRef)

	// The pool spec is carried across onto the widened type. Shared
	// types/values are cloned not copied.
	require.Equal(t, np.Spec.Replicas, bp.Spec.Replicas)
	require.Equal(t, np.Spec.AdditionalSelectorLabels, bp.Spec.AdditionalSelectorLabels)
	require.NotSame(t, &np.Spec.AdditionalSelectorLabels, &bp.Spec.AdditionalSelectorLabels)
	require.Equal(t, np.Spec.AdditionalRedpandaCmdFlags, bp.Spec.AdditionalRedpandaCmdFlags)
	require.NotSame(t, &np.Spec.AdditionalRedpandaCmdFlags, &bp.Spec.AdditionalRedpandaCmdFlags)
	require.Equal(t, np.Spec.Image, bp.Spec.Image)
	require.NotSame(t, np.Spec.Image, bp.Spec.Image)
}

// testNodePool returns a NodePool whose every mutable field is reachable through
// a pointer, map, or slice, so the aliasing assertions below have something to
// bite on.
func testNodePool() *redpandav1alpha2.NodePool {
	return &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pool-a",
			Namespace:  "ns",
			Generation: 42,
			Labels:     map[string]string{"owner": "rp"},
		},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas:                   new(int32(3)),
				AdditionalSelectorLabels:   map[string]string{"selector": "label"},
				AdditionalRedpandaCmdFlags: []string{"--smp=2"},
				Image:                      &redpandav1alpha2.RedpandaImage{Repository: new("original"), Tag: new("v1")},
			},
			ClusterRef: redpandav1alpha2.ClusterRef{
				Group:     new("cluster.redpanda.com"),
				Kind:      new("Redpanda"),
				Name:      "rp",
				Namespace: new("ns"),
			},
		},
	}
}
