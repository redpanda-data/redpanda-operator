// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func pauseReconciliation(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster redpandav1alpha2.Redpanda
	require.NoError(t, t.Get(ctx, key, &cluster))

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations["cluster.redpanda.com/managed"] = "false"
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Paused reconciliation on cluster %q", clusterName)
}

func orphanDeleteStatefulSet(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var sts appsv1.StatefulSet
	require.NoError(t, t.Get(ctx, key, &sts))
	require.NoError(t, t.Delete(ctx, &sts, runtimeclient.PropagationPolicy(metav1.DeletePropagationOrphan)))
	t.Logf("Orphan-deleted StatefulSet %q", clusterName)
}

func createBrokerCRsForCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)

	var sts appsv1.StatefulSet
	// STS may already be gone if orphan-delete propagated; list pods by label instead.
	var pods corev1.PodList
	err := t.Get(ctx, key, &sts)
	if err == nil {
		sel, selErr := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		require.NoError(t, selErr)
		require.NoError(t, t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabelsSelector{Selector: sel}))
	} else {
		require.NoError(t, t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
			"app.kubernetes.io/name":     "redpanda",
		}))
	}
	require.NotEmpty(t, pods.Items, "no pods found for cluster %q", clusterName)

	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })

	for i, pod := range pods.Items {
		brokerName := fmt.Sprintf("%s-%d", key.Name, i)

		specBytes, err := yaml.Marshal(pod.Spec)
		require.NoError(t, err)
		checksum := fmt.Sprintf("%x", sha256.Sum256(specBytes))

		// Collect existing PVC names from the pod's volumes.
		var existingClaims []redpandav1alpha2.ExistingClaim
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil {
				existingClaims = append(existingClaims, redpandav1alpha2.ExistingClaim{
					Name: vol.PersistentVolumeClaim.ClaimName,
				})
			}
		}

		broker := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      brokerName,
				Namespace: key.Namespace,
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef: redpandav1alpha2.ClusterRef{
					Name: clusterName,
				},
				NetworkIndex: ptr.To(int32(i)),
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Labels:      pod.Labels,
					Annotations: map[string]string{"config.redpanda.com/checksum": checksum},
					Spec:        pod.Spec,
				},
				Storage: redpandav1alpha2.BrokerStorage{
					ExistingClaims: existingClaims,
				},
			},
		}
		require.NoError(t, t.Create(ctx, broker))
		t.Logf("Created Broker CR %q (networkIndex=%d)", brokerName, i)
	}

	t.Cleanup(func(ctx context.Context) {
		t := framework.T(ctx)
		var brokers redpandav1alpha2.BrokerList
		_ = t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace))
		for i := range brokers.Items {
			_ = t.Delete(ctx, &brokers.Items[i])
		}
	})
}

func grantRollGrants(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var brokers redpandav1alpha2.BrokerList
	require.NoError(t, t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace)))

	deadline := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	for i := range brokers.Items {
		b := &brokers.Items[i]
		checksum := b.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]
		require.NotEmpty(t, checksum, "broker %q missing config checksum", b.Name)

		patch := runtimeclient.MergeFrom(b.DeepCopy())
		if b.Annotations == nil {
			b.Annotations = map[string]string{}
		}
		b.Annotations["operator.redpanda.com/roll-grant"] = checksum + "/" + deadline
		require.NoError(t, t.Patch(ctx, b, patch))
		t.Logf("Granted roll-grant to Broker %q", b.Name)
	}
}

func allBrokerCRsRunning(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace)); err != nil {
			return false
		}
		if len(brokers.Items) == 0 {
			return false
		}
		for _, b := range brokers.Items {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseRunning {
				t.Logf("Broker %q phase=%s (want Running)", b.Name, b.Status.Phase)
				return false
			}
		}
		return true
	}, 5*time.Minute, 5*time.Second, "not all Broker CRs reached Running phase")
}

func setDecommissionOnBroker(ctx context.Context, t framework.TestingT, brokerName, _ string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	patch := runtimeclient.MergeFrom(broker.DeepCopy())
	broker.Spec.Decommission = true
	require.NoError(t, t.Patch(ctx, &broker, patch))
	t.Logf("Set decommission=true on Broker %q", brokerName)
}

func brokerShouldReachPhase(ctx context.Context, t framework.TestingT, brokerName, phase string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, key, &broker); err != nil {
			return false
		}
		t.Logf("Broker %q phase=%s (want %s)", brokerName, broker.Status.Phase, phase)
		return string(broker.Status.Phase) == phase
	}, 5*time.Minute, 5*time.Second, "Broker %q never reached phase %q", brokerName, phase)
}

func updateBrokerPodTemplateEnv(ctx context.Context, t framework.TestingT, brokerName, envKeyValue, _ string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	parts := strings.SplitN(envKeyValue, "=", 2)
	require.Len(t, parts, 2, "env must be KEY=VALUE, got %q", envKeyValue)

	broker.Spec.PodTemplate.Spec.Containers[0].Env = append(
		broker.Spec.PodTemplate.Spec.Containers[0].Env,
		corev1.EnvVar{Name: parts[0], Value: parts[1]},
	)

	specBytes, err := yaml.Marshal(broker.Spec.PodTemplate.Spec)
	require.NoError(t, err)
	checksum := fmt.Sprintf("%x", sha256.Sum256(specBytes))
	broker.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"] = checksum

	require.NoError(t, t.Update(ctx, &broker))
	t.Logf("Updated Broker %q pod template: added env %s, new checksum %s", brokerName, envKeyValue, checksum[:12])
}

func brokerPodShouldHaveEnv(ctx context.Context, t framework.TestingT, brokerName, envName, envValue string) {
	key := t.ResourceKey(brokerName)
	require.Eventually(t, func() bool {
		var broker redpandav1alpha2.Broker
		if err := t.Get(ctx, key, &broker); err != nil {
			return false
		}
		var pod corev1.Pod
		if err := t.Get(ctx, runtimeclient.ObjectKey{Name: broker.PodName(), Namespace: key.Namespace}, &pod); err != nil {
			return false
		}
		for _, c := range pod.Spec.Containers {
			for _, e := range c.Env {
				if e.Name == envName && e.Value == envValue {
					return true
				}
			}
		}
		t.Logf("Broker %q pod %q does not yet have env %s=%s", brokerName, pod.Name, envName, envValue)
		return false
	}, 5*time.Minute, 5*time.Second, "Broker %q pod never got env %s=%s", brokerName, envName, envValue)
}

func clusterAdminAPIShouldShowBrokers(ctx context.Context, t framework.TestingT, clusterName string, count int) {
	clients := clientsForCluster(ctx, clusterName)
	admin := clients.RedpandaAdmin(ctx)

	require.Eventually(t, func() bool {
		brokers, err := admin.Brokers(ctx)
		if err != nil {
			t.Logf("Failed to get brokers: %v", err)
			return false
		}
		t.Logf("Cluster %q has %d brokers (want %d)", clusterName, len(brokers), count)
		return len(brokers) == count
	}, 5*time.Minute, 5*time.Second, "cluster %q never had %d brokers", clusterName, count)
}
