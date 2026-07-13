// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestBrokerPodName(t *testing.T) {
	v1Broker := func(pool string, index int32) *Broker {
		b := &Broker{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns"},
			Spec: BrokerSpec{
				ClusterRef: ClusterRef{
					Group: ptr.To("redpanda.vectorized.io"),
					Kind:  ptr.To("Cluster"),
					Name:  "rp",
				},
				NetworkIndex: ptr.To(index),
			},
		}
		if pool != "" {
			b.Labels = map[string]string{NodePoolLabel: pool}
		}
		return b
	}

	// Default pool (explicit or unlabeled): pods carry the bare cluster name.
	assert.Equal(t, "rp-0", v1Broker("default", 0).PodName())
	assert.Equal(t, "rp-2", v1Broker("", 2).PodName())
	// Pools declared in Cluster.spec.nodePools: <cluster>-<pool>-<ordinal>,
	// matching the per-pool StatefulSet naming.
	assert.Equal(t, "rp-high-mem-1", v1Broker("high-mem", 1).PodName())

	// NodePool ref: ClusterRef names the pool, the cluster half comes from
	// ClusterNameLabel — pods are <cluster>-<pool>-<ordinal>.
	nodePoolBroker := &Broker{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Labels:    map[string]string{ClusterNameLabel: "rp"},
		},
		Spec: BrokerSpec{
			ClusterRef: ClusterRef{
				Group: ptr.To("cluster.redpanda.com"),
				Kind:  ptr.To("NodePool"),
				Name:  "high-mem",
			},
			NetworkIndex: ptr.To(int32(1)),
		},
	}
	assert.Equal(t, "rp-high-mem-1", nodePoolBroker.PodName())
}

func TestPodOutdated(t *testing.T) {
	broker := &Broker{
		Spec: BrokerSpec{
			PodTemplate: BrokerPodTemplate{
				Annotations: map[string]string{
					BrokerConfigChecksumAnnotation:       "checksum-a",
					BrokerClusterConfigVersionAnnotation: "7",
				},
			},
		},
	}
	pod := func(annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: annotations}}
	}

	// In-sync pod: both rotation keys match.
	assert.False(t, broker.PodOutdated(pod(map[string]string{
		BrokerConfigChecksumAnnotation:       "checksum-a",
		BrokerClusterConfigVersionAnnotation: "7",
	})))

	// Checksum drift demands rotation.
	assert.True(t, broker.PodOutdated(pod(map[string]string{
		BrokerConfigChecksumAnnotation:       "checksum-b",
		BrokerClusterConfigVersionAnnotation: "7",
	})))

	// Restart-marker drift demands rotation.
	assert.True(t, broker.PodOutdated(pod(map[string]string{
		BrokerConfigChecksumAnnotation:       "checksum-a",
		BrokerClusterConfigVersionAnnotation: "6",
	})))

	// A missing pod annotation counts as drift when the template sets one.
	assert.True(t, broker.PodOutdated(pod(map[string]string{
		BrokerConfigChecksumAnnotation: "checksum-a",
	})))

	// An UNSET desired key never demands rotation, whatever the pod has.
	broker.Spec.PodTemplate.Annotations = map[string]string{}
	assert.False(t, broker.PodOutdated(pod(map[string]string{
		BrokerConfigChecksumAnnotation: "anything",
	})))
}

func TestBuildPodDeepCopies(t *testing.T) {
	broker := &Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-0", Namespace: "ns"},
		Spec: BrokerSpec{
			ClusterRef:   ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(0)),
			PodTemplate: BrokerPodTemplate{
				Labels:      map[string]string{"a": "1"},
				Annotations: map[string]string{BrokerConfigChecksumAnnotation: "checksum-a"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda:v1"}},
				},
			},
		},
	}

	pod := broker.BuildPod(broker.PodName())
	assert.Equal(t, "rp-0", pod.Name)
	assert.Equal(t, "checksum-a", pod.Annotations[BrokerConfigChecksumAnnotation])

	// Mutating the built pod must not corrupt the Broker's template.
	pod.Annotations[BrokerConfigChecksumAnnotation] = "mutated"
	pod.Labels["a"] = "mutated"
	pod.Spec.Containers[0].Image = "mutated"
	assert.Equal(t, "checksum-a", broker.Spec.PodTemplate.Annotations[BrokerConfigChecksumAnnotation])
	assert.Equal(t, "1", broker.Spec.PodTemplate.Labels["a"])
	assert.Equal(t, "redpanda:v1", broker.Spec.PodTemplate.Spec.Containers[0].Image)
}
