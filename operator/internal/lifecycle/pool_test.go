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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

func objectNames[T client.Object](list []T) []string {
	ids := []string{}
	for _, o := range list {
		ids = append(ids, o.GetName())
	}
	return ids
}

func clusterObjectNamespaceNames[T clusterObject](list []T) []string {
	ids := []string{}
	for _, o := range list {
		ids = append(ids, objectKeyFromObject(o).String())
	}
	return ids
}

func TestPoolTrackerCheckScale(t *testing.T) {
	for name, tt := range map[string]struct {
		existingPools []*poolWithOrdinals
		canScale      bool
	}{
		"no-pools": {
			canScale: true,
		},
		"replica-pod-mismatch": {
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}},
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
						Status:     appsv1.StatefulSetStatus{Replicas: 2},
					},
				},
			}},
			canScale: false,
		},
		"scale-down": {
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}},
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(0))},
						Status:     appsv1.StatefulSetStatus{Replicas: 1},
					},
				},
			}},
			canScale: false,
		},
		"scale-up": {
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}},
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
						Status:     appsv1.StatefulSetStatus{Replicas: 2},
					},
				},
			}},
			canScale: false,
		},
		"stable": {
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
					},
				}},
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
						Status:     appsv1.StatefulSetStatus{Replicas: 2},
					},
				},
			}},
			canScale: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(0)
			tracker.addExisting(tt.existingPools...)
			require.Equal(t, tt.canScale, tracker.CheckScale())
		})
	}
}

func TestPoolTrackerToCreate(t *testing.T) {
	for name, tt := range map[string]struct {
		existingPools        []*MulticlusterStatefulSet
		desiredPools         []*MulticlusterStatefulSet
		expectedSetsToCreate []string
	}{
		"no-op": {},
		"no-creations": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
		},
		"excess-existing": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-3",
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}},
		},
		"no-existing": {
			expectedSetsToCreate: []string{"pool-1", "pool-2"},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
		},
		"some-existing": {
			expectedSetsToCreate: []string{"pool-2"},
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(0)

			pools := []*poolWithOrdinals{}
			for _, set := range tt.existingPools {
				pools = append(pools, &poolWithOrdinals{
					set: &MulticlusterStatefulSet{
						StatefulSet: set.DeepCopy(),
						clusterName: set.clusterName,
					},
				})
			}

			tracker.addExisting(pools...)
			tracker.addDesired(tt.desiredPools...)

			actual := objectNames(tracker.ToCreate())
			require.ElementsMatch(t, tt.expectedSetsToCreate, actual)
		})
	}
}

func TestPoolTrackerToScaleUp(t *testing.T) {
	for name, tt := range map[string]struct {
		existingPools         []*MulticlusterStatefulSet
		desiredPools          []*MulticlusterStatefulSet
		expectedSetsToScaleUp []string
	}{
		"no-op": {},
		"no-scale-ups": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
		},
		"all-scale-ups": {
			expectedSetsToScaleUp: []string{"pool-1"},
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
		},
		"some-scale-ups": {
			expectedSetsToScaleUp: []string{"pool-1"},
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
		},
		"scale-down": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(0)

			pools := []*poolWithOrdinals{}
			for _, set := range tt.existingPools {
				pools = append(pools, &poolWithOrdinals{
					set: &MulticlusterStatefulSet{
						clusterName: mcmanager.LocalCluster,
						StatefulSet: set.DeepCopy(),
					},
				})
			}

			tracker.addExisting(pools...)
			tracker.addDesired(tt.desiredPools...)

			actual := objectNames(tracker.ToScaleUp())
			require.ElementsMatch(t, tt.expectedSetsToScaleUp, actual)
		})
	}
}

func TestPoolTrackerRequiresUpdate(t *testing.T) {
	for name, tt := range map[string]struct {
		generation           int64
		existingPools        []*MulticlusterStatefulSet
		desiredPools         []*MulticlusterStatefulSet
		expectedSetsToUpdate []string
	}{
		"no-op": {},
		"no-updates": {
			generation: 1,
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pool-1",
						Labels: map[string]string{generationLabel: "1"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pool-2",
						Labels: map[string]string{generationLabel: "1"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
		},
		"scaling-up": {
			generation: 1,
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pool-1",
						Labels: map[string]string{generationLabel: "0"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
		},
		"scaling-down": {
			generation: 1,
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pool-1",
						Labels: map[string]string{generationLabel: "0"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
		},
		"updates": {
			generation:           1,
			expectedSetsToUpdate: []string{"pool-1"},
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pool-1",
						Labels: map[string]string{generationLabel: "0"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
					Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(2))},
				},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(tt.generation)

			pools := []*poolWithOrdinals{}
			for _, set := range tt.existingPools {
				pools = append(pools, &poolWithOrdinals{
					set: &MulticlusterStatefulSet{StatefulSet: set.DeepCopy(), clusterName: set.clusterName},
				})
			}

			tracker.addExisting(pools...)
			tracker.addDesired(tt.desiredPools...)

			actual := objectNames(tracker.RequiresUpdate())
			require.ElementsMatch(t, tt.expectedSetsToUpdate, actual)
		})
	}
}

func TestPoolTrackerToScaleDown(t *testing.T) {
	for name, tt := range map[string]struct {
		existingPools           []*poolWithOrdinals
		desiredPools            []*MulticlusterStatefulSet
		expectedSetsToScaleDown []string
	}{
		"no-op": {},
		"deleted-set": {
			expectedSetsToScaleDown: []string{"pool-1(2): pod-3"},
			existingPools: []*poolWithOrdinals{{
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
					},
				},
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-3"},
					},
				}},
			}},
		},
		"scaling-up-set": {
			existingPools: []*poolWithOrdinals{{
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
					},
				},
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-3"},
					},
				}},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
					Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(4))},
				},
			}},
		},
		"scaling-down-set": {
			expectedSetsToScaleDown: []string{"pool-1(2): pod-3", "pool-3(0): pod-1"},
			existingPools: []*poolWithOrdinals{{
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
					},
				},
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-3"},
					},
				}},
			}, {
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-2"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
					},
				},
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}},
			}, {
				set: &MulticlusterStatefulSet{
					clusterName: mcmanager.LocalCluster,
					StatefulSet: &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{Name: "pool-3"},
						Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
					},
				},
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
					},
				}},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "pool-1"},
					Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "pool-2"},
					Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(1)
			tracker.addExisting(tt.existingPools...)
			tracker.addDesired(tt.desiredPools...)

			toIDs := func(list []*ScaleDownSet) []string {
				ids := []string{}
				for _, o := range list {
					ids = append(ids, fmt.Sprintf("%s(%d): %s", o.StatefulSet.Name, ptr.Deref(o.StatefulSet.Spec.Replicas, 0), o.LastPod.Name))
				}
				return ids
			}

			actual := toIDs(tracker.ToScaleDown())
			require.ElementsMatch(t, tt.expectedSetsToScaleDown, actual)
		})
	}
}

func TestPoolTrackerToDelete(t *testing.T) {
	for name, tt := range map[string]struct {
		existingPools        []*MulticlusterStatefulSet
		desiredPools         []*MulticlusterStatefulSet
		expectedSetsToDelete []string
	}{
		"no-op": {},
		"no-deletions": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}},
		},
		"scaling-down": {
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-3",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(0)),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas: 1,
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}},
		},
		"can-delete": {
			expectedSetsToDelete: []string{"pool-2", "pool-3"},
			existingPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-2",
					},
				},
			}, {
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-3",
					},
				},
			}},
			desiredPools: []*MulticlusterStatefulSet{{
				clusterName: mcmanager.LocalCluster,
				StatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pool-1",
					},
				},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(0)

			pools := []*poolWithOrdinals{}
			for _, set := range tt.existingPools {
				pools = append(pools, &poolWithOrdinals{
					set: &MulticlusterStatefulSet{StatefulSet: set.DeepCopy(), clusterName: set.clusterName},
				})
			}

			tracker.addExisting(pools...)
			tracker.addDesired(tt.desiredPools...)

			actual := objectNames(tracker.ToDelete())
			require.ElementsMatch(t, tt.expectedSetsToDelete, actual)
		})
	}
}

func TestPoolTrackerPodsToRoll(t *testing.T) {
	pool1 := &MulticlusterStatefulSet{
		clusterName: mcmanager.LocalCluster,
		StatefulSet: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pool-1",
			},
		},
	}
	pool1Revisions := []*appsv1.ControllerRevision{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "a",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "b",
		},
	}}
	pool2 := &MulticlusterStatefulSet{
		clusterName: mcmanager.LocalCluster,
		StatefulSet: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pool-2",
			},
		},
	}
	pool2Revisions := []*appsv1.ControllerRevision{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar",
		},
	}}

	for name, tt := range map[string]struct {
		existingPools      []*poolWithOrdinals
		expectedPodsToRoll []string
	}{
		"no-op": {},
		"all-up-to-date": {
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-1",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "b",
							},
						},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-2",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "b",
							},
						},
					},
				}},
				set:       pool1,
				revisions: pool1Revisions,
			}},
		},
		"all-out-of-date": {
			expectedPodsToRoll: []string{"pod-1", "pod-2"},
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-1",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "a",
							},
						},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-2",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "a",
							},
						},
					},
				}},
				set:       pool1,
				revisions: pool1Revisions,
			}},
		},
		"no-revisions": {
			expectedPodsToRoll: []string{"pod-1", "pod-2"},
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-1",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "a",
							},
						},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-2",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "a",
							},
						},
					},
				}},
				set: pool1,
			}},
		},
		"mixed": {
			expectedPodsToRoll: []string{"pod-1", "pod-3"},
			existingPools: []*poolWithOrdinals{{
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-1",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "a",
							},
						},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-2",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "b",
							},
						},
					},
				}},
				set:       pool1,
				revisions: pool1Revisions,
			}, {
				pods: []*podsWithOrdinals{{
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-3",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "foo",
							},
						},
					},
				}, {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod-4",
							Labels: map[string]string{
								appsv1.StatefulSetRevisionLabel: "bar",
							},
						},
					},
				}},
				set:       pool2,
				revisions: pool2Revisions,
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPoolTracker(0)
			tracker.addExisting(tt.existingPools...)

			actual := objectNames(tracker.PodsToRoll())
			require.ElementsMatch(t, tt.expectedPodsToRoll, actual)
		})
	}
}
