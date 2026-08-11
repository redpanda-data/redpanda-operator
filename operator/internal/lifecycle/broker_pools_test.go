// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// installBrokerCRD installs the real Broker CRD (from config/crd/bases) into
// the test API server so broker-mode pool fetching can be exercised against
// the actual schema.
func installBrokerCRD(ctx context.Context, t *testing.T, cl client.Client) {
	t.Helper()

	data, err := os.ReadFile("../../config/crd/bases/cluster.redpanda.com_brokers.yaml")
	require.NoError(t, err)

	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(data, &crd))
	require.NoError(t, installCRD(ctx, cl, &crd))
}

func testBroker(cluster *MockCluster, clusterRefName string, index int32, templateHash string, decommission bool) *redpandav1alpha2.Broker {
	b := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-broker-%d", clusterRefName, index),
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"cluster-name": cluster.Name,
			},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef: redpandav1alpha2.ClusterRef{
				Name: clusterRefName,
			},
			NetworkIndex: ptr.To(index),
			Decommission: decommission,
			PodTemplate: redpandav1alpha2.BrokerPodTemplate{
				Annotations: map[string]string{
					redpandav1alpha2.BrokerPodTemplateHashAnnotation: templateHash,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "redpanda",
						Image: "redpanda:latest",
					}},
				},
			},
		},
	}
	return b
}

func createBrokerPod(ctx context.Context, t *testing.T, cl client.Client, name, podHash string, ready bool) {
	t.Helper()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				redpandav1alpha2.BrokerPodTemplateHashAnnotation: podHash,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "redpanda",
				Image: "redpanda:latest",
			}},
		},
	}
	require.NoError(t, cl.Create(ctx, pod))

	pod.Status.Phase = corev1.PodRunning
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: status},
		{Type: corev1.ContainersReady, Status: status},
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  "redpanda",
		Ready: ready,
	}}
	require.NoError(t, cl.Status().Update(ctx, pod))
}

func TestClientFetchBrokerBackedPools(t *testing.T) {
	stsBackedPool := &MulticlusterStatefulSet{
		clusterName: mcmanager.LocalCluster,
		StatefulSet: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sts-backed",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"label": "label"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"label": "label"},
					},
				},
			},
		},
	}

	tt := &clientTest{
		nodePools: []*MulticlusterStatefulSet{stsBackedPool},
	}

	tt.Run(parentCtx, t, "broker-backed-pools", func(t *testing.T, instances *clientTestInstances, cluster *MockCluster) {
		ctx, cancel := setupContext()
		defer cancel()

		installBrokerCRD(ctx, t, instances.k8sClient)

		// Pool "rp" is fully broker-backed (no StatefulSet):
		//   - broker 0: pod current + ready
		//   - broker 1: pod outdated (stale template hash) + unready
		//   - broker 2: decommission intent, pod current + ready
		brokers := []*redpandav1alpha2.Broker{
			testBroker(cluster, "rp", 0, "hash-0", false),
			testBroker(cluster, "rp", 1, "hash-1", false),
			testBroker(cluster, "rp", 2, "hash-2", true),
		}
		// Pool "sts-backed" still has a live StatefulSet with a shadow
		// Broker (mid-migration) — the STS view must stay authoritative.
		brokers = append(brokers, testBroker(cluster, "sts-backed", 0, "hash-s", false))

		for _, b := range brokers {
			require.NoError(t, controllerutil.SetControllerReference(cluster, b, instances.k8sClient.Scheme()))
			require.NoError(t, instances.k8sClient.Create(ctx, b))
		}

		createBrokerPod(ctx, t, instances.k8sClient, "rp-0", "hash-0", true)
		createBrokerPod(ctx, t, instances.k8sClient, "rp-1", "stale-hash", false)
		createBrokerPod(ctx, t, instances.k8sClient, "rp-2", "hash-2", true)

		// Materialize the STS-backed pool.
		require.NoError(t, instances.resourceClient.PatchPoolSet(ctx, cluster, stsBackedPool))

		tracker, err := instances.resourceClient.FetchExistingAndDesiredPools(ctx, cluster, "version", nil, true)
		require.NoError(t, err)

		existing := tracker.ExistingStatefulSets()
		require.Len(t, existing, 2)
		require.Contains(t, existing, ClusterNamespacedName{
			Cluster:              mcmanager.LocalCluster,
			CanonicalClusterName: instances.manager.GetLocalClusterName(),
			Namespace:            metav1.NamespaceDefault,
			Name:                 "rp",
		}.String())
		require.Contains(t, existing, ClusterNamespacedName{
			Cluster:              mcmanager.LocalCluster,
			CanonicalClusterName: instances.manager.GetLocalClusterName(),
			Namespace:            metav1.NamespaceDefault,
			Name:                 "sts-backed",
		}.String())

		require.True(t, tracker.AnyReady())

		var rpStatus *PoolStatus
		for _, status := range tracker.PoolStatuses() {
			if status.Name == "rp" {
				rpStatus = ptr.To(status)
			}
		}
		require.NotNil(t, rpStatus)
		// Two brokers without decommission intent, three pods total: the
		// decommissioning broker's pod shows up as condemned.
		require.Equal(t, int32(2), rpStatus.DesiredReplicas)
		require.Equal(t, int32(3), rpStatus.Replicas)
		require.Equal(t, int32(2), rpStatus.ReadyReplicas)
		require.Equal(t, int32(1), rpStatus.CondemnedReplicas)
		// broker 1's pod carries a stale template hash.
		require.Equal(t, int32(2), rpStatus.UpToDateReplicas)

		// A decommission is in flight (spec 2 vs 3 pods) — scale ops hold.
		require.False(t, tracker.CheckScale(ctx))

		// Without broker mode the broker-backed pool is invisible.
		tracker, err = instances.resourceClient.FetchExistingAndDesiredPools(ctx, cluster, "version", nil, false)
		require.NoError(t, err)
		require.Len(t, tracker.ExistingStatefulSets(), 1)
	})
}
