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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
