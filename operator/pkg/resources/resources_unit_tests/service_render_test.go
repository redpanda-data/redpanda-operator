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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

func TestRenderClusterService(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *vectorizedv1alpha1.Cluster
		svcPorts       []resources.NamedServicePort
		expectNil      bool
		expectedPorts  int
		validatePorts  func(t *testing.T, ports []corev1.ServicePort)
	}{
		{
			name:      "empty_ports_returns_nil",
			cluster:   createBasicCluster(),
			svcPorts:  []resources.NamedServicePort{},
			expectNil: true,
		},
		{
			name:      "nil_ports_returns_nil",
			cluster:   createBasicCluster(),
			svcPorts:  nil,
			expectNil: true,
		},
		{
			name:    "single_port",
			cluster: createBasicCluster(),
			svcPorts: []resources.NamedServicePort{
				{Name: resources.AdminPortName, Port: 9644},
			},
			expectNil:     false,
			expectedPorts: 1,
			validatePorts: func(t *testing.T, ports []corev1.ServicePort) {
				require.Len(t, ports, 1)
				assert.Equal(t, resources.AdminPortName, ports[0].Name)
				assert.Equal(t, int32(9644), ports[0].Port)
				assert.Equal(t, intstr.FromInt32(9644), ports[0].TargetPort)
				assert.Equal(t, corev1.ProtocolTCP, ports[0].Protocol)
			},
		},
		{
			name:    "multiple_ports",
			cluster: createBasicCluster(),
			svcPorts: []resources.NamedServicePort{
				{Name: resources.AdminPortName, Port: 9644},
				{Name: resources.InternalListenerName, Port: 9092},
				{Name: resources.PandaproxyPortInternalName, Port: 8082},
				{Name: resources.SchemaRegistryPortName, Port: 8081},
			},
			expectNil:     false,
			expectedPorts: 4,
			validatePorts: func(t *testing.T, ports []corev1.ServicePort) {
				require.Len(t, ports, 4)
				portMap := make(map[string]int32)
				for _, p := range ports {
					portMap[p.Name] = p.Port
				}
				assert.Equal(t, int32(9644), portMap[resources.AdminPortName])
				assert.Equal(t, int32(9092), portMap[resources.InternalListenerName])
				assert.Equal(t, int32(8082), portMap[resources.PandaproxyPortInternalName])
				assert.Equal(t, int32(8081), portMap[resources.SchemaRegistryPortName])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := resources.RenderClusterService(ctx, tt.cluster, tt.svcPorts)
			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, result, "expected nil result when no ports provided")
				return
			}

			require.NotNil(t, result)
			svc, ok := result.(*corev1.Service)
			require.True(t, ok, "result should be a Service")

			assert.Equal(t, tt.cluster.Name+"-cluster", svc.Name)
			assert.Equal(t, tt.cluster.Namespace, svc.Namespace)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
			assert.Equal(t, tt.expectedPorts, len(svc.Spec.Ports))
			assert.True(t, svc.Spec.PublishNotReadyAddresses)

			if tt.validatePorts != nil {
				tt.validatePorts(t, svc.Spec.Ports)
			}
		})
	}
}

func TestRenderHeadlessService(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *vectorizedv1alpha1.Cluster
		expectedPorts  int
		validatePorts  func(t *testing.T, ports []corev1.ServicePort)
		validateAnnotations func(t *testing.T, annotations map[string]string)
	}{
		{
			name: "no_ports_still_creates_service",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-ports-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						// No admin API configured
						// No kafka API configured
					},
				},
			},
			expectedPorts: 0,
			validatePorts: func(t *testing.T, ports []corev1.ServicePort) {
				assert.Empty(t, ports, "should have no ports when no APIs configured")
			},
		},
		{
			name: "admin_port_only",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "admin-only-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						AdminAPI: []vectorizedv1alpha1.AdminAPI{
							{Port: 9644},
						},
					},
				},
			},
			expectedPorts: 1,
			validatePorts: func(t *testing.T, ports []corev1.ServicePort) {
				require.Len(t, ports, 1)
				assert.Equal(t, resources.AdminPortName, ports[0].Name)
				assert.Equal(t, int32(9644), ports[0].Port)
			},
		},
		{
			name: "all_internal_ports",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "full-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						AdminAPI: []vectorizedv1alpha1.AdminAPI{
							{Port: 9644},
						},
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{Port: 9092, AuthenticationMethod: "none"},
						},
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{Port: 8082, AuthenticationMethod: "none"},
						},
					},
				},
			},
			expectedPorts: 3,
			validatePorts: func(t *testing.T, ports []corev1.ServicePort) {
				require.Len(t, ports, 3)
				portMap := make(map[string]int32)
				for _, p := range ports {
					portMap[p.Name] = p.Port
				}
				assert.Equal(t, int32(9644), portMap[resources.AdminPortName])
				assert.Equal(t, int32(9092), portMap[resources.InternalListenerName])
				assert.Equal(t, int32(8082), portMap[resources.PandaproxyPortInternalName])
			},
		},
		{
			name: "with_external_listener_subdomain",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "external-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						AdminAPI: []vectorizedv1alpha1.AdminAPI{
							{Port: 9644},
						},
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: 9092,
								AuthenticationMethod: "none",
							},
							{
								Name: "external",
								Port: 30092,
								AuthenticationMethod: "none",
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled:   true,
									Subdomain: "redpanda.example.com",
								},
							},
						},
					},
				},
			},
			expectedPorts: 2, // Only internal ports
			validateAnnotations: func(t *testing.T, annotations map[string]string) {
				assert.Equal(t, "redpanda.example.com", annotations["external-dns.alpha.kubernetes.io/hostname"])
				assert.Equal(t, "true", annotations["external-dns.alpha.kubernetes.io/use-external-host-ip"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := resources.RenderHeadlessService(ctx, tt.cluster)
			require.NoError(t, err)
			require.NotNil(t, result, "headless service should always be created")

			svc, ok := result.(*corev1.Service)
			require.True(t, ok, "result should be a Service")

			assert.Equal(t, tt.cluster.Name, svc.Name)
			assert.Equal(t, tt.cluster.Namespace, svc.Namespace)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
			assert.True(t, svc.Spec.PublishNotReadyAddresses)
			assert.Equal(t, tt.expectedPorts, len(svc.Spec.Ports))

			if tt.validatePorts != nil {
				tt.validatePorts(t, svc.Spec.Ports)
			}

			if tt.validateAnnotations != nil {
				tt.validateAnnotations(t, svc.Annotations)
			}
		})
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