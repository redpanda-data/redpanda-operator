// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources_unit_tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

func TestRenderClusterService(t *testing.T) {
	cluster := createBasicCluster()
	services := resources.RenderClusterService(cluster)
	
	// Should create services based on the cluster configuration
	require.Greater(t, len(services), 0, "should create at least one service")
	
	for i, svc := range services {
		t.Logf("Service %d - Name: %s, Ports: %d", i, svc.Name, len(svc.Spec.Ports))
		assert.Equal(t, cluster.Name+"-cluster", svc.Name)
		assert.Equal(t, cluster.Namespace, svc.Namespace)
		assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		assert.True(t, svc.Spec.PublishNotReadyAddresses)
		
		for j, port := range svc.Spec.Ports {
			t.Logf("  Port %d: %s:%d", j, port.Name, port.Port)
		}
	}
}


func TestDebugClusterServicePorts(t *testing.T) {
	cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Replicas: ptr.To(int32(1)),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Name: "kafka",
						Port: 9092,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: false,
						},
					},
					{
						Name: "kafka-external",
						Port: 9093,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: true,
							Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
								Port: 9094,
							},
						},
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{
					{
						Port: 9644,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: false,
						},
					},
				},
				RPCServer: vectorizedv1alpha1.SocketAddress{
					Port: 33145,
				},
			},
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		},
	}

	t.Logf("Internal listener: %+v", cluster.InternalListener())
	t.Logf("Admin API internal: %+v", cluster.AdminAPIInternal())
	t.Logf("External listeners: %+v", cluster.KafkaAPIExternalListeners())
	
	services := resources.RenderClusterService(cluster)
	t.Logf("Number of cluster services: %d", len(services))
	for i, svc := range services {
		t.Logf("Service %d - Name: %s, Ports: %d", i, svc.Name, len(svc.Spec.Ports))
		for j, port := range svc.Spec.Ports {
			t.Logf("  Port %d: %s:%d", j, port.Name, port.Port)
		}
	}
	
	lbService := resources.RenderLoadBalancerService(cluster)
	if lbService != nil {
		t.Logf("LB Service - Name: %s, Ports: %d", lbService.Name, len(lbService.Spec.Ports))
		for j, port := range lbService.Spec.Ports {
			t.Logf("  LB Port %d: %s:%d", j, port.Name, port.Port)
		}
	} else {
		t.Logf("No LB service rendered")
	}
	
	nodePortService := resources.RenderNodePortService(cluster)
	if nodePortService != nil {
		t.Logf("NodePort Service - Name: %s, Ports: %d", nodePortService.Name, len(nodePortService.Spec.Ports))
		for j, port := range nodePortService.Spec.Ports {
			t.Logf("  NodePort Port %d: %s:%d", j, port.Name, port.Port)
		}
	} else {
		t.Logf("No NodePort service rendered")
	}
}

func createBasicCluster() *vectorizedv1alpha1.Cluster {
	return &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:    "redpanda",
			Version:  "v23.1.0",
			Replicas: ptr.To(int32(1)),
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				AdminAPI: []vectorizedv1alpha1.AdminAPI{
					{Port: 9644},
				},
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{Port: 9092, AuthenticationMethod: "none"},
				},
			},
		},
	}
}
