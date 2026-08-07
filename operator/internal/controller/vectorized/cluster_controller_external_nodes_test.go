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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// TestCreateExternalNodesListSkipsUnscheduledPods pins the guard for pods
// without a node: in external-IP mode the per-pod node lookup used to run
// with an empty name, fail unconditionally, and — via reportStatus — abort
// every reconcile before configuration, license, and ghost-decommission
// handling for as long as ANY pod was Pending. A pod pinned to a dead node
// by PV affinity keeps that state indefinitely.
func TestCreateExternalNodesListSkipsUnscheduledPods(t *testing.T) {
	ctx := context.Background()

	cluster := &vectorizedv1alpha1.Cluster{
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

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "rp-0", Namespace: "test"},
			Spec:       corev1.PodSpec{NodeName: "node-a"},
		},
		{
			// Unscheduled (e.g. PV affinity pinned to a dead node): no
			// NodeName, no addresses — must be skipped, not fail the list.
			ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "test"},
			Spec:       corev1.PodSpec{},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, nodePortSvc, node).Build()
	r := &ClusterReconciler{Client: c, Scheme: scheme}

	nodeList, err := r.createExternalNodesList(ctx, pods, cluster,
		types.NamespacedName{Name: "rp-external", Namespace: "test"},
		types.NamespacedName{Name: "rp-bootstrap", Namespace: "test"})
	require.NoError(t, err, "an unscheduled pod must not fail external node list construction")
	require.NotNil(t, nodeList)
	require.Equal(t, []string{"203.0.113.7:30093"}, nodeList.External,
		"only the scheduled pod contributes an external address")
}
