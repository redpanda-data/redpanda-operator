// Copyright 2026 Redpanda Data, Inc.
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
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	redpandaclient "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/brokerset"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
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

	fakeClient := fake.NewClientBuilder().Build()

	ssres := StatefulSetResource{
		Client: fakeClient,
		nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{
				Name: "default",
			},
		},
		pandaCluster: &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
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
	stsChange, _, err := ssres.shouldUpdate(context.Background(), sts, stsWithAnnotation)
	require.NoError(t, err)
	require.True(t, stsChange)

	// same statefulset with same annotation
	stsChange, _, err = ssres.shouldUpdate(context.Background(), stsWithAnnotation, stsWithAnnotation)
	require.NoError(t, err)
	require.False(t, stsChange)

	ssres = StatefulSetResource{
		Client: fakeClient,
		nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{
				Name: "default",
			},
		},
		pandaCluster: &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Status: vectorizedv1alpha1.ClusterStatus{
				Restarting: false,
				NodePools: map[string]vectorizedv1alpha1.NodePoolStatus{
					"default": {
						Restarting: true,
					},
				},
			},
		},
	}
	// same statefulset with same annotation, but with node pool status restarting
	_, nodePoolRestarting, err := ssres.shouldUpdate(context.Background(), stsWithAnnotation, stsWithAnnotation)
	require.NoError(t, err)
	require.True(t, nodePoolRestarting)
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
					_ redpandaclient.DialContextFunc,
					timeout time.Duration,
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

// enableRefusingAdminAPI wraps MockAdminAPI so EnableMaintenanceMode fails
// with a caller-chosen error, the way redpanda refuses a maintenance
// transition (e.g. while another broker still holds the maintenance flag).
type enableRefusingAdminAPI struct {
	*adminutils.MockAdminAPI
	enableErr error
}

func (m *enableRefusingAdminAPI) EnableMaintenanceMode(ctx context.Context, nodeID int) error {
	if m.enableErr != nil {
		return m.enableErr
	}
	return m.MockAdminAPI.EnableMaintenanceMode(ctx, nodeID)
}

// TestReplaceFailedPod pins that an outdated pod whose broker cannot start is
// replaced without the health gate or maintenance mode, both of which a down
// broker fails, and only while failedpods.Hold and the in-flight checks allow.
func TestReplaceFailedPod(t *testing.T) {
	const (
		oldImage, newImage = "redpanda:v26.3.1", "redpanda:v26.2.6"
		oldRev, newRev     = "rev-old", "rev-new"
	)
	type testPod struct {
		rev     string
		image   string
		failing bool
	}

	tcs := []struct {
		name         string
		pods         []testPod
		down         []int
		decommission *int32
		// scaleTo scales the pool down from three.
		scaleTo   *int32
		stsBehind bool
		// unmarked leaves outdated pods without the ClusterUpdate mark.
		unmarked bool
		// adminDown fails every admin API call, as when no broker runs.
		adminDown bool
		// deleteFails makes pod deletes report the pod already gone.
		deleteFails bool
		// reports maps a pod to the brokers its broker reports down, in
		// place of down.
		reports map[string][]int
		// rollStarted has an earlier pool start a roll this pass.
		rollStarted bool
		brokerMode  bool
		// deletedPool removes the pool from the spec, so it keeps no pods.
		deletedPool bool
		// otherPool adds a second pool, with its pod terminating, its pod
		// missing, its StatefulSet trailing its spec, or deleted around its
		// orphaned pod to be recreated.
		otherPool string
		// configRestart marks every pod for a configuration restart.
		configRestart bool
		// throughUpdate runs the pool's update, on a pass that changes its
		// StatefulSet if stsChanged.
		throughUpdate, stsChanged bool
		// defaultPool has spec.replicas drive the pool, as on a legacy
		// Cluster: its StatefulSet takes the Cluster's name, and its pods
		// carry no pool label.
		defaultPool bool
		replaced    string
	}{
		{
			name:     "replaces a failed outdated pod",
			pods:     []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:     []int{0},
			replaced: "cluster-first-0",
		},
		{
			name:          "an update replaces a failed pod ahead of the health gate",
			pods:          []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:          []int{0},
			throughUpdate: true,
			replaced:      "cluster-first-0",
		},
		{
			name:          "an update that changes the StatefulSet leaves it to the next pass",
			pods:          []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:          []int{0},
			throughUpdate: true,
			stsChanged:    true,
		},
		{
			name:      "holds while another pool's pod is terminating",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			otherPool: "terminating",
		},
		{
			name:      "holds while another pool's pod is missing",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			otherPool: "missing",
		},
		{
			name:      "holds while another pool's StatefulSet trails its spec",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			otherPool: "behind",
		},
		{
			name:      "holds while another pool's StatefulSet is recreated around its pods",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			otherPool: "orphaned",
		},
		{
			name:      "replaces a failed pod beside another pool that's settled",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			otherPool: "settled",
			replaced:  "cluster-first-0",
		},
		{
			name:          "a current pod marked for a config restart that can't start is kubelet's to restart",
			pods:          []testPod{{newRev, newImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:          []int{0},
			configRestart: true,
		},
		{
			name:        "leaves a deleted pool's pods to its decommission",
			pods:        []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:        []int{0},
			deletedPool: true,
		},
		{
			name:        "holds once another pool started a roll this pass",
			pods:        []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:        []int{0},
			rollStarted: true,
		},
		{
			name:       "leaves a pool in Broker CR mode to its roll grants",
			pods:       []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:       []int{0},
			brokerMode: true,
		},
		{
			name: "holds while the current template's only broker is down",
			pods: []testPod{{newRev, newImage, true}, {oldRev, oldImage, true}, {oldRev, oldImage, false}},
			down: []int{0, 1},
		},
		{
			name: "holds while the first replacement hasn't rejoined",
			pods: []testPod{{newRev, newImage, false}, {oldRev, oldImage, true}, {oldRev, oldImage, false}},
			down: []int{0, 1},
		},
		{
			name: "holds while the first replacement's peers report it down",
			pods: []testPod{{newRev, newImage, false}, {oldRev, oldImage, true}, {oldRev, oldImage, false}},
			reports: map[string][]int{
				"cluster-first-0": {1},
				"cluster-first-2": {0, 1},
			},
		},
		{
			name: "leaves a pod whose peers see its broker alive",
			pods: []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			reports: map[string][]int{
				"cluster-first-1": {},
				"cluster-first-2": {},
			},
		},
		{
			name: "a candidate its peers see alive doesn't stop the next",
			pods: []testPod{{oldRev, oldImage, true}, {oldRev, oldImage, true}, {newRev, newImage, false}},
			reports: map[string][]int{
				"cluster-first-2": {1},
			},
			replaced: "cluster-first-1",
		},
		{
			name: "holds while a pod is missing",
			pods: []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}},
			down: []int{0},
		},
		{
			name:         "replaces a failed pod while another broker is decommissioned",
			pods:         []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:         []int{0},
			decommission: ptr.To(int32(2)),
			replaced:     "cluster-first-0",
		},
		{
			name:         "leaves the broker being decommissioned",
			pods:         []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:         []int{0},
			decommission: ptr.To(int32(0)),
		},
		{
			name:     "replaces a failed pod while the pool scales another one away",
			pods:     []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:     []int{0},
			scaleTo:  ptr.To(int32(2)),
			replaced: "cluster-first-0",
		},
		{
			name:    "leaves the pod a scale-down removes next",
			pods:    []testPod{{newRev, newImage, false}, {newRev, newImage, false}, {oldRev, oldImage, true}},
			down:    []int{2},
			scaleTo: ptr.To(int32(2)),
		},
		{
			name:    "leaves every pod a scale-down removes, not just the next",
			pods:    []testPod{{newRev, newImage, false}, {oldRev, oldImage, true}, {newRev, newImage, false}},
			down:    []int{1},
			scaleTo: ptr.To(int32(1)),
		},
		{
			name:     "a down broker the scale-down removes doesn't hold a replacement",
			pods:     []testPod{{oldRev, oldImage, true}, {oldRev, oldImage, false}, {newRev, newImage, true}},
			down:     []int{0, 2},
			scaleTo:  ptr.To(int32(2)),
			replaced: "cluster-first-0",
		},
		{
			name:      "replaces a failed pod when every broker is down and none can answer",
			pods:      []testPod{{oldRev, oldImage, true}, {oldRev, oldImage, true}, {oldRev, oldImage, true}},
			adminDown: true,
			replaced:  "cluster-first-0",
		},
		{
			name:      "holds while running brokers can't answer, as when the operator can't reach them",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			adminDown: true,
		},
		{
			name:        "a failed delete holds rather than fails",
			pods:        []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:        []int{0},
			deleteFails: true,
		},
		{
			name:      "holds while the StatefulSet status trails its spec",
			pods:      []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:      []int{0},
			stsBehind: true,
		},
		{
			name:     "leaves a pod its pool no longer marks for update",
			pods:     []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:     []int{0},
			unmarked: true,
		},
		{
			name: "leaves live outdated pods to the gated roll",
			pods: []testPod{{oldRev, oldImage, false}, {oldRev, oldImage, false}, {oldRev, oldImage, false}},
		},
		{
			name:        "replaces a failed pod in the pool spec.replicas drives",
			pods:        []testPod{{oldRev, oldImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:        []int{0},
			defaultPool: true,
			replaced:    "cluster-0",
		},
		{
			name:     "replaces a failed pod whose diff shows nothing, as when a field was removed",
			pods:     []testPod{{oldRev, newImage, true}, {newRev, newImage, false}, {newRev, newImage, false}},
			down:     []int{0},
			replaced: "cluster-first-0",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cluster := pandaCluster()
			cluster.Spec.NodePools[0].Replicas = ptr.To(int32(3))
			cluster.SetDecommissionBrokerID(tc.decommission)
			if tc.scaleTo != nil {
				cluster.Spec.NodePools[0].Replicas = tc.scaleTo
				cluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{cluster.Spec.NodePools[0].Name: {CurrentReplicas: 3}}
			}
			if tc.throughUpdate {
				cluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{cluster.Spec.NodePools[0].Name: {CurrentReplicas: 3, Restarting: true}}
			}
			template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "redpanda", Image: newImage}},
			}}
			pool := cluster.Spec.NodePools[0]
			if tc.defaultPool {
				cluster.Spec.Replicas, cluster.Spec.NodePools = pool.Replicas, nil
				pool = cluster.GetNodePoolsFromSpec()[0]
			}
			if tc.deletedPool {
				cluster.Spec.NodePools = nil
				cluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{pool.Name: {CurrentReplicas: 3}}
			}
			arbitration := &brokerset.Arbitration{}
			if tc.rollStarted {
				arbitration.MarkRollGrant()
			}
			ssres := StatefulSetResource{
				pandaCluster: cluster,
				nodePool:     vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: pool, Deleted: tc.deletedPool},
				logger:       ctrl.Log.WithName("TestReplaceFailedPod"),
				arbitration:  arbitration,
				brokerMode:   tc.brokerMode,
			}

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ssres.Key().Name,
					Namespace: cluster.Namespace,
					UID:       "sts-uid",
					Labels:    labels.ForCluster(cluster).WithNodePool(pool.Name),
				},
				Spec:   appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3)), Template: template},
				Status: appsv1.StatefulSetStatus{UpdateRevision: newRev},
			}
			if tc.stsBehind {
				sts.Generation, sts.Status.ObservedGeneration = 2, 1
			}
			objs := []client.Object{sts}
			var brokers []rpadmin.Broker
			for ordinal, tp := range tc.pods {
				name := fmt.Sprintf("%s-%d", sts.Name, ordinal)
				redpanda := corev1.ContainerStatus{Name: "redpanda", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, Started: ptr.To(true)}
				if tp.failing {
					now := metav1.Now()
					redpanda.State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
					redpanda.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
				}
				podLabels := labels.ForCluster(cluster).WithNodePool(pool.Name)
				if tc.defaultPool {
					delete(podLabels, labels.NodePoolKey)
				}
				podLabels[appsv1.StatefulSetRevisionLabel] = tp.rev
				conditions := []corev1.PodCondition{{Type: corev1.PodInitialized, Status: corev1.ConditionTrue}}
				switch {
				case tc.configRestart:
					conditions = append(conditions, corev1.PodCondition{Type: ClusterUpdatePodCondition, Status: corev1.ConditionTrue, Reason: ClusterUpdateReasonConfig})
				case tp.rev == oldRev && !tc.unmarked:
					conditions = append(conditions, corev1.PodCondition{Type: ClusterUpdatePodCondition, Status: corev1.ConditionTrue, Reason: ClusterUpdateReasonVersion})
				}
				objs = append(objs, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        name,
						Namespace:   cluster.Namespace,
						Labels:      podLabels,
						Annotations: map[string]string{PodAnnotationNodeIDKey: strconv.Itoa(ordinal)},
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true),
						}},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "redpanda", Image: tp.image}},
						Volumes: []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-" + name},
						}}},
					},
					Status: corev1.PodStatus{
						Conditions:        conditions,
						ContainerStatuses: []corev1.ContainerStatus{redpanda},
					},
				})
				brokers = append(brokers, rpadmin.Broker{NodeID: ordinal, InternalRPCAddress: name + ".cluster.default.svc"})
			}
			if tc.otherPool != "" {
				other := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-second", Namespace: cluster.Namespace, UID: "sts-second", Labels: labels.ForCluster(cluster).WithNodePool("second")},
					Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
					Status:     appsv1.StatefulSetStatus{UpdateRevision: newRev},
				}
				switch tc.otherPool {
				case "missing":
					other.Spec.Replicas = ptr.To(int32(2))
				case "behind":
					other.Generation, other.Status.ObservedGeneration = 2, 1
				}
				podLabels := labels.ForCluster(cluster).WithNodePool("second")
				podLabels[appsv1.StatefulSetRevisionLabel] = newRev
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-second-0", Namespace: cluster.Namespace, Labels: podLabels,
						OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: other.Name, UID: other.UID, Controller: ptr.To(true)}},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: newImage}}},
					Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
						Name: "redpanda", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, Started: ptr.To(true),
					}}},
				}
				switch tc.otherPool {
				case "terminating":
					pod.DeletionTimestamp, pod.Finalizers = ptr.To(metav1.Now()), []string{"test/hold"}
				case "orphaned":
					pod.OwnerReferences = nil
				}
				objs = append(objs, pod)
				if tc.otherPool != "orphaned" {
					objs = append(objs, other)
				}
				brokers = append(brokers, rpadmin.Broker{NodeID: 9, InternalRPCAddress: "cluster-second-0.cluster.default.svc"})
			}
			var funcs interceptor.Funcs
			if tc.deleteFails {
				funcs.Delete = func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewNotFound(corev1.Resource("pods"), obj.GetName())
				}
			}
			c := fake.NewClientBuilder().WithObjects(objs...).WithStatusSubresource(&corev1.Pod{}).WithInterceptorFuncs(funcs).Build()
			ssres.Client = c
			var healthChecked bool
			ssres.adminAPIClientFactory = func(
				_ context.Context,
				_ client.Reader,
				_ *vectorizedv1alpha1.Cluster,
				_ string,
				_ types.AdminTLSConfigProvider,
				_ redpandaclient.DialContextFunc,
				_ time.Duration,
				pods ...string,
			) (adminutils.AdminAPIClient, error) {
				if len(pods) == 0 {
					healthChecked = true
				}
				if tc.adminDown {
					return nil, fmt.Errorf("no broker answers")
				}
				down := tc.down
				if len(pods) == 1 && tc.reports != nil {
					down = tc.reports[pods[0]]
				}
				var all []int
				for _, b := range brokers {
					all = append(all, b.NodeID)
				}
				return &clusterViewAdminAPI{
					MockAdminAPI: &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")},
					all:          all,
					down:         down,
					brokers:      brokers,
				}, nil
			}

			var err error
			if tc.throughUpdate {
				var current appsv1.StatefulSet
				require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(sts), &current))
				modified := current.DeepCopy()
				if tc.stsChanged {
					modified.Spec.Template.Spec.Containers[0].Image = "redpanda:v26.2.7"
				}
				err = ssres.runUpdate(context.Background(), &current, modified)
				// A replacement requeues ahead of the health gate.
				require.Equal(t, tc.replaced == "", healthChecked, "health checked")
			} else {
				err = ssres.replaceFailedPod(context.Background(), &template)
			}
			if tc.replaced != "" || tc.throughUpdate {
				var requeue *RequeueAfterError
				require.ErrorAs(t, err, &requeue)
			} else {
				require.NoError(t, err)
			}

			for _, obj := range objs[1:] {
				if _, ok := obj.(*corev1.Pod); !ok {
					continue
				}
				getErr := c.Get(context.Background(), client.ObjectKeyFromObject(obj), &corev1.Pod{})
				if obj.GetName() == tc.replaced {
					require.True(t, apierrors.IsNotFound(getErr), "%s should have been replaced, got: %v", obj.GetName(), getErr)
					continue
				}
				require.NoError(t, getErr, "%s should not have been replaced", obj.GetName())
			}
			require.Equal(t, tc.rollStarted || tc.replaced != "" || tc.deleteFails, arbitration.RollGranted(), "a delete holds the pass's later pools")
		})
	}
}

// clusterViewAdminAPI reports a fixed set of down brokers and broker addresses.
type clusterViewAdminAPI struct {
	*adminutils.MockAdminAPI
	all, down []int
	brokers   []rpadmin.Broker
}

func (m *clusterViewAdminAPI) GetHealthOverview(context.Context) (rpadmin.ClusterHealthOverview, error) {
	return rpadmin.ClusterHealthOverview{AllNodes: m.all, NodesDown: m.down}, nil
}

func (m *clusterViewAdminAPI) Brokers(context.Context) ([]rpadmin.Broker, error) {
	return m.brokers, nil
}

func TestPodEvictionHonorsMaintenanceModeRefusal(t *testing.T) {
	httpErr := func(code int) *rpadmin.HTTPResponseError {
		return &rpadmin.HTTPResponseError{
			Response: &http.Response{StatusCode: code},
			Body:     []byte(fmt.Sprintf(`{"message": "refused", "code": %d}`, code)),
		}
	}

	tcs := []struct {
		name       string
		enableErr  error
		podDeleted bool
		// replace evicts as a replacement of a broker that cannot start.
		replace bool
	}{
		// Redpanda refusing the maintenance transition must delay the
		// eviction, not be overridden: the refusal is redpanda's own
		// one-disruption-at-a-time interlock (another broker may still hold
		// the maintenance flag), and deleting the pod anyway takes a second
		// broker down, undrained.
		{"400 refusal requeues without deleting", httpErr(http.StatusBadRequest), false, false},
		{"503 refusal requeues without deleting", httpErr(http.StatusServiceUnavailable), false, false},
		// 404 means the broker is gone (decommissioned): there is nothing to
		// drain, so the eviction proceeds.
		{"404 proceeds with the eviction", httpErr(http.StatusNotFound), true, false},
		// A broker that can't start has nothing to drain and can't be asked to.
		{"a replacement deletes without maintenance mode", httpErr(http.StatusBadRequest), true, true},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cluster := pandaCluster()
			cluster.Spec.NodePools[0].Replicas = ptr.To(int32(3))

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-first-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{{
						Type:   ClusterUpdatePodCondition,
						Status: corev1.ConditionTrue,
						Reason: ClusterUpdateReasonConfig,
					}},
				},
			}
			c := fake.NewClientBuilder().WithObjects(pod).Build()

			arbitration := &brokerset.Arbitration{}
			ssres := StatefulSetResource{
				Client:       c,
				arbitration:  arbitration,
				pandaCluster: cluster,
				nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{
					NodePoolSpec: cluster.Spec.NodePools[0],
				},
				logger: ctrl.Log.WithName("TestPodEvictionHonorsMaintenanceModeRefusal"),
				adminAPIClientFactory: func(
					ctx context.Context,
					k8sClient client.Reader,
					redpandaCluster *vectorizedv1alpha1.Cluster,
					fqdn string,
					adminTLSProvider types.AdminTLSConfigProvider,
					_ redpandaclient.DialContextFunc,
					timeout time.Duration,
					pods ...string,
				) (adminutils.AdminAPIClient, error) {
					return &enableRefusingAdminAPI{
						MockAdminAPI: &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")},
						enableErr:    tc.enableErr,
					}, nil
				},
			}

			var artificialPod corev1.Pod
			artificialPod.Spec = pod.Spec

			err := ssres.podEviction(context.Background(), pod, &artificialPod, nil, tc.replace)

			var requeue *RequeueAfterError
			require.ErrorAs(t, err, &requeue)

			var got corev1.Pod
			getErr := c.Get(context.Background(), client.ObjectKeyFromObject(pod), &got)
			if tc.podDeleted {
				require.True(t, apierrors.IsNotFound(getErr), "pod should have been deleted, got: %v", getErr)
			} else {
				require.NoError(t, getErr, "pod must NOT be deleted while redpanda refuses maintenance mode")
			}
			require.Equal(t, tc.podDeleted, arbitration.RollGranted(), "a delete holds the pass's later pools")
		})
	}
}

func TestRollingUpdateResumesInFlightMaintenance(t *testing.T) {
	const replicas = 3

	tcs := []struct {
		name string
		// inMaintenance is the node ID holding the maintenance slot when the
		// pass starts, or -1.
		inMaintenance int
		marked        []int
		evicted       string
	}{
		// A StatefulSet change mid-drain re-marks every pod. Restarting at
		// ordinal 0 deadlocks: redpanda refuses broker 0 while broker 1 holds
		// the slot, and broker 1 is never evicted, so never cleared.
		{"resumes the broker already in maintenance", 1, []int{0, 1, 2}, "cluster-first-1"},
		{"rolls in ordinal order with no broker in maintenance", -1, []int{0, 1, 2}, "cluster-first-0"},
		{"clears maintenance left on an already-rolled broker", 1, []int{0, 2}, "cluster-first-0"},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cluster := pandaCluster()
			cluster.Spec.NodePools[0].Replicas = ptr.To(int32(replicas))
			pool := cluster.Spec.NodePools[0]

			var pods []client.Object
			for ordinal := range replicas {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        fmt.Sprintf("cluster-first-%d", ordinal),
						Namespace:   cluster.Namespace,
						Labels:      labels.ForCluster(cluster).WithNodePool(pool.Name),
						Annotations: map[string]string{PodAnnotationNodeIDKey: strconv.Itoa(ordinal)},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda:v26.2.4"}},
					},
				}
				if slices.Contains(tc.marked, ordinal) {
					pod.Status.Conditions = []corev1.PodCondition{{
						Type:   ClusterUpdatePodCondition,
						Status: corev1.ConditionTrue,
						Reason: ClusterUpdateReasonVersion,
					}}
				}
				pods = append(pods, pod)
			}
			c := fake.NewClientBuilder().WithObjects(pods...).Build()

			holder := tc.inMaintenance
			ssres := StatefulSetResource{
				Client:       c,
				pandaCluster: cluster,
				nodePool:     vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: pool},
				logger:       ctrl.Log.WithName("TestRollingUpdateResumesInFlightMaintenance"),
				adminAPIClientFactory: func(
					_ context.Context,
					_ client.Reader,
					_ *vectorizedv1alpha1.Cluster,
					_ string,
					_ types.AdminTLSConfigProvider,
					_ redpandaclient.DialContextFunc,
					_ time.Duration,
					pods ...string,
				) (adminutils.AdminAPIClient, error) {
					var pod string
					if len(pods) > 0 {
						pod = pods[0]
					}
					return &maintenanceSlotAdminAPI{
						MockAdminAPI: &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")},
						brokers:      replicas,
						pod:          pod,
						holder:       &holder,
					}, nil
				},
			}

			template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda:v26.2.5"}},
			}}
			err := ssres.rollingUpdate(context.Background(), &template)

			var requeue *RequeueAfterError
			require.ErrorAs(t, err, &requeue)

			for _, pod := range pods {
				getErr := c.Get(context.Background(), client.ObjectKeyFromObject(pod), &corev1.Pod{})
				if pod.GetName() == tc.evicted {
					require.True(t, apierrors.IsNotFound(getErr), "%s should have been evicted, got: %v", pod.GetName(), getErr)
				} else {
					require.NoError(t, getErr, "%s should not have been evicted", pod.GetName())
				}
			}
		})
	}
}

// maintenanceSlotAdminAPI models redpanda's single maintenance slot, shared
// by every client the factory hands out: EnableMaintenanceMode refuses all
// but the holder, and drains finish instantly. A pod-scoped client reports
// the pod's ordinal as its node ID.
type maintenanceSlotAdminAPI struct {
	*adminutils.MockAdminAPI
	brokers int
	pod     string
	holder  *int // -1 when the slot is free
}

func (m *maintenanceSlotAdminAPI) GetNodeConfig(context.Context) (rpadmin.NodeConfig, error) {
	ordinal, err := utils.GetPodOrdinal(m.pod, "cluster")
	return rpadmin.NodeConfig{NodeID: int(ordinal)}, err
}

func (m *maintenanceSlotAdminAPI) Brokers(context.Context) ([]rpadmin.Broker, error) {
	var brokers []rpadmin.Broker
	for id := range m.brokers {
		brokers = append(brokers, m.broker(id))
	}
	return brokers, nil
}

func (m *maintenanceSlotAdminAPI) Broker(_ context.Context, id int) (rpadmin.Broker, error) {
	return m.broker(id), nil
}

func (m *maintenanceSlotAdminAPI) EnableMaintenanceMode(_ context.Context, id int) error {
	if *m.holder != -1 && *m.holder != id {
		return &rpadmin.HTTPResponseError{
			Response: &http.Response{StatusCode: http.StatusBadRequest},
			Body:     []byte(`{"message": "invalid state transition requested", "code": 400}`),
		}
	}
	*m.holder = id
	return nil
}

func (m *maintenanceSlotAdminAPI) DisableMaintenanceMode(_ context.Context, id int, _ bool) error {
	if *m.holder == id {
		*m.holder = -1
	}
	return nil
}

func (m *maintenanceSlotAdminAPI) broker(id int) rpadmin.Broker {
	b := rpadmin.Broker{NodeID: id, Maintenance: &rpadmin.MaintenanceStatus{}}
	if *m.holder == id {
		b.Maintenance = &rpadmin.MaintenanceStatus{Draining: true, Finished: ptr.To(true)}
	}
	return b
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

func TestShouldUpdate_PodsWithClusterUpdateCondition(t *testing.T) {
	var replicas int32 = 3
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "cluster-first",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":        "redpanda",
					"app.kubernetes.io/instance":    "cluster",
					"app.kubernetes.io/component":   "redpanda",
					"cluster.redpanda.com/nodepool": "first",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":        "redpanda",
						"app.kubernetes.io/instance":    "cluster",
						"app.kubernetes.io/component":   "redpanda",
						"cluster.redpanda.com/nodepool": "first",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda:v25.2.7"}},
				},
			},
		},
	}

	// Create pods with ClusterUpdate condition set (simulating stuck state)
	pods := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-first-0",
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/name":        "redpanda",
					"app.kubernetes.io/instance":    "cluster",
					"app.kubernetes.io/component":   "redpanda",
					"cluster.redpanda.com/nodepool": "first",
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: ClusterUpdatePodCondition, Status: corev1.ConditionTrue, Reason: ClusterUpdateReasonVersion},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-first-1",
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/name":        "redpanda",
					"app.kubernetes.io/instance":    "cluster",
					"app.kubernetes.io/component":   "redpanda",
					"cluster.redpanda.com/nodepool": "first",
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: ClusterUpdatePodCondition, Status: corev1.ConditionTrue, Reason: ClusterUpdateReasonVersion},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithObjects(pods...).Build()

	ssres := StatefulSetResource{
		Client: fakeClient,
		logger: ctrl.Log.WithName("test"),
		nodePool: vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{
				Name: "first",
			},
		},
		pandaCluster: &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster",
				Namespace: "default",
			},
			Status: vectorizedv1alpha1.ClusterStatus{
				Restarting: false, // Status says not restarting (stale cache)
				NodePools: map[string]vectorizedv1alpha1.NodePoolStatus{
					"first": {
						Restarting: false, // NodePool status also says not restarting
					},
				},
			},
		},
	}

	// No StatefulSet diff, status says not restarting, but pods have ClusterUpdate condition
	// Should detect this and return nodePoolRestarting=true for idempotency
	stsChange, nodePoolRestarting, err := ssres.shouldUpdate(context.Background(), sts, sts)
	require.NoError(t, err)
	require.False(t, stsChange, "StatefulSet should not have changed")
	require.True(t, nodePoolRestarting, "Should detect pods need restart despite status saying otherwise")
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

// stubSRClientsFactory returns a SchemaRegistryClientsFactory serving one
// per-broker client per given base URL, with a short HTTP timeout so a
// blocking /status/ready handler (the "store still replaying" signal) fails
// fast in tests. A non-nil err is returned instead when set.
func stubSRClientsFactory(t *testing.T, err error, urls ...string) SchemaRegistryClientsFactory {
	t.Helper()
	return func(context.Context, client.Client, *vectorizedv1alpha1.Cluster, string, types.AdminTLSConfigProvider, redpandaclient.DialContextFunc, ...string) ([]schemaregistry.Broker, error) {
		if err != nil {
			return nil, err
		}
		brokers := make([]schemaregistry.Broker, 0, len(urls))
		for _, u := range urls {
			client, cerr := sr.NewClient(sr.URLs(u), sr.HTTPClient(&http.Client{Timeout: 150 * time.Millisecond}))
			require.NoError(t, cerr)
			brokers = append(brokers, schemaregistry.Broker{URL: u, Client: client})
		}
		return brokers, nil
	}
}

func TestWaitOnSchemaRegistrySyncReady(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/status/ready", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	ssres := StatefulSetResource{schemaRegistryClientFactory: stubSRClientsFactory(t, nil, ts.URL)}
	require.NoError(t, ssres.waitOnSchemaRegistrySync(context.Background()))
}

func TestWaitOnSchemaRegistrySyncReplaying(t *testing.T) {
	// Mirrors core's read_sync() await: /status/ready blocks while the SR
	// store replays _schemas. The gate must treat the resulting timeout as
	// "not synced".
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer ts.Close()
	ssres := StatefulSetResource{schemaRegistryClientFactory: stubSRClientsFactory(t, nil, ts.URL)}
	require.Error(t, ssres.waitOnSchemaRegistrySync(context.Background()))
}

func TestWaitOnSchemaRegistrySyncNotReadyStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()
	ssres := StatefulSetResource{schemaRegistryClientFactory: stubSRClientsFactory(t, nil, ts.URL)}
	require.Error(t, ssres.waitOnSchemaRegistrySync(context.Background()))
}

func TestWaitOnSchemaRegistrySyncSkips(t *testing.T) {
	// No factory injected — how the gate is disabled
	// (--wait-for-schema-registry-sync=false wires nil): skipped.
	ssres := StatefulSetResource{}
	require.NoError(t, ssres.waitOnSchemaRegistrySync(context.Background()))

	// Schema Registry disabled on the cluster: the factory reports
	// schemaregistry.ErrDisabled and the gate skips.
	ssres = StatefulSetResource{schemaRegistryClientFactory: stubSRClientsFactory(t, schemaregistry.ErrDisabled)}
	require.NoError(t, ssres.waitOnSchemaRegistrySync(context.Background()))
}

func TestWaitOnSchemaRegistrySyncFailsClosed(t *testing.T) {
	// A factory error other than ErrDisabled means we cannot confirm sync —
	// the gate must error (the roll loop requeues).
	ssres := StatefulSetResource{schemaRegistryClientFactory: stubSRClientsFactory(t, assert.AnError)}
	require.Error(t, ssres.waitOnSchemaRegistrySync(context.Background()))
}
