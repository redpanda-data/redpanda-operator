// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type gvkClientObject struct {
	gvk schema.GroupVersionKind
	o   client.Object
}

type scopedGVKClientObject struct {
	scope meta.RESTScope
	gvk   schema.GroupVersionKind
	o     client.Object
}

func TestGetGVK(t *testing.T) {
	for name, tt := range map[string]struct {
		registeredObjects []gvkClientObject
		found             []gvkClientObject
		notFound          []client.Object
	}{
		"no-op": {},
		"none-registered": {
			notFound: []client.Object{&corev1.Pod{}},
		},
		"not-found": {
			registeredObjects: []gvkClientObject{{
				gvk: corev1.SchemeGroupVersion.WithKind("Pod"),
				o:   &corev1.Pod{},
			}},
			found: []gvkClientObject{{
				gvk: corev1.SchemeGroupVersion.WithKind("Pod"),
				o:   &corev1.Pod{},
			}},
			notFound: []client.Object{&corev1.PersistentVolume{}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()

			for _, register := range tt.registeredObjects {
				scheme.AddKnownTypeWithName(register.gvk, register.o)
			}

			for _, o := range tt.found {
				gvk, err := getGroupVersionKind(scheme, o.o)
				require.NoError(t, err)
				require.Equal(t, o.gvk, *gvk)
			}

			for _, o := range tt.notFound {
				_, err := getGroupVersionKind(scheme, o)
				require.Error(t, err)
			}
		})
	}
}

func TestSortCreation(t *testing.T) {
	now := metav1.Now()
	for name, tt := range map[string]struct {
		items    []client.Object
		expected []string
	}{
		"no-op": {
			expected: []string{},
		},
		"ordered": {
			expected: []string{"pod-1", "pod-2", "pod-3"},
			items: []client.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-1",
					CreationTimestamp: now,
				}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-2",
					CreationTimestamp: now,
				}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-3",
					CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
				}},
			},
		},
		"unordered": {
			expected: []string{"pod-1", "pod-2", "pod-3"},
			items: []client.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-2",
					CreationTimestamp: now,
				}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-3",
					CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
				}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name:              "pod-1",
					CreationTimestamp: now,
				}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := objectNames(sortCreation(tt.items))
			require.EqualValues(t, tt.expected, actual)
		})
	}
}

func TestSortByName(t *testing.T) {
	for name, tt := range map[string]struct {
		items    []*MulticlusterPod
		expected []string
	}{
		"no-op": {
			expected: []string{},
		},
		"ordered": {
			expected: []string{"cluster-a/namespace-1/pod-1", "cluster-a/namespace-1/pod-2", "cluster-a/namespace-2/pod-1", "cluster-a/namespace-2/pod-2", "cluster-b/namespace-1/pod-1"},
			items: []*MulticlusterPod{
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-1",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-2",
							Namespace: "namespace-1",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-2",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-2",
							Namespace: "namespace-2",
						},
					},
				},
				{
					clusterName: "cluster-b",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-1",
						},
					},
				},
			},
		},
		"unordered": {
			expected: []string{"cluster-a/namespace-1/pod-1", "cluster-a/namespace-1/pod-2", "cluster-a/namespace-2/pod-1", "cluster-a/namespace-2/pod-2", "cluster-b/namespace-1/pod-1"},
			items: []*MulticlusterPod{
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-2",
							Namespace: "namespace-2",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-1",
						},
					},
				},
				{
					clusterName: "cluster-b",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-1",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-2",
							Namespace: "namespace-1",
						},
					},
				},
				{
					clusterName: "cluster-a",
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-2",
						},
					},
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := clusterObjectNamespaceNames(sortByNameAndCluster(tt.items))
			require.EqualValues(t, tt.expected, actual)
		})
	}
}

func TestSortRevisions(t *testing.T) {
	now := metav1.Now()
	for name, tt := range map[string]struct {
		items    []*appsv1.ControllerRevision
		expected []string
	}{
		"no-op": {
			expected: []string{},
		},
		"ordered": {
			expected: []string{"revision-1-1", "revision-1-2", "revision-1-3", "revision-2"},
			items: []*appsv1.ControllerRevision{
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-1",
					CreationTimestamp: now,
				}, Revision: 1},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-2",
					CreationTimestamp: now,
				}, Revision: 1},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-3",
					CreationTimestamp: metav1.NewTime(now.Add(time.Minute)),
				}, Revision: 1},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-2",
					CreationTimestamp: now,
				}, Revision: 2},
			},
		},
		"unordered": {
			expected: []string{"revision-1-1", "revision-1-2", "revision-1-3", "revision-2"},
			items: []*appsv1.ControllerRevision{
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-3",
					CreationTimestamp: metav1.NewTime(now.Add(time.Minute)),
				}, Revision: 1},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-2",
					CreationTimestamp: now,
				}, Revision: 2},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-2",
					CreationTimestamp: now,
				}, Revision: 1},
				{ObjectMeta: metav1.ObjectMeta{
					Name:              "revision-1-1",
					CreationTimestamp: now,
				}, Revision: 1},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := objectNames(sortRevisions(tt.items))
			require.EqualValues(t, tt.expected, actual)
		})
	}
}

func TestExtractOrdinal(t *testing.T) {
	for _, tt := range []struct {
		pod     string
		ordinal int
		err     error
	}{{
		pod:     "pod-1",
		ordinal: 1,
	}, {
		pod:     "lots-of-dashes-100",
		ordinal: 100,
	}, {
		pod: "nodashes",
		err: errors.New("invalid resource name for ordinal fetching"),
	}, {
		pod: "no-ordinals-here",
		err: errors.New("parsing resource name"),
	}} {
		t.Run(tt.pod, func(t *testing.T) {
			t.Parallel()

			actualOrdinal, actualErr := extractOrdinal(tt.pod)
			if tt.err != nil {
				require.Error(t, actualErr)
				require.ErrorContains(t, actualErr, tt.err.Error())
				return
			}

			require.NoError(t, actualErr)
			require.Equal(t, tt.ordinal, actualOrdinal)
		})
	}
}

func TestSortPodsByOrdinal(t *testing.T) {
	for name, tt := range map[string]struct {
		items    []*corev1.Pod
		expected []int
		err      error
	}{
		"ordered": {
			expected: []int{1, 2, 3, 8, 9, 10},
			items: []*corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-8"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-9"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-10"}},
			},
		},
		"unordered": {
			expected: []int{1, 2, 3, 8, 9, 10},
			items: []*corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-9"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-10"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-8"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
			},
		},
		"error": {
			items: []*corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-10"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod"}},
			},
			err: errors.New("invalid resource name for ordinal fetching"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pods, err := sortPodsByOrdinal(tt.items...)

			if tt.err != nil {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.err.Error())
				return
			}

			ordinals := []int{}
			for _, p := range pods {
				ordinals = append(ordinals, p.ordinal)
			}

			require.EqualValues(t, tt.expected, ordinals)
		})
	}
}

func TestGetResourceScope(t *testing.T) {
	for name, tt := range map[string]struct {
		registeredObjects []scopedGVKClientObject
		resource          client.Object
		scope             meta.RESTScope
		err               error
	}{
		"not-registered": {
			registeredObjects: []scopedGVKClientObject{{
				scope: meta.RESTScopeNamespace,
				gvk:   corev1.SchemeGroupVersion.WithKind("Pod"),
				o:     &corev1.Pod{},
			}, {
				scope: meta.RESTScopeRoot,
				gvk:   corev1.SchemeGroupVersion.WithKind("PersistentVolume"),
				o:     &corev1.PersistentVolume{},
			}},
			resource: &corev1.PersistentVolumeClaim{},
			err:      errors.New("no kind is registered for the type v1.PersistentVolumeClaim"),
		},
		"namespace-scoped": {
			registeredObjects: []scopedGVKClientObject{{
				scope: meta.RESTScopeNamespace,
				gvk:   corev1.SchemeGroupVersion.WithKind("Pod"),
				o:     &corev1.Pod{},
			}, {
				scope: meta.RESTScopeRoot,
				gvk:   corev1.SchemeGroupVersion.WithKind("PersistentVolume"),
				o:     &corev1.PersistentVolume{},
			}},
			resource: &corev1.Pod{},
			scope:    meta.RESTScopeNamespace,
		},
		"cluster-scoped": {
			registeredObjects: []scopedGVKClientObject{{
				scope: meta.RESTScopeNamespace,
				gvk:   corev1.SchemeGroupVersion.WithKind("Pod"),
				o:     &corev1.Pod{},
			}, {
				scope: meta.RESTScopeRoot,
				gvk:   corev1.SchemeGroupVersion.WithKind("PersistentVolume"),
				o:     &corev1.PersistentVolume{},
			}},
			resource: &corev1.PersistentVolume{},
			scope:    meta.RESTScopeRoot,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mapper := meta.NewDefaultRESTMapper(nil)
			scheme := runtime.NewScheme()

			for _, register := range tt.registeredObjects {
				scheme.AddKnownTypeWithName(register.gvk, register.o)
				mapper.Add(register.gvk, register.scope)
			}

			actualScope, actualErr := getResourceScope(mapper, scheme, tt.resource)
			if tt.err != nil {
				require.Error(t, actualErr)
				require.ErrorContains(t, actualErr, tt.err.Error())
				return
			}

			require.NoError(t, actualErr)
			require.Equal(t, tt.scope, actualScope)
		})
	}
}
