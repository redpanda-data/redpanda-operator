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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// TestOwnerReferenceForCluster pins the GC owner-reference builder used for the
// opt-in auto-cleanup: deleting the referenced cluster CR should cascade-delete
// the layered CR. The reference must (a) carry the correct GVK for v1/v2/stretch
// refs, (b) be set only when the cluster lives in the same namespace as the
// object (Kubernetes GC ignores cross-namespace owner refs), and (c) never block
// the cluster's own deletion (controller=false, blockOwnerDeletion=false).
func TestOwnerReferenceForCluster(t *testing.T) {
	const ns = "team-a"

	v2Cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: ns, UID: types.UID("uid-v2")},
	}
	v1Cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: ns, UID: types.UID("uid-v1")},
	}

	v2Ref := &redpandav1alpha2.ClusterRef{Name: "rp"} // group/kind default to v2
	v1Ref := &redpandav1alpha2.ClusterRef{
		Name:  "legacy",
		Group: ptr.To("redpanda.vectorized.io"),
		Kind:  ptr.To("Cluster"),
	}

	tests := []struct {
		name            string
		ref             *redpandav1alpha2.ClusterRef
		cluster         client.Object
		objectNamespace string
		wantNil         bool
		wantAPIVersion  string
		wantKind        string
		wantName        string
		wantUID         types.UID
	}{
		{
			name:            "v2 Redpanda, same namespace",
			ref:             v2Ref,
			cluster:         v2Cluster,
			objectNamespace: ns,
			wantAPIVersion:  "cluster.redpanda.com/v1alpha2",
			wantKind:        "Redpanda",
			wantName:        "rp",
			wantUID:         types.UID("uid-v2"),
		},
		{
			name:            "v1 vectorized Cluster, same namespace",
			ref:             v1Ref,
			cluster:         v1Cluster,
			objectNamespace: ns,
			wantAPIVersion:  "redpanda.vectorized.io/v1alpha1",
			wantKind:        "Cluster",
			wantName:        "legacy",
			wantUID:         types.UID("uid-v1"),
		},
		{
			name:            "cross-namespace yields no owner ref",
			ref:             v2Ref,
			cluster:         v2Cluster,
			objectNamespace: "other-team",
			wantNil:         true,
		},
		{
			name:            "nil cluster yields no owner ref",
			ref:             v2Ref,
			cluster:         nil,
			objectNamespace: ns,
			wantNil:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ownerReferenceForCluster(tt.ref, tt.cluster, tt.objectNamespace)
			if tt.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.wantAPIVersion, ptr.Deref(got.APIVersion, ""))
			require.Equal(t, tt.wantKind, ptr.Deref(got.Kind, ""))
			require.Equal(t, tt.wantName, ptr.Deref(got.Name, ""))
			require.Equal(t, tt.wantUID, ptr.Deref(got.UID, ""))
			require.False(t, ptr.Deref(got.Controller, true), "owner ref must not be a controller ref")
			require.False(t, ptr.Deref(got.BlockOwnerDeletion, true), "owner ref must not block cluster deletion")
		})
	}
}
