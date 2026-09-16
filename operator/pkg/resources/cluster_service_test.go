// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

func TestClusterServiceEndpointSteering(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &vectorizedv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "ns"}}
	ports := []NamedServicePort{{Name: "kafka", Port: 9092}, {Name: "schema-registry", Port: 8081}}

	for _, steering := range []bool{false, true} {
		res := NewClusterService(nil, cluster, scheme, ports, logr.Discard()).WithEndpointSteering(steering)
		obj, err := res.obj()
		require.NoError(t, err)

		svc := obj.(*corev1.Service)
		require.Equal(t, "rp-cluster", svc.Name)
		require.Len(t, svc.Spec.Ports, 2)

		if !steering {
			require.Equal(t, labels.ForCluster(cluster).AsAPISelector().MatchLabels, svc.Spec.Selector)
			require.NotContains(t, svc.Annotations, redpandachart.EndpointSteeringAnnotation)
			continue
		}
		// Steered: no selector, so the native EndpointSlice controller
		// leaves the Service alone, and the annotation names the cluster.
		require.Nil(t, svc.Spec.Selector)
		require.Equal(t, "rp", svc.Annotations[redpandachart.EndpointSteeringAnnotation])
	}
}

// TestHeadlessServiceIsNotSteered pins that broker discovery stays with the
// native controller: the headless Service carries no Schema Registry port,
// so steering it would make seed and admin DNS depend on the operator being
// up for nothing in return.
func TestHeadlessServiceIsNotSteered(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &vectorizedv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "ns"}}
	res := NewHeadlessService(nil, cluster, scheme, []NamedServicePort{{Name: "kafka", Port: 9092}}, logr.Discard())
	obj, err := res.obj()
	require.NoError(t, err)

	svc := obj.(*corev1.Service)
	require.Equal(t, labels.ForCluster(cluster).AsAPISelector().MatchLabels, svc.Spec.Selector)
	require.NotContains(t, svc.Annotations, redpandachart.EndpointSteeringAnnotation)
	require.True(t, svc.Spec.PublishNotReadyAddresses)
}
