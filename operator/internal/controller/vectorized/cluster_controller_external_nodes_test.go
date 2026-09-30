// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// TestCreateExternalNodesList checks the per-pod external Kafka address in
// both modes: node external IP when the listener has no subdomain, and the
// endpointTemplate rendered from the pod ordinal and the pool's
// hostIndexOffset when it has one.
func TestCreateExternalNodesList(t *testing.T) {
	const subdomain = "test.example.com"
	subdomainCluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
		Spec: vectorizedv1alpha1.ClusterSpec{
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{Name: "blue-a", Replicas: ptr.To[int32](3), HostIndexOffset: 10},
			},
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{Port: 9092},
					{Port: 9093, External: vectorizedv1alpha1.ExternalConnectivityConfig{
						Enabled:          true,
						Subdomain:        subdomain,
						EndpointTemplate: "{{.Index | add .HostIndexOffset}}",
					}},
				},
			},
		},
	}
	externalIPCluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{Port: 9092},
					// External listener with no subdomain: addresses need
					// the node's external IP.
					{Port: 9093, External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}},
				},
			},
		},
	}

	for _, tc := range []struct {
		name     string
		cluster  *vectorizedv1alpha1.Cluster
		pods     []corev1.Pod
		external []string
		errMsg   string
	}{
		{
			name:    "external IP mode skips unscheduled pods",
			cluster: externalIPCluster,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "rp-0", Namespace: "test"},
					Spec:       corev1.PodSpec{NodeName: "node-a"},
				},
				{
					// Unscheduled (e.g. PV affinity pinned to a dead node): no
					// NodeName, no addresses. Must be skipped, not fail the list.
					ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "test"},
					Spec:       corev1.PodSpec{},
				},
			},
			external: []string{"203.0.113.7:30093"},
		},
		{
			name:    "subdomain mode with Broker-created pod",
			cluster: subdomainCluster,
			pods: []corev1.Pod{{
				// RenderBrokers sets generateName on the Broker CR, not the
				// pod, and mirrors the StatefulSet identity labels.
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rp-blue-a-2",
					Namespace: "test",
					Labels: map[string]string{
						appsv1.PodIndexLabel: "2",
						labels.NodePoolKey:   "blue-a",
					},
				},
				Spec: corev1.PodSpec{NodeName: "node-a"},
			}},
			external: []string{"12." + subdomain + ":30093"},
		},
		{
			name:    "subdomain mode with StatefulSet-created pod",
			cluster: subdomainCluster,
			pods: []corev1.Pod{{
				// No pod-index label: the StatefulSet controller only sets
				// it with the PodIndexLabel feature gate, so the ordinal
				// must still come from the name.
				ObjectMeta: metav1.ObjectMeta{
					Name:         "rp-blue-a-2",
					GenerateName: "rp-blue-a-",
					Namespace:    "test",
					Labels:       map[string]string{labels.NodePoolKey: "blue-a"},
				},
				Spec: corev1.PodSpec{NodeName: "node-a"},
			}},
			external: []string{"12." + subdomain + ":30093"},
		},
		{
			name:    "subdomain mode with unparsable pod name",
			cluster: subdomainCluster,
			pods: []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "rp-blue-a-debug", Namespace: "test"},
				Spec:       corev1.PodSpec{NodeName: "node-a"},
			}},
			errMsg: "could not parse ordinal of pod rp-blue-a-debug",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodePortSvc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "rp-external", Namespace: "test"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: resources.ExternalListenerName, Port: 9093, NodePort: 30093},
					},
				},
			}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.7"},
					},
				},
			}

			scheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(scheme))
			require.NoError(t, vectorizedv1alpha1.Install(scheme))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.cluster, nodePortSvc, node).Build()
			r := &ClusterReconciler{Client: c, Scheme: scheme}

			nodeList, err := r.createExternalNodesList(context.Background(), tc.pods, tc.cluster,
				types.NamespacedName{Name: "rp-external", Namespace: "test"},
				types.NamespacedName{Name: "rp-bootstrap", Namespace: "test"})
			if tc.errMsg != "" {
				require.ErrorContains(t, err, tc.errMsg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, nodeList)
			require.Equal(t, tc.external, nodeList.External)
		})
	}
}
