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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
)

func TestResolvePools(t *testing.T) {
	now := metav1.Now()
	nodePool := func(name string, mutate ...func(*redpandav1alpha2.NodePool)) *redpandav1alpha2.NodePool {
		np := &redpandav1alpha2.NodePool{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name}}
		for _, m := range mutate {
			m(np)
		}
		return np
	}
	annotated := func(value string) func(*redpandav1alpha2.NodePool) {
		return func(np *redpandav1alpha2.NodePool) {
			np.Annotations = map[string]string{redpandav1alpha2.MigrateToBrokerPoolAnnotation: value}
		}
	}
	deleting := func(np *redpandav1alpha2.NodePool) { np.DeletionTimestamp = &now }
	brokerPool := func(name string) *redpandav1alpha2.RedpandaBrokerPool {
		return &redpandav1alpha2.RedpandaBrokerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name}}
	}

	for name, tc := range map[string]struct {
		nodePools   []*redpandav1alpha2.NodePool
		brokerPools []*redpandav1alpha2.RedpandaBrokerPool
		// want maps each resolved pool to its owner's kind, in order.
		want []string
	}{
		"no pools": {},
		"NodePools only": {
			nodePools: []*redpandav1alpha2.NodePool{nodePool("b"), nodePool("a")},
			want:      []string{"a:NodePool", "b:NodePool"},
		},
		"RedpandaBrokerPools only": {
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("a")},
			want:        []string{"a:RedpandaBrokerPool"},
		},
		"the NodePool keeps its pool until it releases it": {
			nodePools:   []*redpandav1alpha2.NodePool{nodePool("a")},
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("a")},
			want:        []string{"a:NodePool"},
		},
		"an annotation other than true keeps the pool": {
			nodePools:   []*redpandav1alpha2.NodePool{nodePool("a", annotated("false"))},
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("a")},
			want:        []string{"a:NodePool"},
		},
		"the annotation hands the pool over": {
			nodePools:   []*redpandav1alpha2.NodePool{nodePool("a", annotated("true"))},
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("a")},
			want:        []string{"a:RedpandaBrokerPool"},
		},
		"deletion hands the pool over": {
			nodePools:   []*redpandav1alpha2.NodePool{nodePool("a", deleting)},
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("a")},
			want:        []string{"a:RedpandaBrokerPool"},
		},
		"a release without a RedpandaBrokerPool keeps the pool": {
			nodePools: []*redpandav1alpha2.NodePool{nodePool("a", annotated("true")), nodePool("b", deleting)},
			want:      []string{"a:NodePool", "b:NodePool"},
		},
		"pools of different names are independent": {
			nodePools:   []*redpandav1alpha2.NodePool{nodePool("a", annotated("true")), nodePool("b")},
			brokerPools: []*redpandav1alpha2.RedpandaBrokerPool{brokerPool("c"), brokerPool("a")},
			want:        []string{"a:RedpandaBrokerPool", "b:NodePool", "c:RedpandaBrokerPool"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string
			for _, p := range ResolvePools(tc.nodePools, tc.brokerPools) {
				ref := p.Ref()
				require.Equal(t, p.BrokerPool.Name, ref.Name)
				got = append(got, fmt.Sprintf("%s:%s", ref.Name, ref.GetKind()))
			}
			require.Equal(t, tc.want, got)
		})
	}
}

// TestNodePoolToBrokerPool pins that the conversion carries every field a
// NodePool has: a dropped field would silently change the rendered pool.
func TestNodePoolToBrokerPool(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		np := rapid.MakeCustom[redpandav1alpha2.NodePool](rapidutil.KubernetesTypes).Draw(t, "nodePool")

		bp := NodePoolToBrokerPool(&np)

		require.Equal(t, np.ObjectMeta, bp.ObjectMeta)
		require.Equal(t, np.Spec.ClusterRef, bp.Spec.ClusterRef)
		// JSON, as the renderer merges it; DeepCopy normalizes some
		// apply-configuration fields, which Equal would flag.
		want, err := json.Marshal(np.Spec.EmbeddedNodePoolSpec)
		require.NoError(t, err)
		got, err := json.Marshal(brokerPoolNodeSpec(bp))
		require.NoError(t, err)
		require.JSONEq(t, string(want), string(got))
	})
}

// TestBrokerPoolRendersAsNodePool pins the migration invariant: a
// RedpandaBrokerPool with a NodePool's spec renders the NodePool's
// StatefulSet, so handing the pool over rolls no broker. Only the generation
// label, which is StatefulSet metadata, follows the owner.
func TestBrokerPoolRendersAsNodePool(t *testing.T) {
	cluster := &redpandav1alpha2.Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "redpanda"}}
	pin := func(*redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage {
		return &redpandav1alpha2.RedpandaImage{Repository: ptr.To("redpandadata/redpanda"), Tag: ptr.To("v26.1.1")}
	}

	for name, spec := range map[string]redpandav1alpha2.EmbeddedNodePoolSpec{
		"defaults":  {},
		"replicas":  {Replicas: ptr.To(int32(5))},
		"image tag": {Image: &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v26.1.2")}},
		"pod template": {PodTemplate: &redpandav1alpha2.PodTemplate{Spec: &applycorev1.PodSpecApplyConfiguration{
			NodeSelector: map[string]string{"pool": "big"},
		}}},
		"retention policy": {PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenScaled: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		}},
		"flags and selector labels": {
			AdditionalRedpandaCmdFlags: []string{"--smp=2"},
			AdditionalSelectorLabels:   map[string]string{"team": "a"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			np := &redpandav1alpha2.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "redpanda", UID: types.UID("np"), Generation: 7},
				Spec:       redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: spec, ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp"}},
			}
			bp := &redpandav1alpha2.RedpandaBrokerPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "redpanda", UID: types.UID("bp"), Generation: 1},
				Spec: redpandav1alpha2.BrokerPoolSpec{
					EmbeddedBrokerPoolSpec: NodePoolToBrokerPool(np).Spec.EmbeddedBrokerPoolSpec,
					ClusterRef:             redpandav1alpha2.ClusterRef{Name: "rp", Kind: ptr.To(redpandav1alpha2.RedpandaKind)},
				},
			}

			render := func(pool *redpandav1alpha2.RedpandaBrokerPool) *appsv1.StatefulSet {
				state, err := ConvertV2ToRenderState(nil, &V2Defaulters{RedpandaImage: pin, SidecarImage: pin}, cluster, []*redpandav1alpha2.RedpandaBrokerPool{pool})
				require.NoError(t, err)
				sets, err := redpandachart.RenderNodePools(state)
				require.NoError(t, err)
				for _, set := range sets {
					if set.Labels[redpandachart.NodePoolLabelName] == pool.Name {
						delete(set.Labels, redpandachart.NodePoolLabelGeneration)
						return set
					}
				}
				require.FailNow(t, "pool StatefulSet not rendered")
				return nil
			}

			require.Equal(t, render(NodePoolToBrokerPool(np)), render(bp))
		})
	}
}
