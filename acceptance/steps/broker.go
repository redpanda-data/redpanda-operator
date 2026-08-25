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
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
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

	// The Redpanda controller recreates a deleted StatefulSet if a racing
	// pre-pause reconcile is still in flight; a recreated STS would re-adopt
	// the orphaned pods and corrupt the broker-count assertions. Fail fast
	// with a clear message instead (mirrors the integration-test guard).
	require.Eventually(t, func() bool {
		var recreated appsv1.StatefulSet
		return apierrors.IsNotFound(t.Get(ctx, key, &recreated))
	}, time.Minute, 2*time.Second, "StatefulSet %q still exists after orphan-delete (recreated by a racing reconcile?)", clusterName)
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

		// The live pod spec carries instance-specific scheduling state.
		// NodeName in particular would pin every replacement pod to the
		// original node — if that node is gone, scheduling times out.
		podSpec := pod.Spec
		podSpec.NodeName = ""

		specBytes, err := yaml.Marshal(podSpec)
		require.NoError(t, err)
		checksum := fmt.Sprintf("%x", sha256.Sum256(specBytes))

		// Collect existing PVC names from the pod's volumes.
		// For StatefulSet pods, the ClaimName is already the full PVC name
		// (e.g. "datadir-broker-test-0"), not the VolumeClaimTemplate name.
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
				Labels: map[string]string{
					redpandav1alpha2.ClusterNameLabel: clusterName,
				},
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef: redpandav1alpha2.ClusterRef{
					Name: clusterName,
				},
				NetworkIndex: ptr.To(int32(i)),
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Labels:      pod.Labels,
					Annotations: map[string]string{redpandav1alpha2.BrokerConfigChecksumAnnotation: checksum},
					Spec:        podSpec,
				},
				Storage: redpandav1alpha2.BrokerStorage{
					ExistingClaims: existingClaims,
				},
			},
		}
		// Stamp the rotation identity the way every template writer must;
		// the adoption backfill copies it onto the pre-existing pod.
		broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = broker.Spec.PodTemplate.Hash()
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

func grantRollGrantToBroker(ctx context.Context, t framework.TestingT, brokerName string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	// Grants are keyed on the pod-template hash — the rotation identity.
	templateHash := broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	require.NotEmpty(t, templateHash, "broker %q missing pod-template hash", brokerName)

	patch := runtimeclient.MergeFrom(broker.DeepCopy())
	if broker.Annotations == nil {
		broker.Annotations = map[string]string{}
	}
	broker.Annotations[feature.RollGrant.Key] = feature.FormatRollGrant(templateHash, time.Now().Add(feature.RollGrantTTL))
	require.NoError(t, t.Patch(ctx, &broker, patch))
	t.Logf("Granted roll-grant to Broker %q", brokerName)
}

func allBrokerCRsRunning(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			redpandav1alpha2.ClusterNameLabel: clusterName,
		}); err != nil {
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

// allBrokerCRsStable waits for every Broker CR of the cluster to report the
// Stable roll-up condition as True: Ready, StorageBound, BrokerRegistered,
// ConfigSynced and Quiesced all hold.
func allBrokerCRsStable(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		if len(brokers.Items) == 0 {
			return false
		}
		for _, b := range brokers.Items {
			cond := apimeta.FindStatusCondition(b.Status.Conditions, "Stable")
			if cond == nil || cond.Status != metav1.ConditionTrue {
				status := "<missing>"
				if cond != nil {
					status = string(cond.Status)
				}
				t.Logf("Broker %q condition Stable=%s (want True)", b.Name, status)
				return false
			}
		}
		return true
	}, 10*time.Minute, 5*time.Second, "not all Broker CRs for cluster %q became Stable", clusterName)
}

func setDecommissionOnBroker(ctx context.Context, t framework.TestingT, brokerName string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	patch := runtimeclient.MergeFrom(broker.DeepCopy())
	broker.Spec.Decommission = true
	require.NoError(t, t.Patch(ctx, &broker, patch))
	t.Logf("Set decommission=true on Broker %q", brokerName)
}

type decommissionedBrokerKey struct {
	cluster string
	index   int32
}

// setDecommissionOnBrokerWithIndex marks the Broker with the given network
// index for decommission and records its UID so a later step can assert the
// CR was replaced rather than recommissioned.
func setDecommissionOnBrokerWithIndex(ctx context.Context, t framework.TestingT, index int, clusterName string) context.Context {
	key := t.ResourceKey(clusterName)
	var brokers redpandav1alpha2.BrokerList
	require.NoError(t, t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
	}))

	var target *redpandav1alpha2.Broker
	for i := range brokers.Items {
		if ptr.Deref(brokers.Items[i].Spec.NetworkIndex, -1) == int32(index) {
			target = &brokers.Items[i]
			break
		}
	}
	require.NotNil(t, target, "no Broker with index %d for cluster %q", index, clusterName)

	patch := runtimeclient.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, t.Patch(ctx, target, patch))
	t.Logf("Set decommission=true on Broker %q (index %d, UID %s)", target.Name, index, target.UID)
	return context.WithValue(ctx, decommissionedBrokerKey{clusterName, int32(index)}, string(target.UID))
}

// brokerWithIndexShouldBeReplaced waits until the Broker CR recorded by
// setDecommissionOnBrokerWithIndex has been deleted and a fresh Broker
// (different UID, no decommission intent) exists at the same network index.
func brokerWithIndexShouldBeReplaced(ctx context.Context, t framework.TestingT, index int, clusterName string) {
	oldUID, ok := ctx.Value(decommissionedBrokerKey{clusterName, int32(index)}).(string)
	require.True(t, ok, "no decommissioned-broker record for cluster %q index %d", clusterName, index)

	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		for i := range brokers.Items {
			b := &brokers.Items[i]
			if ptr.Deref(b.Spec.NetworkIndex, -1) != int32(index) {
				continue
			}
			if string(b.UID) == oldUID {
				t.Logf("Broker index %d is still the old CR (phase=%s)", index, b.Status.Phase)
				return false
			}
			t.Logf("Broker index %d replaced by %q (phase=%s)", index, b.Name, b.Status.Phase)
			return !b.Spec.Decommission
		}
		t.Logf("Broker index %d: old CR gone, waiting for replacement", index)
		return false
	}, 10*time.Minute, 5*time.Second, "Broker with index %d for cluster %q was never replaced", index, clusterName)
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

	parts := strings.SplitN(envKeyValue, "=", 2)
	require.Len(t, parts, 2, "env must be KEY=VALUE, got %q", envKeyValue)

	var checksum string
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var broker redpandav1alpha2.Broker
		if err := t.Get(ctx, key, &broker); err != nil {
			return err
		}

		broker.Spec.PodTemplate.Spec.Containers[0].Env = append(
			broker.Spec.PodTemplate.Spec.Containers[0].Env,
			corev1.EnvVar{Name: parts[0], Value: parts[1]},
		)

		specBytes, err := yaml.Marshal(broker.Spec.PodTemplate.Spec)
		if err != nil {
			return err
		}
		checksum = fmt.Sprintf("%x", sha256.Sum256(specBytes))
		broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = checksum
		// Every template writer must re-stamp the rotation identity after a
		// mutation — grants are keyed on it.
		broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = broker.Spec.PodTemplate.Hash()

		return t.Update(ctx, &broker)
	}))
	t.Logf("Updated Broker %q pod template: added env %s, new checksum %s", brokerName, envKeyValue, checksum[:12])
}

// brokerPodShouldNotBeRotated asserts for a fixed window that the Broker's
// pod is neither deleted nor recreated — the negative half of the roll-grant
// gate: an outdated pod WITHOUT a grant must stay put. Without this check, a
// gate that always allowed rotation would be observationally identical to a
// working one.
func brokerPodShouldNotBeRotated(ctx context.Context, t framework.TestingT, brokerName string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	podKey := runtimeclient.ObjectKey{Name: broker.PodName(), Namespace: key.Namespace}
	var pod corev1.Pod
	require.NoError(t, t.Get(ctx, podKey, &pod))
	originalUID := pod.UID

	require.Never(t, func() bool {
		var current corev1.Pod
		if err := t.Get(ctx, podKey, &current); err != nil {
			return true // pod deleted = rotation started
		}
		return current.UID != originalUID
	}, 30*time.Second, 2*time.Second, "pod %q was rotated without a roll-grant", broker.PodName())
	t.Logf("Pod %q kept UID %s without a roll-grant", broker.PodName(), originalUID)
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

func brokerShouldNotBeInMaintenanceMode(ctx context.Context, t framework.TestingT, brokerName, clusterName string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))
	require.NotNil(t, broker.Status.BrokerID, "broker %q has no broker ID", brokerName)
	brokerID := int(*broker.Status.BrokerID)

	clients := clientsForCluster(ctx, clusterName)
	admin := clients.RedpandaAdmin(ctx)

	brokers, err := admin.Brokers(ctx)
	require.NoError(t, err)

	for _, b := range brokers {
		if b.NodeID == brokerID {
			draining := b.Maintenance != nil && b.Maintenance.Draining
			t.Logf("Broker %q (nodeID=%d) maintenance=%+v", brokerName, brokerID, b.Maintenance)
			require.False(t, draining, "broker %q (nodeID=%d) is still in maintenance mode", brokerName, brokerID)
			return
		}
	}
	t.Fatalf("broker %q (nodeID=%d) not found in admin API response", brokerName, brokerID)
}

func brokerPVCsShouldBeOwnedByBrokerCR(ctx context.Context, t framework.TestingT, brokerName, _ string) {
	key := t.ResourceKey(brokerName)
	var broker redpandav1alpha2.Broker
	require.NoError(t, t.Get(ctx, key, &broker))

	require.NotEmpty(t, broker.Spec.Storage.ExistingClaims, "Broker %q has no ExistingClaims", brokerName)
	for _, ec := range broker.Spec.Storage.ExistingClaims {
		var pvc corev1.PersistentVolumeClaim
		require.NoError(t, t.Get(ctx, runtimeclient.ObjectKey{Name: ec.Name, Namespace: key.Namespace}, &pvc))

		ctrl := metav1.GetControllerOf(&pvc)
		require.NotNil(t, ctrl, "PVC %q has no controller ownerRef", ec.Name)
		require.Equal(t, broker.Name, ctrl.Name, "PVC %q controller ownerRef name mismatch", ec.Name)
		require.Equal(t, "Broker", ctrl.Kind, "PVC %q controller ownerRef kind mismatch", ec.Name)
		t.Logf("PVC %q is owned by Broker %q", ec.Name, broker.Name)
	}
}

func allBrokerCRsShouldHaveConditions(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var brokers redpandav1alpha2.BrokerList
	require.NoError(t, t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace)))
	require.NotEmpty(t, brokers.Items)

	for _, b := range brokers.Items {
		for _, condType := range []string{"Ready", "PodScheduled", "BrokerRegistered", "StorageBound"} {
			found := false
			for _, c := range b.Status.Conditions {
				if c.Type == condType {
					found = true
					require.Equal(t, string(metav1.ConditionTrue), string(c.Status),
						"Broker %q condition %s is %s (want True)", b.Name, condType, c.Status)
					break
				}
			}
			require.True(t, found, "Broker %q missing condition %s", b.Name, condType)
		}
		t.Logf("Broker %q has all 4 conditions True", b.Name)
	}
}

func brokerShouldHaveCondition(ctx context.Context, t framework.TestingT, brokerName, condType, expectedStatus string) {
	key := t.ResourceKey(brokerName)
	want := metav1.ConditionStatus(expectedStatus)
	require.Eventually(t, func() bool {
		var broker redpandav1alpha2.Broker
		if err := t.Get(ctx, key, &broker); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(broker.Status.Conditions, condType)
		if cond == nil {
			t.Logf("Broker %q missing condition %s", brokerName, condType)
			return false
		}
		if cond.Status != want {
			t.Logf("Broker %q condition %s=%s (want %s)", brokerName, condType, cond.Status, want)
			return false
		}
		return true
	}, 2*time.Minute, 5*time.Second, "Broker %q condition %s never reached %s", brokerName, condType, expectedStatus)
}

func clusterShouldHaveNBrokerCRs(ctx context.Context, t framework.TestingT, clusterName string, count int) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		t.Logf("Cluster %q has %d Broker CRs (want %d)", clusterName, len(brokers.Items), count)
		return len(brokers.Items) == count
	}, 5*time.Minute, 5*time.Second, "cluster %q never had %d Broker CRs", clusterName, count)
}

func noStatefulSetShouldExistForCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var stsList appsv1.StatefulSetList
	require.NoError(t, t.List(ctx, &stsList, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
	}))
	require.Empty(t, stsList.Items, "expected no StatefulSets for cluster %q, found %d", clusterName, len(stsList.Items))
}

func clusterAdminAPIShouldShowBrokers(ctx context.Context, t framework.TestingT, clusterName string, count int) {
	clients := resolveClusterClients(ctx, clusterName)
	require.Eventually(t, func() bool {
		admin, err := clients.factory.RedpandaAdminClient(ctx, clients.resourceTarget)
		if err != nil {
			t.Logf("Failed to create admin client: %v", err)
			return false
		}
		defer admin.Close()
		brokers, err := admin.Brokers(ctx)
		if err != nil {
			t.Logf("Failed to get brokers: %v", err)
			return false
		}
		t.Logf("Cluster %q has %d brokers (want %d)", clusterName, len(brokers), count)
		return len(brokers) == count
	}, 5*time.Minute, 5*time.Second, "cluster %q never had %d brokers", clusterName, count)
}

func resolveClusterClients(ctx context.Context, clusterName string) *clusterClients {
	t := framework.T(ctx)
	key := t.ResourceKey(clusterName)

	var v2 redpandav1alpha2.Redpanda
	if err := t.Get(ctx, key, &v2); err == nil {
		return clientsForCluster(ctx, clusterName)
	}
	return v1ClientsForCluster(ctx, clusterName)
}

func setAnnotationOnV1Cluster(ctx context.Context, t framework.TestingT, annotationKey, annotationValue, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cluster vectorizedv1alpha1.Cluster
		if err := t.Get(ctx, key, &cluster); err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		cluster.Annotations[annotationKey] = annotationValue
		return t.Update(ctx, &cluster)
	}))
	t.Logf("Set annotation %s=%s on V1 cluster %q", annotationKey, annotationValue, clusterName)
}

func removeAnnotationFromV1Cluster(ctx context.Context, t framework.TestingT, annotationKey, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cluster vectorizedv1alpha1.Cluster
		if err := t.Get(ctx, key, &cluster); err != nil {
			return err
		}
		delete(cluster.Annotations, annotationKey)
		return t.Update(ctx, &cluster)
	}))
	t.Logf("Removed annotation %s from V1 cluster %q", annotationKey, clusterName)
}

func setAnnotationOnRedpanda(ctx context.Context, t framework.TestingT, annotationKey, annotationValue, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cluster redpandav1alpha2.Redpanda
		if err := t.Get(ctx, key, &cluster); err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		cluster.Annotations[annotationKey] = annotationValue
		return t.Update(ctx, &cluster)
	}))
	t.Logf("Set annotation %s=%s on Redpanda %q", annotationKey, annotationValue, clusterName)
}

func removeAnnotationFromRedpanda(ctx context.Context, t framework.TestingT, annotationKey, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cluster redpandav1alpha2.Redpanda
		if err := t.Get(ctx, key, &cluster); err != nil {
			return err
		}
		delete(cluster.Annotations, annotationKey)
		return t.Update(ctx, &cluster)
	}))
	t.Logf("Removed annotation %s from Redpanda %q", annotationKey, clusterName)
}

func statefulSetShouldExistForCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var stsList appsv1.StatefulSetList
	require.NoError(t, t.List(ctx, &stsList, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
	}))
	require.NotEmpty(t, stsList.Items, "expected at least one StatefulSet for cluster %q", clusterName)
}

func noStatefulSetShouldEventuallyExistForCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var stsList appsv1.StatefulSetList
		if err := t.List(ctx, &stsList, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		t.Logf("Cluster %q has %d StatefulSets (want 0)", clusterName, len(stsList.Items))
		return len(stsList.Items) == 0
	}, 5*time.Minute, 5*time.Second, "cluster %q still has StatefulSets", clusterName)
}

func statefulSetShouldEventuallyExistForCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var stsList appsv1.StatefulSetList
		if err := t.List(ctx, &stsList, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		t.Logf("Cluster %q has %d StatefulSets (want >=1)", clusterName, len(stsList.Items))
		return len(stsList.Items) > 0
	}, 5*time.Minute, 5*time.Second, "cluster %q never got a StatefulSet", clusterName)
}

type podUIDSnapshotKey struct{ cluster string }

func snapshotPodUIDs(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	key := t.ResourceKey(clusterName)
	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "redpanda",
	}))
	uids := map[string]string{}
	for _, p := range pods.Items {
		uids[p.Name] = string(p.UID)
	}
	t.Logf("Snapshot %d pod UIDs for cluster %q: %v", len(uids), clusterName, uids)
	return context.WithValue(ctx, podUIDSnapshotKey{clusterName}, uids)
}

// podsShouldHaveNoContainerRestarts asserts that no container of any of the
// cluster's pods has ever restarted. It complements the pod-UID checks: UID
// stability proves no pod was recreated, but a pod can keep its UID while a
// container inside it crash-loops — and conversely a recreated pod starts
// back at zero restarts, so neither check subsumes the other.
func podsShouldHaveNoContainerRestarts(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "redpanda",
	}))
	require.NotEmpty(t, pods.Items, "no pods found for cluster %q", clusterName)
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			require.Zerof(t, cs.RestartCount, "container %q of pod %q restarted %d time(s)", cs.Name, p.Name, cs.RestartCount)
		}
	}
	t.Logf("All containers of %d pods for cluster %q have zero restarts", len(pods.Items), clusterName)
}

func podUIDsShouldBeUnchanged(ctx context.Context, t framework.TestingT, clusterName string) {
	snap, ok := ctx.Value(podUIDSnapshotKey{clusterName}).(map[string]string)
	require.True(t, ok, "no pod UID snapshot found for cluster %q", clusterName)

	key := t.ResourceKey(clusterName)
	// The pod set is eventually consistent with the step sequence around it:
	// after a scale-down the excess pod lingers in Terminating for its grace
	// period after its Broker CR (and admin-API membership) are already gone.
	// Extra or lagging pods converge, so retry on them; a CHANGED UID on a
	// snapshotted pod never converges (the pod was restarted), so bail out
	// immediately and let the assertion below report it.
	var current map[string]string
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		if err := t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
			"app.kubernetes.io/name":     "redpanda",
		}); err != nil {
			t.Logf("failed to list pods: %v", err)
			return false
		}
		current = map[string]string{}
		for _, p := range pods.Items {
			current[p.Name] = string(p.UID)
		}
		for name, oldUID := range snap {
			if newUID, ok := current[name]; ok && newUID != oldUID {
				return true
			}
		}
		return maps.Equal(snap, current)
	}, 2*time.Minute, 2*time.Second, "pod set for cluster %q never converged to the UID snapshot; snapshot: %v, last seen: %v", clusterName, snap, current)
	for name, oldUID := range snap {
		newUID, exists := current[name]
		if !exists {
			t.Logf("Pod %q: MISSING (was UID %s)", name, oldUID)
		} else if newUID != oldUID {
			t.Logf("Pod %q: UID CHANGED %s -> %s (pod was recreated)", name, oldUID, newUID)
		} else {
			t.Logf("Pod %q: UID unchanged %s", name, oldUID)
		}
	}
	for name := range current {
		if _, ok := snap[name]; !ok {
			t.Logf("Pod %q: NEW (UID %s, not in snapshot)", name, current[name])
		}
	}
	require.Equal(t, snap, current, "pod UIDs changed for cluster %q — pods were restarted", clusterName)
	t.Logf("All %d pod UIDs for cluster %q are unchanged", len(current), clusterName)
}

func setNodePoolReplicasOnV1Cluster(ctx context.Context, t framework.TestingT, poolName string, replicas int, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster vectorizedv1alpha1.Cluster
	require.NoError(t, t.Get(ctx, key, &cluster))

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	found := false
	for i := range cluster.Spec.NodePools {
		if cluster.Spec.NodePools[i].Name == poolName {
			cluster.Spec.NodePools[i].Replicas = ptr.To(int32(replicas))
			found = true
		}
	}
	require.True(t, found, "nodePool %q not found on cluster %q", poolName, clusterName)
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Set nodePool %q replicas to %d on V1 cluster %q", poolName, replicas, clusterName)
}

// addNodePoolToV1Cluster appends a new nodePool cloned from the FIRST pool in
// the spec (storage, resources, tolerations — everything but name, replicas,
// and hostIndexOffset), so the pool schedules under the same constraints the
// scenario's manifest set up. The hostIndexOffset is bumped by 100 per
// existing pool to keep host indices collision-free.
func addNodePoolToV1Cluster(ctx context.Context, t framework.TestingT, poolName string, replicas int, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster vectorizedv1alpha1.Cluster
	require.NoError(t, t.Get(ctx, key, &cluster))
	require.NotEmpty(t, cluster.Spec.NodePools, "cluster %q has no nodePools to clone from", clusterName)

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	for _, np := range cluster.Spec.NodePools {
		require.NotEqual(t, poolName, np.Name, "nodePool %q already exists on cluster %q", poolName, clusterName)
	}
	pool := *cluster.Spec.NodePools[0].DeepCopy()
	pool.Name = poolName
	pool.Replicas = ptr.To(int32(replicas))
	pool.HostIndexOffset = cluster.Spec.NodePools[0].HostIndexOffset + 100*len(cluster.Spec.NodePools)
	cluster.Spec.NodePools = append(cluster.Spec.NodePools, pool)
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Added nodePool %q with %d replica(s) to V1 cluster %q", poolName, replicas, clusterName)
}

func removeNodePoolFromV1Cluster(ctx context.Context, t framework.TestingT, poolName, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster vectorizedv1alpha1.Cluster
	require.NoError(t, t.Get(ctx, key, &cluster))

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	kept := cluster.Spec.NodePools[:0]
	found := false
	for _, np := range cluster.Spec.NodePools {
		if np.Name == poolName {
			found = true
			continue
		}
		kept = append(kept, np)
	}
	require.True(t, found, "nodePool %q not found on cluster %q", poolName, clusterName)
	cluster.Spec.NodePools = kept
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Removed nodePool %q from V1 cluster %q", poolName, clusterName)
}

func addAdditionalConfigurationToV1Cluster(ctx context.Context, t framework.TestingT, configKey, configValue, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster vectorizedv1alpha1.Cluster
	require.NoError(t, t.Get(ctx, key, &cluster))

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	if cluster.Spec.AdditionalConfiguration == nil {
		cluster.Spec.AdditionalConfiguration = map[string]string{}
	}
	cluster.Spec.AdditionalConfiguration[configKey] = configValue
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Set additionalConfiguration %s=%s on V1 cluster %q", configKey, configValue, clusterName)
}

// podsShouldRollOneAtATime waits until every pod from the UID snapshot (taken
// via snapshotPodUIDs) has been replaced and is ready again, while asserting
// that at no sampled point more than one pod was being REPLACED — i.e. the
// cluster controller serialized the roll via roll-grants. "Being replaced"
// means deleted (missing) or recreated-but-never-yet-ready; once a
// replacement has been observed Ready its roll is complete and later
// readiness dips don't re-count it. Readiness alone is deliberately not the
// signal: the V1 readiness probe reports CLUSTER health, so every rotation's
// node-down window can flip neighboring pods unready, and a k3d agent
// NotReady blip marks all its pods unready at once — neither is a
// serialization violation.
func podsShouldRollOneAtATime(ctx context.Context, t framework.TestingT, clusterName string) {
	snap, ok := ctx.Value(podUIDSnapshotKey{clusterName}).(map[string]string)
	require.True(t, ok, "no pod UID snapshot found for cluster %q", clusterName)
	require.NotEmpty(t, snap)

	key := t.ResourceKey(clusterName)
	maxUnavailable := 0
	// wasReady latches per pod name once its REPLACEMENT has been observed
	// Ready: from then on that pod's roll is done, whatever its probe says.
	wasReady := map[string]bool{}
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		if err := t.List(ctx, &pods, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
			"app.kubernetes.io/name":     "redpanda",
		}); err != nil {
			return false
		}

		byName := map[string]*corev1.Pod{}
		for i := range pods.Items {
			byName[pods.Items[i].Name] = &pods.Items[i]
		}

		unavailable, replaced, readyNow := 0, 0, 0
		for name, oldUID := range snap {
			pod, exists := byName[name]
			replacedUID := exists && string(pod.UID) != oldUID
			ready := exists && utils.IsPodReady(pod)
			if replacedUID && ready {
				wasReady[name] = true
			}
			if ready {
				readyNow++
			}
			switch {
			case !exists || (replacedUID && !wasReady[name]):
				unavailable++
			case replacedUID:
				replaced++
			}
		}
		if unavailable > maxUnavailable {
			maxUnavailable = unavailable
		}
		t.Logf("Roll progress for cluster %q: %d/%d replaced, %d unavailable, %d ready (max unavailable seen %d)",
			clusterName, replaced, len(snap), unavailable, readyNow, maxUnavailable)
		// Done when every pod is replaced AND currently ready — the latch
		// only relaxes the mid-roll accounting, not the exit condition.
		return unavailable == 0 && replaced == len(snap) && readyNow == len(snap)
	}, 15*time.Minute, 2*time.Second, "pods for cluster %q never finished rolling", clusterName)

	require.LessOrEqual(t, maxUnavailable, 1,
		"more than one pod was unavailable at once during the roll — rolls are not serialized")
}

func clusterShouldEventuallyHaveNBrokerCRs(ctx context.Context, t framework.TestingT, clusterName string, count int) {
	key := t.ResourceKey(clusterName)
	require.Eventually(t, func() bool {
		var brokers redpandav1alpha2.BrokerList
		if err := t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}); err != nil {
			return false
		}
		t.Logf("Cluster %q has %d Broker CRs (want %d)", clusterName, len(brokers.Items), count)
		return len(brokers.Items) == count
	}, 5*time.Minute, 5*time.Second, "cluster %q never reached %d Broker CRs", clusterName, count)
}

// scaleTwoNodePoolsOnV1Cluster shrinks two nodePools in ONE update — the
// shape of a single kubectl apply touching both pools, which a single
// reconcile pass then observes together.
func scaleTwoNodePoolsOnV1Cluster(ctx context.Context, t framework.TestingT, poolA string, replicasA int, poolB string, replicasB int, clusterName string) {
	key := t.ResourceKey(clusterName)
	var cluster vectorizedv1alpha1.Cluster
	require.NoError(t, t.Get(ctx, key, &cluster))

	patch := runtimeclient.MergeFrom(cluster.DeepCopy())
	want := map[string]int32{poolA: int32(replicasA), poolB: int32(replicasB)}
	found := 0
	for i := range cluster.Spec.NodePools {
		if replicas, ok := want[cluster.Spec.NodePools[i].Name]; ok {
			cluster.Spec.NodePools[i].Replicas = ptr.To(replicas)
			found++
		}
	}
	require.Equal(t, 2, found, "expected both nodePools %q and %q on cluster %q", poolA, poolB, clusterName)
	require.NoError(t, t.Patch(ctx, &cluster, patch))
	t.Logf("Scaled nodePool %q to %d and nodePool %q to %d on V1 cluster %q in a single update", poolA, replicasA, poolB, replicasB, clusterName)
}

// atMostOneBrokerDecommissioningUntil polls the cluster's Broker CRs and
// fails the moment two are decommissioning concurrently (the cluster-wide
// one-disruptive-operation-at-a-time invariant), until the broker count
// settles at want with no decommission in flight.
func atMostOneBrokerDecommissioningUntil(ctx context.Context, t framework.TestingT, clusterName string, want int) {
	key := t.ResourceKey(clusterName)
	deadline := time.Now().Add(15 * time.Minute)
	for {
		require.Less(t, time.Now(), deadline,
			"cluster %q never settled at %d Broker CRs with drains finished", clusterName, want)

		var brokers redpandav1alpha2.BrokerList
		require.NoError(t, t.List(ctx, &brokers, runtimeclient.InNamespace(key.Namespace), runtimeclient.MatchingLabels{
			"app.kubernetes.io/instance": clusterName,
		}))
		var decommissioning []string
		for i := range brokers.Items {
			b := &brokers.Items[i]
			if b.Spec.Decommission && b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				decommissioning = append(decommissioning, fmt.Sprintf("%s(id=%v)", b.Name, b.Status.BrokerID))
			}
		}
		require.LessOrEqualf(t, len(decommissioning), 1,
			"one-disruptive-operation-at-a-time violated: %d Brokers decommissioning concurrently: %v",
			len(decommissioning), decommissioning)
		if len(brokers.Items) == want && len(decommissioning) == 0 {
			t.Logf("Cluster %q settled at %d Broker CRs with serialized decommissions", clusterName, want)
			return
		}
		t.Logf("Cluster %q: %d Broker CRs (want %d), decommissioning: %v", clusterName, len(brokers.Items), want, decommissioning)
		time.Sleep(time.Second)
	}
}
