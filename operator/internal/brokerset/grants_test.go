// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestUnmigratedPool: a broker pod a StatefulSet runs that no Broker names is
// one the pass can't ask.
func TestUnmigratedPool(t *testing.T) {
	owned := statefulSet("rp-pa", true, 2)
	for name, tc := range map[string]struct {
		objs    []k8sclient.Object
		brokers []string
		lost    []string
		failing bool
		want    bool
	}{
		"no StatefulSet": {},
		"every pod named, as mid-migration": {
			objs:    []k8sclient.Object{owned, podOf("rp-pa-0", owned), podOf("rp-pa-1", owned)},
			brokers: []string{"rp-pa-0", "rp-pa-1"},
		},
		"a pod no Broker names": {
			objs:    []k8sclient.Object{owned, podOf("rp-pa-0", owned), podOf("rp-pa-1", owned)},
			brokers: []string{"rp-pa-0"},
			want:    true,
		},
		"a pod only a tombstone names": {
			objs:    []k8sclient.Object{owned, podOf("rp-pa-0", owned), podOf("rp-pa-1", owned)},
			brokers: []string{"rp-pa-0"},
			lost:    []string{"rp-pa-1"},
			want:    true,
		},
		"more replicas than pods listed, as when one was just created": {
			objs:    []k8sclient.Object{owned, podOf("rp-pa-0", owned)},
			brokers: []string{"rp-pa-0"},
			want:    true,
		},
		"a pod the StatefulSet no longer controls is the Brokers'": {
			objs:    []k8sclient.Object{statefulSet("rp-pa", true, 1), podOf("rp-pa-0", owned), podOf("rp-pa-1", nil)},
			brokers: []string{"rp-pa-0"},
		},
		"another owner's StatefulSet": {
			objs: []k8sclient.Object{statefulSet("rp-pa", false, 2), podOf("rp-pa-0", nil)},
		},
		"a list that fails": {
			failing: true,
			want:    true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(scheme))
			require.NoError(t, redpandav1alpha2.Install(scheme))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c k8sclient.WithWatch, list k8sclient.ObjectList, opts ...k8sclient.ListOption) error {
					if tc.failing {
						return errors.New("list failed")
					}
					return c.List(ctx, list, opts...)
				},
			}).Build()

			var brokers []redpandav1alpha2.Broker
			for _, pod := range tc.brokers {
				brokers = append(brokers, namingBroker(pod, false))
			}
			for _, pod := range tc.lost {
				brokers = append(brokers, namingBroker(pod, true))
			}
			s := poolBrokerSet(scheme, c, nil, nil, "pa")
			require.Equal(t, tc.want, s.unmigratedPool(t.Context(), brokers))
		})
	}
}

func statefulSet(name string, owned bool, replicas int32) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: propertyNamespace, UID: types.UID(name)},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}},
		Status:     appsv1.StatefulSetStatus{Replicas: replicas},
	}
	if owned {
		owner := propertyOwner()
		sts.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: ptr.To(true)}}
	}
	return sts
}

// podOf is a pod of sts's selector, controlled by it unless nil.
func podOf(name string, sts *appsv1.StatefulSet) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: propertyNamespace, Labels: map[string]string{"app": "rp-pa"}}}
	if sts != nil {
		pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true)}}
	}
	return pod
}

// namingBroker is a Broker of pool pa whose pod is named pod.
func namingBroker(pod string, diskLost bool) redpandav1alpha2.Broker {
	b := redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: pod, Namespace: propertyNamespace, Labels: map[string]string{redpandav1alpha2.NodePoolLabel: "pa"}},
		Spec:       redpandav1alpha2.BrokerSpec{ClusterRef: redpandav1alpha2.ClusterRef{Name: "rp"}, NetworkIndex: ptr.To(int32(pod[len(pod)-1] - '0'))},
	}
	if diskLost {
		b.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}
	}
	return b
}
