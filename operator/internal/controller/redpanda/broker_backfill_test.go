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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestBackfillRotationKeysCoversAllRotationKeys(t *testing.T) {
	broker := &redpandav1alpha2.Broker{
		Spec: redpandav1alpha2.BrokerSpec{
			PodTemplate: redpandav1alpha2.BrokerPodTemplate{
				Annotations: map[string]string{
					redpandav1alpha2.BrokerPodTemplateHashAnnotation:      "hash-1",
					redpandav1alpha2.BrokerConfigChecksumAnnotation:       "checksum-1",
					redpandav1alpha2.BrokerClusterConfigVersionAnnotation: "config-v1",
				},
			},
		},
	}
	pod := &corev1.Pod{}

	require.True(t, backfillRotationKeys(broker, pod), "an annotation-less pod must get stamped")

	for _, key := range redpandav1alpha2.RotationAnnotations {
		assert.Equalf(t, broker.Spec.PodTemplate.Annotations[key], pod.Annotations[key],
			"rotation key %q was not backfilled onto the adopted pod", key)
	}
	assert.False(t, broker.PodOutdated(pod),
		"an adopted pod must not be pending a rotation immediately after backfill")
}

// TestPodMetadataSyncStampsBrokerID: the node_id reaches the live pod through
// the in-place metadata sync once the identity is adopted, so a pod created
// before registration still carries it on its next start. The write is
// idempotent, and a Broker without an identity stamps nothing.
func TestPodMetadataSyncStampsBrokerID(t *testing.T) {
	for _, tc := range []struct {
		name     string
		brokerID *int32
		want     string
	}{
		{name: "adopted identity is stamped", brokerID: ptr.To(int32(3)), want: "3"},
		{name: "no identity, no annotation", brokerID: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broker := &redpandav1alpha2.Broker{
				ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "ns", UID: types.UID("broker-uid-1")},
				Spec: redpandav1alpha2.BrokerSpec{
					ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
					NetworkIndex: ptr.To(int32(1)),
				},
				Status: redpandav1alpha2.BrokerStatus{BrokerID: tc.brokerID},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rp-1",
					Namespace: "ns",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: redpandav1alpha2.GroupVersion.String(),
						Kind:       "Broker",
						Name:       "rp-1",
						UID:        types.UID("broker-uid-1"),
						Controller: ptr.To(true),
					}},
				},
			}
			c := fake.NewClientBuilder().WithScheme(diskLostScheme(t)).
				WithObjects(pod).WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
			r := &BrokerReconciler{}

			for pass := range 2 {
				state, err := r.fetchState(t.Context(), mcreconcile.Request{}, c, broker)
				require.NoError(t, err)
				_, err = r.reconcilePodMetadata(t.Context(), state, &diskLostCluster{client: c})
				require.NoError(t, err)

				var live corev1.Pod
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(pod), &live))
				got, ok := live.Annotations[redpandav1alpha2.BrokerIDAnnotation]
				assert.Equalf(t, tc.want != "", ok, "pass %d: annotation presence", pass)
				assert.Equalf(t, tc.want, got, "pass %d", pass)
			}
		})
	}
}
