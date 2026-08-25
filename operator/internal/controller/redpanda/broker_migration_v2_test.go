// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
)

// TestV2MigrationAndRollback exercises the annotation-driven StatefulSet →
// Broker CR migration against a live V2 Redpanda end to end: opting in via
// operator.redpanda.com/use-broker-cr adopts every pod in place (no
// restarts, no data movement), scaling works without a StatefulSet
// afterwards, and removing the annotation restores the StatefulSet from its
// backup and re-adopts the pods — again without restarts.
func (s *BrokerControllerSuite) TestV2MigrationAndRollback() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWait(t, ctx, c, rp)

	uidsBefore := s.brokerPodUIDs(t, ctx, c, rp, 3)

	// Opt in: migration must adopt the pods in place.
	s.setMigrationAnnotation(t, ctx, c, rp, ptr.To("true"))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
		assert.True(ct, apierrors.IsNotFound(err), "StatefulSet should be orphan-deleted")

		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 3) {
			return
		}
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase, "broker %q should be Running", b.Name)
			assert.True(ct, metav1.IsControlledBy(&b, rp), "broker %q should be controller-owned by the Redpanda", b.Name)

			var pod corev1.Pod
			if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: b.PodName(), Namespace: rp.Namespace}, &pod)) {
				continue
			}
			assert.Equal(ct, uidsBefore[pod.Name], pod.UID, "pod %q must be adopted in place, not recreated", pod.Name)
			ref := metav1.GetControllerOf(&pod)
			if assert.NotNil(ct, ref, "pod %q should have a controller owner", pod.Name) {
				assert.Equal(ct, "Broker", ref.Kind, "pod %q should be owned by its Broker", pod.Name)
			}
		}
	}, 5*time.Minute, 5*time.Second)

	s.waitForMigrationCondition(t, ctx, c, rp, "Complete")

	// Quiescence: adoption copied the live checksums, so nothing may be
	// pending a rotation and no roll grant may be outstanding.
	generations := map[string]int64{}
	for _, b := range s.listBrokers(t, ctx, c, rp) {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: b.PodName(), Namespace: rp.Namespace}, &pod))
		require.False(t, b.PodOutdated(&pod), "broker %q must not be pending a rotation after adoption", b.Name)
		require.Empty(t, b.Annotations[feature.RollGrant.Key], "broker %q must not hold a roll grant after adoption", b.Name)
		generations[b.Name] = b.Generation
	}

	// Quiescence must hold across reconciles: force one and verify Broker
	// specs don't churn. A desired render that doesn't compare equal to the
	// stored (API-defaulted) object would rewrite — and generation-bump —
	// every Broker CR on every pass.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	poke := client.MergeFrom(rp.DeepCopy())
	rp.Annotations["test.redpanda.com/quiescence-poke"] = "1"
	require.NoError(t, c.Patch(ctx, rp, poke))
	time.Sleep(20 * time.Second)
	for _, b := range s.listBrokers(t, ctx, c, rp) {
		require.Equal(t, generations[b.Name], b.Generation,
			"broker %q spec churned across reconciles on a converged cluster", b.Name)
	}

	// Scale up: a fourth Broker CR and pod appear with no StatefulSet
	// involved.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(4)
	s.applyAndWait(t, ctx, c, rp)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
		assert.True(ct, apierrors.IsNotFound(err), "no StatefulSet may reappear on scale-up")

		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 4) {
			return
		}
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase, "broker %q should be Running", b.Name)
		}
	}, 5*time.Minute, 5*time.Second)

	// The original pods must not have been touched by the scale-up.
	for name, uid := range uidsBefore {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod))
		require.Equal(t, uid, pod.UID, "pod %q must survive the scale-up untouched", name)
	}

	// Scale down: the excess broker is decommissioned and its CR deleted.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWait(t, ctx, c, rp)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 3) {
			return
		}
		for _, b := range brokers {
			assert.False(ct, b.Spec.Decommission, "broker %q should not carry decommission intent", b.Name)
		}
		var pod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-3", rp.Name), Namespace: rp.Namespace}, &pod)
		assert.True(ct, apierrors.IsNotFound(err), "the decommissioned broker's pod should be gone")
	}, 10*time.Minute, 5*time.Second)

	// Rollback: removing the annotation restores the StatefulSet from the
	// migration backup, which re-adopts the pods in place.
	s.setMigrationAnnotation(t, ctx, c, rp, nil)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		assert.Empty(ct, brokers, "all Broker CRs should be removed on rollback")

		var sts appsv1.StatefulSet
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)) {
			return
		}
		assert.Equal(ct, int32(3), sts.Status.ReadyReplicas, "restored StatefulSet should re-adopt all pods")
	}, 5*time.Minute, 5*time.Second)

	s.waitForMigrationCondition(t, ctx, c, rp, "RolledBack")

	for name, uid := range uidsBefore {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod))
		require.Equal(t, uid, pod.UID, "pod %q must survive the rollback untouched", name)
		ref := metav1.GetControllerOf(&pod)
		if assert.NotNil(t, ref) {
			assert.Equal(t, "StatefulSet", ref.Kind, "pod %q should be re-adopted by the StatefulSet", name)
		}
	}
}

// TestV2FreshClusterWithBrokerCRs verifies that a Redpanda created with the
// use-broker-cr annotation already set never gets a StatefulSet: the
// brokers are created as Broker CRs from day one.
func (s *BrokerControllerSuite) TestV2FreshClusterWithBrokerCRs() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = "true"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWait(t, ctx, c, rp)

	var sts appsv1.StatefulSet
	err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
	require.True(t, apierrors.IsNotFound(err), "a fresh broker-mode cluster must never create a StatefulSet")

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 3) {
			return
		}
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase, "broker %q should be Running", b.Name)
			assert.NotEmpty(ct, b.Spec.Storage.VolumeClaimTemplates, "fresh brokers use volume claim templates, not existing claims")
		}
	}, 5*time.Minute, 5*time.Second)
}

// TestV2NodePoolBrokers covers the V2 + NodePools ownership path from the
// RFC's table: Brokers of NodePool pools point their clusterRef at the
// NodePool (the Broker controller resolves the cluster through the NodePool's
// own clusterRef), while the implicit default pool points at the Redpanda.
func (s *BrokerControllerSuite) TestV2NodePoolBrokers() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = "true"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(1)

	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pool-" + testenv.RandString(6),
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalNodePoolSpec(rp),
	}
	pool.Spec.Image.Repository = ptr.To(os.Getenv("TEST_REDPANDA_REPO"))
	pool.Spec.Replicas = ptr.To(int32(2))
	require.NoError(t, c.Create(ctx, pool))

	s.applyAndWait(t, ctx, c, rp)

	var stsList appsv1.StatefulSetList
	require.NoError(t, c.List(ctx, &stsList, client.InNamespace(rp.Namespace)))
	require.Empty(t, stsList.Items, "a fresh broker-mode cluster must never create StatefulSets")

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 3) {
			return
		}

		var defaultPool, nodePool int
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase, "broker %q should be Running", b.Name)
			// Broker ID discovery requires the admin client to resolve
			// through the NodePool's own clusterRef for NodePool-referencing
			// Brokers.
			assert.NotNil(ct, b.Status.BrokerID, "broker %q should have discovered its broker ID", b.Name)
			assert.True(ct, apimeta.IsStatusConditionTrue(b.Status.Conditions, "BrokerRegistered"),
				"broker %q should be registered", b.Name)
			// The "Owner Kind" printer column reads .spec.clusterRef.kind
			// verbatim — it must be stamped, not defaulted.
			assert.NotNil(ct, b.Spec.ClusterRef.Kind, "broker %q should carry an explicit clusterRef kind", b.Name)

			if b.Spec.ClusterRef.IsNodePool() {
				nodePool++
				assert.Equal(ct, pool.Name, b.Spec.ClusterRef.Name)
				assert.Equal(ct, rp.Name, b.Labels[redpandav1alpha2.ClusterNameLabel])
				assert.Equal(ct, pool.Name, b.Labels[redpandav1alpha2.NodePoolLabel])
			} else {
				defaultPool++
				assert.Equal(ct, rp.Name, b.Spec.ClusterRef.Name)
				assert.Equal(ct, redpandav1alpha2.DefaultNodePoolName, b.Labels[redpandav1alpha2.NodePoolLabel])
			}
		}
		assert.Equal(ct, 1, defaultPool, "the implicit default pool should have one Redpanda-referencing Broker")
		assert.Equal(ct, 2, nodePool, "the NodePool should have two NodePool-referencing Brokers")

		for _, name := range []string{
			fmt.Sprintf("%s-0", rp.Name),
			fmt.Sprintf("%s-%s-0", rp.Name, pool.Name),
			fmt.Sprintf("%s-%s-1", rp.Name, pool.Name),
		} {
			var pod corev1.Pod
			assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod), "pod %q should exist", name)
		}
	}, 5*time.Minute, 5*time.Second)

	// Pod-template metadata flows from BOTH spec levels without a restart:
	// Redpanda.spec.clusterSpec.statefulset.podTemplate reaches the default
	// pool's pod, NodePool.spec.podTemplate reaches the pool's pods, via the
	// Broker controller's in-place metadata sync.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	rpPatch := client.MergeFrom(rp.DeepCopy())
	rp.Spec.ClusterSpec.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{
		Annotations: map[string]string{"test.redpanda.com/cluster-level": "yes"},
	}
	require.NoError(t, c.Patch(ctx, rp, rpPatch))

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pool), pool))
	poolPatch := client.MergeFrom(pool.DeepCopy())
	pool.Spec.PodTemplate = &redpandav1alpha2.PodTemplate{
		Annotations: map[string]string{"test.redpanda.com/pool-level": "yes"},
	}
	require.NoError(t, c.Patch(ctx, pool, poolPatch))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var pod corev1.Pod
		if assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-0", rp.Name), Namespace: rp.Namespace}, &pod)) {
			assert.Equal(ct, "yes", pod.Annotations["test.redpanda.com/cluster-level"],
				"Redpanda-level podTemplate annotations should reach the default pool's pod in place")
		}
		for _, name := range []string{
			fmt.Sprintf("%s-%s-0", rp.Name, pool.Name),
			fmt.Sprintf("%s-%s-1", rp.Name, pool.Name),
		} {
			if assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod)) {
				assert.Equal(ct, "yes", pod.Annotations["test.redpanda.com/pool-level"],
					"NodePool-level podTemplate annotations should reach pod %q in place", name)
			}
		}
	}, 5*time.Minute, 5*time.Second)

	// Metadata sync must never have rotated anything: same pods throughout.
	for _, b := range s.listBrokers(t, ctx, c, rp) {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: b.PodName(), Namespace: rp.Namespace}, &pod))
		require.False(t, b.PodOutdated(&pod), "broker %q must not be pending a rotation after a metadata-only change", b.Name)
	}

	// Removing the NodePool drains its brokers: decommission intent one at a
	// time, executed by the Broker controller — whose admin resolution must
	// fall back to the controller owner, the NodePool being gone (its
	// deletion is not gated on the drain). The default pool's broker and the
	// rest of the reconcile chain must keep operating throughout.
	require.NoError(t, c.Delete(ctx, pool))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 1, "the removed pool's brokers should drain away") {
			return
		}
		assert.False(ct, brokers[0].Spec.ClusterRef.IsNodePool(), "the surviving broker belongs to the default pool")
		assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, brokers[0].Status.Phase)

		for _, name := range []string{
			fmt.Sprintf("%s-%s-0", rp.Name, pool.Name),
			fmt.Sprintf("%s-%s-1", rp.Name, pool.Name),
		} {
			var pod corev1.Pod
			err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod)
			assert.True(ct, apierrors.IsNotFound(err), "drained pool's pod %q should be gone", name)
		}
	}, 10*time.Minute, 5*time.Second)
}

// TestV2BrokerModeFlagOffOperatorDoesNotCreateStatefulSet pins the downgrade
// safety of broker mode: a reconciler running WITHOUT --enable-broker (an
// operator downgrade, or a values regression) must never start managing a
// broker-mode cluster's pods through StatefulSets. Without a guard, the
// broker-backed pools are invisible to the flag-off tracker, its plain path
// renders a fresh StatefulSet against pods the Broker CRs own, and the roll
// loop then fights the Broker controller over them — a silent unrequested
// fleet restart.
func (s *BrokerControllerSuite) TestV2BrokerModeFlagOffOperatorDoesNotCreateStatefulSet() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = "true"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(1)
	s.applyAndWait(t, ctx, c, rp)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 1) {
			return
		}
		assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, brokers[0].Status.Phase)
	}, 5*time.Minute, 5*time.Second)
	uids := s.brokerPodUIDs(t, ctx, c, rp, 1)

	// The downgraded operator: identical wiring, --enable-broker off.
	flagOff := &redpanda.RedpandaReconciler{
		Manager:       s.mgr,
		ClientFactory: s.clientFactory,
		LifecycleClient: lifecycle.NewResourceClient(s.mgr, lifecycle.V2ResourceManagers(
			lifecycle.Image{Repository: os.Getenv("TEST_REDPANDA_REPO"), Tag: os.Getenv("TEST_REDPANDA_VERSION")},
			lifecycle.Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
			lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false},
		)),
		UseNodePools:    true,
		BrokerCREnabled: false,
	}
	req := mcreconcile.Request{
		Request:     reconcile.Request{NamespacedName: client.ObjectKeyFromObject(rp)},
		ClusterName: mcmanager.LocalCluster,
	}
	for range 3 {
		if _, err := flagOff.Reconcile(ctx, req); err != nil {
			// Errors are acceptable (refusing loudly is a valid guard);
			// creating a StatefulSet is not.
			t.Logf("flag-off reconcile returned error: %v", err)
		}
	}

	var sts appsv1.StatefulSet
	err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
	require.Truef(t, apierrors.IsNotFound(err),
		"a reconciler without --enable-broker created StatefulSet %q for a broker-mode cluster; it would fight the Broker controller for the pods", rp.Name)
	for name, uid := range uids {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod))
		require.Equal(t, uid, pod.UID, "pod %q must survive flag-off reconciles untouched", name)
	}
}

// TestV2NodePoolDeployedGenerationAdvances pins broker-mode NodePool rollout
// tracking: after a NodePool spec change converges, Status.DeployedGeneration
// must catch up to metadata.generation — consumers (the cloud control plane)
// gate rollout completion on exactly that equality. The broker-mode status is
// derived from the Broker CRs, so whatever carries the generation there has
// to advance when the desired state is synced, not only at Broker creation.
func (s *BrokerControllerSuite) TestV2NodePoolDeployedGenerationAdvances() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = "true"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(1)

	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pool-" + testenv.RandString(6),
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalNodePoolSpec(rp),
	}
	pool.Spec.Image.Repository = ptr.To(os.Getenv("TEST_REDPANDA_REPO"))
	pool.Spec.Replicas = ptr.To(int32(1))
	require.NoError(t, c.Create(ctx, pool))

	s.applyAndWait(t, ctx, c, rp)

	// Baseline: the pool converges and reports its creation generation.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(pool), pool)) {
			return
		}
		assert.Equal(ct, int32(1), pool.Status.Replicas)
		assert.Equal(ct, pool.Generation, pool.Status.DeployedGeneration)
	}, 5*time.Minute, 5*time.Second)

	// A metadata-only spec change: bumps the generation; the Broker
	// controller syncs it to the pod in place, no rotation involved.
	patch := client.MergeFrom(pool.DeepCopy())
	pool.Spec.PodTemplate = &redpandav1alpha2.PodTemplate{
		Annotations: map[string]string{"test.redpanda.com/deployed-generation-bump": "yes"},
	}
	require.NoError(t, c.Patch(ctx, pool, patch))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pool), pool))
	bumped := pool.Generation
	require.Greater(t, bumped, int64(1), "the spec patch must bump the generation")

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(pool), pool)) {
			return
		}
		assert.Equal(ct, bumped, pool.Status.DeployedGeneration,
			"DeployedGeneration must advance once the new generation's desired state is synced")
	}, 3*time.Minute, 5*time.Second)
}

func (s *BrokerControllerSuite) brokerPodUIDs(t testing.TB, ctx context.Context, c client.Client, rp *redpandav1alpha2.Redpanda, replicas int) map[string]types.UID {
	t.Helper()

	uids := map[string]types.UID{}
	for i := range replicas {
		name := fmt.Sprintf("%s-%d", rp.Name, i)
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod))
		uids[name] = pod.UID
	}
	return uids
}

func (s *BrokerControllerSuite) setMigrationAnnotation(t testing.TB, ctx context.Context, c client.Client, rp *redpandav1alpha2.Redpanda, value *string) {
	t.Helper()

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	patch := client.MergeFrom(rp.DeepCopy())
	if value == nil {
		delete(rp.Annotations, redpandav1alpha2.AnnotationUseBrokerCR)
	} else {
		if rp.Annotations == nil {
			rp.Annotations = map[string]string{}
		}
		rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = *value
	}
	require.NoError(t, c.Patch(ctx, rp, patch))
}

func (s *BrokerControllerSuite) listBrokers(t require.TestingT, ctx context.Context, c client.Client, rp *redpandav1alpha2.Redpanda) []redpandav1alpha2.Broker {
	var list redpandav1alpha2.BrokerList
	require.NoError(t, c.List(ctx, &list, client.InNamespace(rp.Namespace)))

	var owned []redpandav1alpha2.Broker
	for _, b := range list.Items {
		if metav1.IsControlledBy(&b, rp) {
			owned = append(owned, b)
		}
	}
	return owned
}

func (s *BrokerControllerSuite) waitForMigrationCondition(t testing.TB, ctx context.Context, c client.Client, rp *redpandav1alpha2.Redpanda, reason string) {
	t.Helper()

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(rp), rp)) {
			return
		}
		cond := apimeta.FindStatusCondition(rp.Status.Conditions, redpandav1alpha2.BrokerMigrationConditionType)
		if !assert.NotNil(ct, cond, "BrokerMigration condition should exist") {
			return
		}
		assert.Equal(ct, reason, cond.Reason)
	}, 5*time.Minute, 5*time.Second)
}
