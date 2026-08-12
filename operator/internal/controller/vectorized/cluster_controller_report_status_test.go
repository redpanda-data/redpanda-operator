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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// TestReportStatusWithholdsVersionDuringBrokerRoll pins broker mode to the
// StatefulSet-mode contract for Status.Version: it records what is ROLLED
// OUT, not what was requested. Stamping the spec version before every pod
// runs it kills the UpgradeInProgress guard in getQuiescentCondition, so
// OperatorQuiescent reports True (and ObservedGeneration is bumped) for the
// entire serialized restart of a version upgrade.
func TestReportStatusWithholdsVersionDuringBrokerRoll(t *testing.T) {
	const (
		oldVersion = "v25.1.1"
		newVersion = "v25.2.1"
	)

	tcs := []struct {
		name string
		// podImageTags orders pods by ordinal; "" means the pod is missing.
		podImageTags []string
		podReady     []bool
		wantVersion  string
	}{
		{
			name:         "mid-roll: all pods Ready on the old image",
			podImageTags: []string{oldVersion, oldVersion, oldVersion},
			podReady:     []bool{true, true, true},
			wantVersion:  oldVersion,
		},
		{
			name:         "mid-roll: one pod already on the new image",
			podImageTags: []string{newVersion, oldVersion, oldVersion},
			podReady:     []bool{true, true, true},
			wantVersion:  oldVersion,
		},
		{
			name:         "mid-roll: granted pod deleted",
			podImageTags: []string{"", newVersion, newVersion},
			podReady:     []bool{false, true, true},
			wantVersion:  oldVersion,
		},
		{
			name:         "mid-roll: replacement not yet Ready",
			podImageTags: []string{newVersion, newVersion, newVersion},
			podReady:     []bool{false, true, true},
			wantVersion:  oldVersion,
		},
		{
			name:         "converged: every pod Ready on the new image",
			podImageTags: []string{newVersion, newVersion, newVersion},
			podReady:     []bool{true, true, true},
			wantVersion:  newVersion,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			cluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    "vectorized/redpanda",
					Version:  newVersion,
					Replicas: ptr.To(int32(3)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{{Port: 9092}},
					},
				},
				Status: vectorizedv1alpha1.ClusterStatus{
					Version: oldVersion,
				},
			}

			selector := labels.ForCluster(cluster)

			objs := []runtime.Object{cluster}
			for i := 0; i < 3; i++ {
				broker := &redpandav1alpha2.Broker{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("rp-broker-%d", i),
						Namespace: "test",
						Labels: map[string]string{
							labels.NameKey:      selector[labels.NameKey],
							labels.InstanceKey:  selector[labels.InstanceKey],
							labels.ComponentKey: selector[labels.ComponentKey],
							labels.NodePoolKey:  "default",
						},
					},
					Spec: redpandav1alpha2.BrokerSpec{
						ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
						NetworkIndex: ptr.To(int32(i)),
					},
					Status: redpandav1alpha2.BrokerStatus{
						Conditions: []metav1.Condition{{
							Type:               statuses.BrokerReady,
							Status:             metav1.ConditionTrue,
							Reason:             "Ready",
							LastTransitionTime: metav1.Now(),
						}},
					},
				}
				objs = append(objs, broker)

				if tc.podImageTags[i] == "" {
					continue
				}
				readyStatus := corev1.ConditionFalse
				if tc.podReady[i] {
					readyStatus = corev1.ConditionTrue
				}
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("rp-%d", i),
						Namespace: "test",
						Labels: map[string]string{
							labels.NameKey:      selector[labels.NameKey],
							labels.InstanceKey:  selector[labels.InstanceKey],
							labels.ComponentKey: selector[labels.ComponentKey],
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "redpanda",
							Image: "vectorized/redpanda:" + tc.podImageTags[i],
						}},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: readyStatus},
							{Type: corev1.ContainersReady, Status: readyStatus},
						},
						ContainerStatuses: []corev1.ContainerStatus{{
							Name:  "redpanda",
							Ready: tc.podReady[i],
						}},
					},
				}
				objs = append(objs, pod)
			}

			scheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(scheme))
			require.NoError(t, vectorizedv1alpha1.Install(scheme))
			require.NoError(t, redpandav1alpha2.Install(scheme))
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				WithStatusSubresource(&vectorizedv1alpha1.Cluster{}).
				Build()
			r := &ClusterReconciler{Client: c, Scheme: scheme}

			require.NoError(t, r.reportStatus(
				ctx,
				cluster,
				nil,  // no StatefulSets in broker mode
				true, // brokerMode
				"rp.test.svc.cluster.local",
				"rp-cluster.test.svc.cluster.local",
				8081,
				types.NamespacedName{Name: "rp-external", Namespace: "test"},
				types.NamespacedName{Name: "rp-bootstrap", Namespace: "test"},
			))

			var got vectorizedv1alpha1.Cluster
			require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &got))
			require.Equal(t, tc.wantVersion, got.Status.Version,
				"Status.Version must record the rolled-out version, not the requested one")
		})
	}
}
