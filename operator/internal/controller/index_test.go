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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestIndexByClusterSource(t *testing.T) {
	v2 := func(cr *redpandav1alpha2.ClusterRef) bool { return cr.IsV2() }

	// A ShadowLink in namespace "b" implements both ClusterReferencingObject
	// (shadowCluster) and RemoteClusterReferencingObject (sourceCluster).
	link := func(shadow, source *redpandav1alpha2.ClusterRef) *redpandav1alpha2.ShadowLink {
		return &redpandav1alpha2.ShadowLink{
			ObjectMeta: metav1.ObjectMeta{Namespace: "b", Name: "link"},
			Spec: redpandav1alpha2.ShadowLinkSpec{
				ShadowCluster: &redpandav1alpha2.ClusterSource{ClusterRef: shadow},
				SourceCluster: &redpandav1alpha2.ClusterSource{ClusterRef: source},
			},
		}
	}

	v1Ref := &redpandav1alpha2.ClusterRef{
		Name:  "redpanda-target",
		Group: ptr.To("redpanda.vectorized.io"),
		Kind:  ptr.To("Cluster"),
	}

	for name, tc := range map[string]struct {
		obj      client.Object
		expected []string
	}{
		"cross-namespace remote ref is keyed by the ref namespace": {
			obj: link(
				&redpandav1alpha2.ClusterRef{Name: "redpanda-target"},
				&redpandav1alpha2.ClusterRef{Name: "redpanda-source", Namespace: ptr.To("a")},
			),
			expected: []string{"b/redpanda-target", "a/redpanda-source"},
		},
		"refs without a namespace fall back to the object namespace": {
			obj: link(
				&redpandav1alpha2.ClusterRef{Name: "redpanda-target"},
				&redpandav1alpha2.ClusterRef{Name: "redpanda-source"},
			),
			expected: []string{"b/redpanda-target", "b/redpanda-source"},
		},
		"remote branch filters on the remote ref, not the local ref": {
			// The shadow (local) ref is V1 so the V2 index skips it, but the
			// source (remote) ref is V2 and must still be indexed. Guards the
			// bug where the remote branch checked the local clusterRef.
			obj: link(
				v1Ref,
				&redpandav1alpha2.ClusterRef{Name: "redpanda-source", Namespace: ptr.To("a")},
			),
			expected: []string{"a/redpanda-source"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.ElementsMatch(t, tc.expected, indexByClusterSource(v2)(tc.obj))
		})
	}
}

func TestPoolsForCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.Install(scheme))

	ref := redpandav1alpha2.ClusterRef{Name: "rp"}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&redpandav1alpha2.NodePool{}, clusterReferenceIndexName(PoolIndex), v2ClusterKeys).
		WithIndex(&redpandav1alpha2.RedpandaBrokerPool{}, clusterReferenceIndexName(PoolIndex), v2ClusterKeys).
		WithObjects(
			&redpandav1alpha2.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "ns"},
				Spec:       redpandav1alpha2.NodePoolSpec{ClusterRef: ref},
			},
			&redpandav1alpha2.RedpandaBrokerPool{
				ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns"},
				Spec:       redpandav1alpha2.BrokerPoolSpec{ClusterRef: ref},
			},
			// Another cluster's pool, and a pool naming another namespace.
			&redpandav1alpha2.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns"},
				Spec:       redpandav1alpha2.NodePoolSpec{ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp-2"}},
			},
			&redpandav1alpha2.RedpandaBrokerPool{
				ObjectMeta: metav1.ObjectMeta{Name: "elsewhere", Namespace: "ns-2"},
				Spec:       redpandav1alpha2.BrokerPoolSpec{ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp", Namespace: ptr.To("ns")}},
			},
		).Build()

	pools, err := PoolsForCluster(t.Context(), c, types.NamespacedName{Namespace: "ns", Name: "rp"})
	require.NoError(t, err)

	var got []string
	for _, p := range pools {
		ref := p.Ref()
		got = append(got, fmt.Sprintf("%s:%s", p.BrokerPool.Name, ref.GetKind()))
	}
	require.Equal(t, []string{"blue:NodePool", "green:RedpandaBrokerPool"}, got)
}
