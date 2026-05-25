// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/fuzzing"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
)

func TestNodepoolConversion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		values := rapid.MakeCustom[redpanda.Values](rapidutil.KubernetesTypes).Draw(t, "values")
		pools := rapid.SliceOf(rapid.MakeCustom[redpandav1alpha2.NodePool](rapidutil.KubernetesTypes)).Draw(t, "pools")
		_, err := convertV2NodepoolsToPools(values, functional.MapFn(ptr.To, pools), &V2Defaulters{})
		require.NoError(t, err)
	})
}

// TestPersistentVolumeClaimRetentionPolicyPrecedence pins the precedence chain for
// persistentVolumeClaimRetentionPolicy: NodePool override > Redpanda CR cluster-level
// value > chart default. The chain is implicit in the order of convertV2Fields and
// convertV2NodepoolsToPools, so this test guards against silent regressions if those
// are re-ordered.
func TestPersistentVolumeClaimRetentionPolicyPrecedence(t *testing.T) {
	deletePolicy := func(scaled, deleted appsv1.PersistentVolumeClaimRetentionPolicyType) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
		return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenScaled:  scaled,
			WhenDeleted: deleted,
		}
	}
	clusterPolicy := deletePolicy(appsv1.DeletePersistentVolumeClaimRetentionPolicyType, appsv1.RetainPersistentVolumeClaimRetentionPolicyType)
	poolPolicy := deletePolicy(appsv1.RetainPersistentVolumeClaimRetentionPolicyType, appsv1.DeletePersistentVolumeClaimRetentionPolicyType)

	cases := []struct {
		name        string
		clusterSet  *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy
		poolSet     *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy
		wantCluster appsv1.PersistentVolumeClaimRetentionPolicyType
		wantScaled  appsv1.PersistentVolumeClaimRetentionPolicyType
		wantDeleted appsv1.PersistentVolumeClaimRetentionPolicyType
	}{
		{
			name:        "neither set -> chart default Retain/Retain",
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "cluster only -> pool inherits cluster",
			clusterSet:  clusterPolicy,
			wantScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "pool only -> pool wins over chart default",
			poolSet:     poolPolicy,
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "pool overrides cluster",
			clusterSet:  clusterPolicy,
			poolSet:     poolPolicy,
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Run convertV2Fields to populate the cluster-level value into state.Values,
			// mirroring what ConvertV2ToRenderState does before per-pool conversion.
			values := redpanda.Values{}
			clusterSpec := &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					PersistentVolumeClaimRetentionPolicy: tc.clusterSet,
				},
			}
			state := &redpanda.RenderState{Values: values}
			require.NoError(t, convertV2Fields(state, &state.Values, clusterSpec))

			pool := &redpandav1alpha2.NodePool{
				Spec: redpandav1alpha2.NodePoolSpec{
					EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
						PersistentVolumeClaimRetentionPolicy: tc.poolSet,
					},
				},
			}
			converted, err := convertV2NodepoolToPool(state.Values, pool, &V2Defaulters{})
			require.NoError(t, err)
			got := converted.Statefulset.PersistentVolumeClaimRetentionPolicy
			require.NotNil(t, got, "rendered pool should always carry a policy (chart default ensures this)")
			require.Equal(t, tc.wantScaled, got.WhenScaled)
			require.Equal(t, tc.wantDeleted, got.WhenDeleted)
		})
	}
}

func TestConvertV2Fields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		partialValues := rapid.MakeCustom[redpanda.PartialValues](fuzzing.ClusterSpecConfig()).Draw(t, "values")
		if partialValues.Storage != nil && partialValues.Storage.Tiered != nil && partialValues.Storage.Tiered.PersistentVolume != nil {
			partialValues.Storage.Tiered.PersistentVolume.Size = nil
		}
		values := redpanda.Values{}
		clusterSpec := &redpandav1alpha2.RedpandaClusterSpec{
			Affinity: &corev1.Affinity{},
			Statefulset: &redpandav1alpha2.Statefulset{
				PodAffinity: &corev1.PodAffinity{},
			},
		}
		marshaled, err := json.Marshal(partialValues)
		require.NoError(t, err)

		require.NoError(t, json.Unmarshal(marshaled, clusterSpec))
		require.NoError(t, json.Unmarshal(marshaled, &values))
		state := &redpanda.RenderState{
			Values: values,
		}
		err = convertV2Fields(state, &state.Values, clusterSpec)
		require.NoError(t, err)
	})
}
