// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// The headless ClusterIP Service is cluster-wide (one per member K8s
// cluster) so its annotations belong on StretchCluster.spec, not on
// individual NodePools. Restoring this surface closes the regression
// Rafał flagged — the prior `spec.service.internal.annotations` had no
// equivalent after the service field was removed wholesale.
func TestServiceInternal_AppliesInternalServiceAnnotations(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
		Spec: redpandav1alpha2.StretchClusterSpec{
			InternalServiceAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "redpanda.example.com",
				"custom.example.com/key":                    "value",
			},
		},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	}

	state, err := NewRenderState(nil, sc, []*redpandav1alpha2.NodePool{pool}, []*redpandav1alpha2.NodePool{pool}, "test")
	require.NoError(t, err)

	svc := serviceInternal(state)
	require.NotNil(t, svc)
	require.Equal(t,
		map[string]string{
			"external-dns.alpha.kubernetes.io/hostname": "redpanda.example.com",
			"custom.example.com/key":                    "value",
		},
		svc.Annotations,
		"the headless Service must carry the cluster-wide internal-service annotations verbatim",
	)
}

// Annotations default to nil when the user didn't supply any — the
// renderer must not write an empty map literal that downstream
// comparisons might treat as a meaningful object.
func TestServiceInternal_NoAnnotationsWhenUnset(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	}

	state, err := NewRenderState(nil, sc, []*redpandav1alpha2.NodePool{pool}, []*redpandav1alpha2.NodePool{pool}, "test")
	require.NoError(t, err)

	svc := serviceInternal(state)
	require.NotNil(t, svc)
	require.Nil(t, svc.Annotations, "unset InternalServiceAnnotations must not materialize an empty map on the Service")
}
