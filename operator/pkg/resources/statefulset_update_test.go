// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:testpackage // the tests use private methods
package resources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

func pandaCluster() *vectorizedv1alpha1.Cluster {
	var replicas int32 = 1

	res := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	}

	return &vectorizedv1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RedpandaCluster",
			APIVersion: "core.vectorized.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
			Labels: map[string]string{
				"app": "redpanda",
			},
			UID: "ff2770aa-c919-43f0-8b4a-30cb7cfdaf79",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:   "image",
			Version: "v22.3.0",
			CloudStorage: vectorizedv1alpha1.CloudStorageConfig{
				Enabled: true,
				CacheStorage: &vectorizedv1alpha1.StorageSpec{
					Capacity:         resource.MustParse("10Gi"),
					StorageClassName: "local",
				},
				SecretKeyRef: corev1.ObjectReference{
					Namespace: "default",
					Name:      "archival",
				},
			},
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: 345}},
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{{Port: 123, AuthenticationMethod: "none"}},
			},

			Sidecars: vectorizedv1alpha1.Sidecars{
				RpkStatus: &vectorizedv1alpha1.Sidecar{
					Enabled: true,
					Resources: &corev1.ResourceRequirements{
						Limits:   res,
						Requests: res,
					},
				},
			},

			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{
					Name:     "first",
					Replicas: ptr.To(replicas),
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   res,
							Requests: res,
						},
						Redpanda: nil,
					},
					Storage: vectorizedv1alpha1.StorageSpec{
						Capacity:         resource.MustParse("10Gi"),
						StorageClassName: "storage-class",
					},
				},
			},
		},
	}
}

func TestShouldUpdate_AnnotationChange(t *testing.T) {
	var replicas int32 = 1
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			ServiceName: "test",
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Namespace:   "default",
					Annotations: map[string]string{"test": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "test",
							Image:   "nginx",
							Command: []string{"nginx"},
						},
					},
				},
			},
		},
	}
	stsWithAnnotation := sts.DeepCopy()
	stsWithAnnotation.Spec.Template.Annotations = map[string]string{"test": "test2"}
	ssres := StatefulSetResource{
		nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{
				Name: "default",
			},
		},
		pandaCluster: &vectorizedv1alpha1.Cluster{
			Status: vectorizedv1alpha1.ClusterStatus{
				Restarting: false,
				NodePools: map[string]vectorizedv1alpha1.NodePoolStatus{
					"default": {
						Restarting: false,
					},
				},
			},
		},
	}
	update, err := ssres.shouldUpdate(sts, stsWithAnnotation)
	require.NoError(t, err)
	require.True(t, update)

	// same statefulset with same annotation
	update, err = ssres.shouldUpdate(stsWithAnnotation, stsWithAnnotation)
	require.NoError(t, err)
	require.False(t, update)
}

func TestPutInMaintenanceMode(t *testing.T) {
	cluster := pandaCluster()
	tcs := []struct {
		name              string
		maintenanceStatus *rpadmin.MaintenanceStatus
		errorRequired     error
		pandaCluster      *vectorizedv1alpha1.Cluster
	}{
		{
			"maintenance finished",
			&rpadmin.MaintenanceStatus{
				Finished: ptr.To(true),
			},
			nil,
			cluster,
		},
		{
			"maintenance draining",
			&rpadmin.MaintenanceStatus{
				Draining: true,
			},
			ErrMaintenanceNotFinished,
			cluster,
		},
		{
			"maintenance failed",
			&rpadmin.MaintenanceStatus{
				Failed: ptr.To(1),
			},
			ErrMaintenanceNotFinished,
			cluster,
		},
		{
			"maintenance has errors",
			&rpadmin.MaintenanceStatus{
				Errors: ptr.To(true),
			},
			ErrMaintenanceNotFinished,
			cluster,
		},
		{
			"maintenance did not finished",
			&rpadmin.MaintenanceStatus{
				Finished: ptr.To(false),
			},
			ErrMaintenanceNotFinished,
			cluster,
		},
		{
			"maintenance was not returned",
			nil,
			ErrMaintenanceMissing,
			cluster,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ssres := StatefulSetResource{
				adminAPIClientFactory: func(
					ctx context.Context,
					k8sClient client.Reader,
					redpandaCluster *vectorizedv1alpha1.Cluster,
					fqdn string,
					adminTLSProvider types.AdminTLSConfigProvider,
					_ redpanda.DialContextFunc,
					pods ...string,
				) (adminutils.AdminAPIClient, error) {
					return &adminutils.MockAdminAPI{
						Log:               ctrl.Log.WithName("testAdminAPI").WithName("mockAdminAPI"),
						MaintenanceStatus: tc.maintenanceStatus,
					}, nil
				},
			}
			podName := fmt.Sprintf("%s-%s-0", tc.pandaCluster.Name, tc.pandaCluster.Spec.NodePools[0].Name)
			err := ssres.putInMaintenanceMode(context.Background(), podName)
			if tc.errorRequired == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.errorRequired)
			}
		})
	}
}

func TestEvaluateRedpandaUnderReplicatedPartition(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open("testdata/metrics.golden.txt")
		require.NoError(t, err)

		_, err = io.Copy(w, f)
		require.NoError(t, err)
	}))
	defer ts.Close()

	ssres := StatefulSetResource{pandaCluster: &vectorizedv1alpha1.Cluster{
		Spec: vectorizedv1alpha1.ClusterSpec{
			RestartConfig: &vectorizedv1alpha1.RestartConfig{},
		},
	}}

	adminURL := url.URL{
		Scheme: "http",
		Host:   ts.Listener.Addr().String(),
		Path:   "metrics",
	}

	err := ssres.evaluateUnderReplicatedPartitions(context.Background(), &adminURL)
	require.NoError(t, err)
}

func TestEvaluateAboveThresholdRedpandaUnderReplicatedPartition(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
# HELP vectorized_cluster_partition_under_replicated_replicas Number of under replicated replicas
# TYPE vectorized_cluster_partition_under_replicated_replicas gauge
vectorized_cluster_partition_under_replicated_replicas{namespace="kafka",partition="0",shard="0",topic="test"} 1.000000
`)
	}))
	defer ts.Close()

	ssres := StatefulSetResource{pandaCluster: &vectorizedv1alpha1.Cluster{
		Spec: vectorizedv1alpha1.ClusterSpec{
			RestartConfig: &vectorizedv1alpha1.RestartConfig{},
		},
	}}

	adminURL := url.URL{
		Scheme: "http",
		Host:   ts.Listener.Addr().String(),
		Path:   "metrics",
	}

	err := ssres.evaluateUnderReplicatedPartitions(context.Background(), &adminURL)
	require.Error(t, err)
}

func TestEvaluateEqualThresholdInUnderReplicatedPartition(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
# HELP vectorized_cluster_partition_under_replicated_replicas Number of under replicated replicas
# TYPE vectorized_cluster_partition_under_replicated_replicas gauge
vectorized_cluster_partition_under_replicated_replicas{namespace="kafka",partition="0",shard="0",topic="test"} 1.000000
`)
	}))
	defer ts.Close()

	ssres := StatefulSetResource{pandaCluster: &vectorizedv1alpha1.Cluster{
		Spec: vectorizedv1alpha1.ClusterSpec{
			RestartConfig: &vectorizedv1alpha1.RestartConfig{
				UnderReplicatedPartitionThreshold: 1,
			},
		},
	}}

	adminURL := url.URL{
		Scheme: "http",
		Host:   ts.Listener.Addr().String(),
		Path:   "metrics",
	}

	err := ssres.evaluateUnderReplicatedPartitions(context.Background(), &adminURL)
	require.NoError(t, err)
}

func TestEvaluateWithoutRestartConfigInUnderReplicatedPartition(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
# HELP vectorized_cluster_partition_under_replicated_replicas Number of under replicated replicas
# TYPE vectorized_cluster_partition_under_replicated_replicas gauge
vectorized_cluster_partition_under_replicated_replicas{namespace="kafka",partition="0",shard="0",topic="test"} 1.000000
`)
	}))
	defer ts.Close()

	ssres := StatefulSetResource{pandaCluster: &vectorizedv1alpha1.Cluster{
		Spec: vectorizedv1alpha1.ClusterSpec{},
	}}

	adminURL := url.URL{
		Scheme: "http",
		Host:   ts.Listener.Addr().String(),
		Path:   "metrics",
	}

	err := ssres.evaluateUnderReplicatedPartitions(context.Background(), &adminURL)
	require.NoError(t, err)
}

//nolint:funlen // the test data doesn't count
func Test_sortPodList(t *testing.T) {
	const clusterName = "sortPodListCluster"
	cluster := vectorizedv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}

	emptyPodList := corev1.PodList{
		Items: []corev1.Pod{},
	}

	//nolint:dupl // not duplicate
	orderedPodList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-0"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-1"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-2"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-3"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-4"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-5"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-6"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-7"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-8"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-9"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-10"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-11"},
			},
		},
	}

	//nolint:dupl // not duplicate
	unorderedPodList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-11"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-4"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-1"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-10"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-0"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-2"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-3"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-5"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-9"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-6"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-8"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-7"},
			},
		},
	}
	type args struct {
		podList *corev1.PodList
		cluster *vectorizedv1alpha1.Cluster
	}
	tests := []struct {
		name string
		args args
		want *corev1.PodList
	}{
		{
			name: "empty pod list says empty",
			args: args{
				podList: &emptyPodList,
				cluster: &cluster,
			},
			want: &emptyPodList,
		},
		{
			name: "ordered pod list stays ordered",
			args: args{
				podList: &orderedPodList,
				cluster: &cluster,
			},
			want: &orderedPodList,
		},
		{
			name: "unordered pod list is sorted",
			args: args{
				podList: &unorderedPodList,
				cluster: &cluster,
			},
			want: &orderedPodList,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sortPodList(tt.args.podList, tt.args.cluster); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sortPodList() = %v, want %v", got, tt.want)
			}
		})
	}
}
