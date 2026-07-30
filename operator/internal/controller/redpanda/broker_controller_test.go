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
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestIntegrationBrokerController(t *testing.T) {
	testutil.SkipIfNotIntegration(t)
	suite.Run(t, new(BrokerControllerSuite))
}

type BrokerControllerSuite struct {
	suite.Suite

	env           *testenv.Env
	clientFactory internalclient.ClientFactory
	// importImages is the image set newEnv loads into every cluster it
	// builds; tests that add k3d nodes mid-run re-import it onto them.
	importImages []string
}

var _ suite.SetupAllSuite = (*BrokerControllerSuite)(nil)

func (s *BrokerControllerSuite) setup() (*testing.T, context.Context, context.CancelFunc, client.Client) {
	t := s.T()
	t.Parallel()
	return s.setupNamespace(t)
}

func (s *BrokerControllerSuite) setupNamespace(t *testing.T) (*testing.T, context.Context, context.CancelFunc, client.Client) {
	ctx, cancel := context.WithTimeout(trace.Test(t), 15*time.Minute)
	ns := s.env.CreateTestNamespace(t)
	return t, ctx, cancel, ns.Client
}

func (s *BrokerControllerSuite) SetupSuite() {
	t := s.T()
	s.env, s.clientFactory = s.newEnv(t, "")
}

// newEnv builds a testenv (shared k3d cluster when clusterName is empty, a
// dedicated one otherwise) with the Broker/Redpanda/NodePool controllers and
// RBAC set up. Tests that disrupt cluster infrastructure (node deletion) use
// a dedicated cluster so concurrently-running test PACKAGES sharing the
// default cluster don't lose pods and node-pinned PVCs.
func (s *BrokerControllerSuite) newEnv(t *testing.T, clusterName string) (*testenv.Env, internalclient.ClientFactory) {
	ctx := trace.Test(t)

	importImages := []string{
		"localhost/redpanda-operator:dev",
		"ghcr.io/loft-sh/vcluster-pro:0.35.1",
		"registry.k8s.io/kube-controller-manager:v1.33.12",
		"registry.k8s.io/kube-apiserver:v1.33.12",
		"quay.io/jetstack/cert-manager-controller:v1.17.2",
		"quay.io/jetstack/cert-manager-cainjector:v1.17.2",
		"quay.io/jetstack/cert-manager-webhook:v1.17.2",
		"coredns/coredns:1.11.1",
	}
	if repo := os.Getenv("TEST_REDPANDA_REPO"); repo != "" {
		if version := os.Getenv("TEST_REDPANDA_VERSION"); version != "" {
			importImages = append(importImages, fmt.Sprintf("%s:%s", repo, version))
		}
	}

	s.importImages = importImages
	env := testenv.New(t, testenv.Options{
		Name:               clusterName,
		Scheme:             controller.V2Scheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(ctx),
		SkipVCluster:       true,
		WatchAllNamespaces: true,
		ImportImages:       importImages,
	})

	var clientFactory internalclient.ClientFactory

	env.SetupManager(s.setupRBAC(ctx, env), func(mgr multicluster.Manager) error {
		dialer := kube.NewPodDialer(mgr.GetLocalManager().GetConfig())
		clientFactory = internalclient.NewFactory(mgr, nil).WithDialer(dialer.DialContext)

		require.NoError(t, (&redpanda.NodePoolReconciler{
			Manager: mgr,
		}).SetupWithManager(ctx, mgr, ""))

		require.NoError(t, (&redpanda.RedpandaReconciler{
			Manager:       mgr,
			ClientFactory: clientFactory,
			LifecycleClient: lifecycle.NewResourceClient(mgr, lifecycle.V2ResourceManagers(
				lifecycle.Image{Repository: os.Getenv("TEST_REDPANDA_REPO"), Tag: os.Getenv("TEST_REDPANDA_VERSION")},
				lifecycle.Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
				lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false},
			)),
			UseNodePools: true,
		}).SetupWithManager(ctx, mgr, ""))

		return redpanda.SetupBrokerController(ctx, mgr, clientFactory, "", 60*time.Second)
	})

	return env, clientFactory
}

func (s *BrokerControllerSuite) setupRBAC(ctx context.Context, env *testenv.Env) string {
	t := s.T()
	c := env.Client()

	roles, err := kube.DecodeYAML(operatorRBAC, c.Scheme())
	require.NoError(t, err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	clusterRole.Rules = append(clusterRole.Rules, role.Rules...)
	clusterRole.Rules = append(clusterRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods/portforward"},
		Verbs:     []string{"*"},
	}, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	})

	name := "testenv-" + testenv.RandString(6)
	clusterRole.Name = name

	s.applyAndWait(t, ctx, c, clusterRole)
	s.applyAndWait(t, ctx, c,
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: env.Namespace(), Name: name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.Name,
			},
		},
	)

	return name
}

// TestFinalizerPodGone verifies that deleting a Broker CR whose pod doesn't
// exist removes the finalizer and lets the CR be garbage-collected.
func (s *BrokerControllerSuite) TestFinalizerPodGone() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	broker := s.minimalBroker("no-such-cluster")
	// Make pod creation impossible (invalid container name): pod-ensure is
	// unconditional, so an ordinary Broker would have a pod by the time the
	// deletion reconcile runs and the pod-gone branch would never be hit.
	broker.Spec.PodTemplate.Spec.Containers[0].Name = "Invalid_Container_Name"
	require.NoError(t, c.Create(ctx, broker))

	s.waitForFinalizer(t, ctx, c, broker)

	require.NoError(t, c.Delete(ctx, broker))

	s.waitForDeletion(t, ctx, c, broker)

	var pod corev1.Pod
	err := c.Get(ctx, client.ObjectKey{Name: broker.PodName(), Namespace: broker.Namespace}, &pod)
	assert.True(t, apierrors.IsNotFound(err), "no pod should ever have existed for the deleted Broker, got err=%v", err)
}

// TestFinalizerPodNotOwned verifies the rollback case: if the pod exists but is
// not owned by the Broker CR, the finalizer is removed without decommissioning,
// and the pod survives.
func (s *BrokerControllerSuite) TestFinalizerPodNotOwned() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	// The Broker's PodName() returns "<clusterRef.Name>-<networkIndex>".
	clusterName := "rollback-" + testenv.RandString(4)
	podName := clusterName + "-0"

	// The pod carries a FOREIGN controller ownerRef: an ownerless pod would
	// be adopted by the reconciler before the deletion below, silently
	// turning this into a release-path test instead of the rollback branch.
	// The owner must be a REAL object — the GC deletes orphans of
	// non-existent owners, which would race the whole test.
	foreignOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-owner"},
	}
	require.NoError(t, c.Create(ctx, foreignOwner))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       foreignOwner.Name,
				UID:        foreignOwner.UID,
				Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:    "redpanda",
				Image:   "busybox",
				Command: []string{"sleep", "3600"},
			}},
		},
	}
	require.NoError(t, c.Create(ctx, pod))

	broker := s.minimalBroker(clusterName)
	require.NoError(t, c.Create(ctx, broker))

	s.waitForFinalizer(t, ctx, c, broker)

	require.NoError(t, c.Delete(ctx, broker))

	s.waitForDeletion(t, ctx, c, broker)

	// Pod must still exist.
	var surviving corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &surviving))
	require.Equal(t, podName, surviving.Name)
}

// TestFinalizerDecommission verifies the decommission-on-delete path: with
// spec.decommission set, deleting the Broker CR decommissions the broker,
// deletes its pod, and removes the finalizer.
func (s *BrokerControllerSuite) TestFinalizerDecommission() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		grantDuration: 10 * time.Minute,
	})

	admin, err := s.clientFactory.RedpandaAdminClient(ctx, brokers[0])
	require.NoError(t, err)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 3)
	}, 2*time.Minute, 5*time.Second)

	// Mark broker-2 for decommission, then delete its CR — deletion with
	// intent decommissions before cleanup (RFC Q2).
	target := brokers[2]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, target, p))
	require.NoError(t, c.Delete(ctx, target))

	// The Broker CR should be fully deleted (finalizer removed after decommission).
	s.waitForDeletion(t, ctx, c, target)

	// The pod should be gone (may take time for kubelet to terminate it).
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var deletedPod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-2", rp.Name), Namespace: rp.Namespace}, &deletedPod)
		assert.True(ct, apierrors.IsNotFound(err), "expected pod to be deleted, got: %v", err)
	}, 2*time.Minute, 5*time.Second)

	// Admin API should show 2 brokers.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 2)
	}, 5*time.Minute, 5*time.Second)

	admin.Close()
}

// TestDecommissionIntentRemovedRevivesSlot verifies the recommission
// contract: clearing spec.decommission after the decommission completed
// revives the broker slot — the pod is recreated (ungated pod-ensure) and the
// broker rejoins the cluster.
func (s *BrokerControllerSuite) TestDecommissionIntentRemovedRevivesSlot() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		grantDuration: 10 * time.Minute,
		// Revival recreates PVCs from templates; ExistingClaims are
		// adopt-only and were deleted by the completed decommission.
		useVolumeClaimTemplates: true,
	})

	admin, err := s.clientFactory.RedpandaAdminClient(ctx, brokers[0])
	require.NoError(t, err)
	defer admin.Close()

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 3)
	}, 2*time.Minute, 5*time.Second)

	// Decommission broker-2 to completion.
	target := brokers[2]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, target, p))
	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseDecommissioned)

	// Clear the intent: the slot revives with a fresh identity.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p = client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = false
	require.NoError(t, c.Patch(ctx, target, p))

	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseRunning)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 3)
	}, 5*time.Minute, 5*time.Second)
}

// TestFinalizerRawDeletionReleases verifies RFC Q2: deleting a Broker CR
// WITHOUT spec.decommission never decommissions — the pod and PVCs are
// released (ownerRefs stripped) and the broker keeps serving.
// TestClusterTeardownSkipsDecommission covers deleting the owning cluster
// while a Broker carries in-flight decommission intent: the finalizer must
// not insist on completing the decommission — the owner is gone, so admin
// resolution fails (and the sibling brokers are dying under it) — or the CR,
// pod, and PVCs leak in Terminating and namespace deletion hangs forever.
// Teardown applies the deletion policy directly instead.
func (s *BrokerControllerSuite) TestClusterTeardownSkipsDecommission() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{})

	// setupBrokerCluster hand-builds unowned Brokers; teardown semantics
	// require the real ownership wiring (controller ownerRef → GC cascade).
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	for _, b := range brokers {
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(b), b))
		patch := client.MergeFrom(b.DeepCopy())
		require.NoError(t, controllerutil.SetControllerReference(rp, b, c.Scheme()))
		require.NoError(t, c.Patch(ctx, b, patch))
	}

	// Mark a decommission and immediately tear the owner down: on the next
	// finalizer pass the owner is gone, so completing the decommission is
	// impossible — it must be skipped, not waited for.
	target := brokers[len(brokers)-1]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	patch := client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, target, patch))

	require.NoError(t, c.Delete(ctx, rp))

	// Every Broker CR — including the decommissioning one — must finalize
	// and cascade its pod away.
	for _, b := range brokers {
		s.waitForDeletion(t, ctx, c, b)
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		for i := range brokers {
			var pod corev1.Pod
			err := c.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%d", rp.Name, i), Namespace: rp.Namespace}, &pod)
			if !apierrors.IsNotFound(err) && pod.DeletionTimestamp.IsZero() {
				assert.Fail(ct, "pod still alive after cascade teardown", "pod %s-%d", rp.Name, i)
			}
		}
	}, 5*time.Minute, 5*time.Second)
}

func (s *BrokerControllerSuite) TestFinalizerRawDeletionReleases() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		grantDuration: 10 * time.Minute,
	})

	admin, err := s.clientFactory.RedpandaAdminClient(ctx, brokers[0])
	require.NoError(t, err)
	defer admin.Close()

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 3)
	}, 2*time.Minute, 5*time.Second)

	// Raw deletion: no decommission intent.
	target := brokers[2]
	targetPodName := fmt.Sprintf("%s-2", rp.Name)
	require.NoError(t, c.Delete(ctx, target))

	s.waitForDeletion(t, ctx, c, target)

	// The pod survives, released from the deleted CR.
	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: targetPodName, Namespace: rp.Namespace}, &pod))
	assert.Nil(t, metav1.GetControllerOf(&pod), "pod should have been released from the deleted Broker CR")

	// The PVCs survive too, with the CR's ownerRefs stripped — this is the
	// data-preservation half of the release contract.
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		var pvc corev1.PersistentVolumeClaim
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: vol.PersistentVolumeClaim.ClaimName, Namespace: rp.Namespace}, &pvc))
		for _, ref := range pvc.OwnerReferences {
			assert.NotEqual(t, target.UID, ref.UID, "PVC %s still owned by the deleted Broker CR", pvc.Name)
		}
	}

	// Membership is untouched: still 3 brokers. Retried because the admin
	// endpoint we dial may be mid-rotation (the setup grant is still live),
	// which surfaces as transient EOFs or a briefly missing pod.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		if !assert.NoError(ct, err) {
			return
		}
		assert.Len(ct, bs, 3)
	}, 2*time.Minute, 5*time.Second)
}

// TestSpecDecommission verifies the spec-driven decommission path:
// create a real Redpanda cluster, migrate pods to Broker CRs, then set
// spec.decommission=true on one Broker CR and verify it decommissions,
// deletes its pod and PVCs, and reaches Decommissioned phase.
func (s *BrokerControllerSuite) TestSpecDecommission() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		grantDuration: 10 * time.Minute,
	})

	admin, err := s.clientFactory.RedpandaAdminClient(ctx, brokers[0])
	require.NoError(t, err)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 3)
	}, 2*time.Minute, 5*time.Second)

	// Set spec.decommission=true on broker-2.
	target := brokers[2]
	targetPodName := fmt.Sprintf("%s-2", rp.Name)

	// Record PVC names for later.
	var targetPVCNames []string
	for _, ec := range target.Spec.Storage.ExistingClaims {
		targetPVCNames = append(targetPVCNames, ec.Name)
	}
	require.NotEmpty(t, targetPVCNames)

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, target, p))

	// Wait for Decommissioned phase.
	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseDecommissioned)

	// The pod should be deleted.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var deletedPod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: targetPodName, Namespace: rp.Namespace}, &deletedPod)
		assert.True(ct, apierrors.IsNotFound(err), "expected pod to be deleted, got: %v", err)
	}, 2*time.Minute, 5*time.Second)

	// The PVCs should be deleted.
	for _, pvcName := range targetPVCNames {
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: rp.Namespace}, &pvc)
			assert.True(ct, apierrors.IsNotFound(err), "expected PVC %q to be deleted, got: %v", pvcName, err)
		}, 2*time.Minute, 5*time.Second)
	}

	// Admin API should show 2 brokers.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		bs, err := admin.Brokers(ctx)
		assert.NoError(ct, err)
		assert.Len(ct, bs, 2)
	}, 5*time.Minute, 5*time.Second)

	// The Broker CR should still exist (not deleted, just decommissioned).
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	assert.Equal(t, redpandav1alpha2.BrokerPhaseDecommissioned, target.Status.Phase)

	admin.Close()
}

// TestPodCreatedWithoutGrant verifies that pod creation is NOT gated on a
// roll-grant (RFC Q5): pod-ensure is the unconditional first step, only
// disruptive actions (rotation, PV remediation) require a grant.
func (s *BrokerControllerSuite) TestPodCreatedWithoutGrant() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	broker := s.minimalBroker("no-such-cluster")
	require.NoError(t, c.Create(ctx, broker))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var pod corev1.Pod
		assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: broker.PodName(), Namespace: broker.Namespace}, &pod))
	}, 2*time.Minute, time.Second, "pod should be created without a roll-grant")
}

// TestPodRotationWithoutGrant verifies the roll-grant gate NEGATIVELY: an
// outdated pod with NO grant at all must not rotate — the pod's UID and live
// checksum stay put while ConfigSynced=False reports the pending rotation.
// Only after a matching grant is issued may the pod be recreated. Without
// the negative half, a gate that always allowed rotation would be
// observationally identical to a working one (ConfigSynced=False is set as
// soon as drift is detected, BEFORE the rotation completes, so an Eventually
// on it alone can pass mid-rotation).
// TestPodMetadataSyncedWithoutRotation is the regression test for the
// template-propagation gap: a metadata-only template change (a
// Cluster.spec.annotations edit, a label change) must reach the live pod IN
// PLACE — no roll-grant, no drain, no restart. Spec changes remain the
// rotation-worthy class (TestPodRotationWithoutGrant covers that gate).
func (s *BrokerControllerSuite) TestPodMetadataSyncedWithoutRotation() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{})

	target := brokers[0]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))

	// The adopted pod predates the Broker CR: the adoption backfill must
	// have stamped the desired spec hash onto it (treating it as current) —
	// otherwise every migration would queue a pointless roll.
	desiredHash := target.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	require.NotEmpty(t, desiredHash)
	var podBefore corev1.Pod
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &podBefore)) {
			return
		}
		assert.Equal(ct, desiredHash, podBefore.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation],
			"adoption should backfill the spec hash onto the pre-existing pod")
	}, time.Minute, 2*time.Second)

	// A pure-metadata template change: annotation and label move, the spec
	// hash stays put. NO roll-grant is issued.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.PodTemplate.Annotations["test.redpanda.com/propagation"] = "works"
	if target.Spec.PodTemplate.Labels == nil {
		target.Spec.PodTemplate.Labels = map[string]string{}
	}
	target.Spec.PodTemplate.Labels["test.redpanda.com/label"] = "works"
	require.Equal(t, desiredHash, target.Spec.PodTemplate.Hash(),
		"metadata must not move the spec hash")
	require.NoError(t, c.Patch(ctx, target, p))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var pod corev1.Pod
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod)) {
			return
		}
		assert.Equal(ct, "works", pod.Annotations["test.redpanda.com/propagation"])
		assert.Equal(ct, "works", pod.Labels["test.redpanda.com/label"])
		assert.Equal(ct, podBefore.UID, pod.UID, "metadata sync must not recreate the pod")
	}, 2*time.Minute, 2*time.Second, "metadata never reached the live pod")

	// The rotation bookkeeping keys were not disturbed by the sync.
	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod))
	require.Equal(t, podBefore.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation],
		pod.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation])
	require.Equal(t, desiredHash, pod.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation])
}

func (s *BrokerControllerSuite) TestPodRotationWithoutGrant() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{})

	target := brokers[0]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))

	var podBefore corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &podBefore))

	// Change the desired checksum so the pod is outdated; no grant exists.
	// Re-stamp the template hash the way every production template writer
	// does — grants are keyed on it.
	newChecksum := "deliberately-changed-checksum"
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"] = newChecksum
	target.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = target.Spec.PodTemplate.Hash()
	newTemplateHash := target.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	require.NoError(t, c.Patch(ctx, target, p))

	// Drift is reported...
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(target), target))
		assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, target.Status.Phase)
		cond := apimeta.FindStatusCondition(target.Status.Conditions, statuses.BrokerConfigSynced)
		if assert.NotNil(ct, cond, "ConfigSynced condition not found") {
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		}
	}, 2*time.Minute, 5*time.Second)

	// ...but the pod must NOT be rotated: same UID, same live checksum.
	require.Never(t, func() bool {
		var pod corev1.Pod
		if err := c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod); err != nil {
			return true // pod deleted = rotation started
		}
		return pod.UID != podBefore.UID
	}, 30*time.Second, 2*time.Second, "pod was rotated without a roll-grant")

	// Issue a grant matching the new template hash: the rotation may now
	// proceed.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p = client.MergeFrom(target.DeepCopy())
	if target.Annotations == nil {
		target.Annotations = map[string]string{}
	}
	target.Annotations["operator.redpanda.com/roll-grant"] = feature.FormatRollGrant(newTemplateHash, time.Now().Add(10*time.Minute))
	require.NoError(t, c.Patch(ctx, target, p))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var pod corev1.Pod
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod)) {
			return
		}
		assert.NotEqual(ct, podBefore.UID, pod.UID, "pod should be recreated once granted")
		assert.Equal(ct, newChecksum, pod.Annotations["config.redpanda.com/checksum"])
	}, 4*time.Minute, 5*time.Second, "granted rotation never completed")
}

// TestLastBrokerDecommissionGuard verifies that attempting to decommission the
// only broker in a cluster results in Stuck phase instead of proceeding.
func (s *BrokerControllerSuite) TestLastBrokerDecommissionGuard() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		replicas:      1,
		grantDuration: 10 * time.Minute,
	})

	target := brokers[0]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))

	p := client.MergeFrom(target.DeepCopy())
	target.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, target, p))

	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseStuck)
}

// TestPVAffinityRemediation verifies the dead-node recovery path:
// create a Redpanda cluster, migrate to Broker CRs, delete a k3d node,
// force-delete the stuck pod, and assert the Broker controller reports
// Stuck then — once granted a roll — remediates and recovers.
func (s *BrokerControllerSuite) TestPVAffinityRemediation() {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 20*time.Minute)
	defer cancel()

	// Dedicated k3d cluster: this test deletes a node. Serializing within
	// this suite is not enough — test PACKAGES run concurrently and other
	// packages' testenvs share the default cluster; their pods and
	// node-pinned PVCs would be stranded by the deletion.
	env, _ := s.newEnv(t, "broker-pv-"+strings.ToLower(testenv.RandString(4)))
	ns := env.CreateTestNamespace(t)
	c := ns.Client

	// No setup grant: the Stuck wait below must prove the ungranted broker
	// REFUSES to remediate; the explicit grant later is what unlocks it.
	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{
		useVolumeClaimTemplates: true,
	})

	// Pick a target broker whose pod runs on an AGENT node: k3d server nodes
	// are schedulable, and deleting the server would take down the API
	// server for the entire suite.
	var target *redpandav1alpha2.Broker
	var targetPod corev1.Pod
	for _, b := range brokers {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: b.PodName(), Namespace: b.Namespace}, &pod))
		if pod.Spec.NodeName != "" && !strings.Contains(pod.Spec.NodeName, "server") {
			target = b
			targetPod = pod
			break
		}
	}
	require.NotNil(t, target, "no broker pod scheduled on an agent node")
	targetNode := targetPod.Spec.NodeName
	t.Logf("target pod %q is on node %q", targetPod.Name, targetNode)

	// Record the original PVC names for later comparison.
	var originalPVCNames []string
	for _, vol := range targetPod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			originalPVCNames = append(originalPVCNames, vol.PersistentVolumeClaim.ClaimName)
		}
	}
	require.NotEmpty(t, originalPVCNames, "target pod must have PVCs")

	// Record original PV names bound to these PVCs.
	originalPVNames := map[string]string{} // pvcName -> pvName
	for _, name := range originalPVCNames {
		var pvc corev1.PersistentVolumeClaim
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name, Namespace: target.Namespace}, &pvc))
		require.NotEmpty(t, pvc.Spec.VolumeName, "PVC %q must be bound", name)
		originalPVNames[name] = pvc.Spec.VolumeName
	}

	// Delete the k3d node. No restoration cleanup is needed: the dedicated
	// cluster is torn down with the env at test end.
	t.Logf("deleting k3d node %q", targetNode)
	require.NoError(t, env.Host().DeleteNode(targetNode))

	// Delete the Kubernetes Node object so the Broker controller sees it as gone.
	var nodeObj corev1.Node
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		// The node might take a moment to be reported as not-ready.
		err := c.Get(ctx, client.ObjectKey{Name: targetNode}, &nodeObj)
		if !assert.NoError(ct, err) {
			return
		}
		assert.NoError(ct, c.Delete(ctx, &nodeObj))
	}, 1*time.Minute, 2*time.Second)

	// Force-delete the pod stuck in Terminating (kubelet on dead node can't confirm).
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var pod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: targetPod.Name, Namespace: targetPod.Namespace}, &pod)
		if apierrors.IsNotFound(err) {
			return
		}
		assert.NoError(ct, err)
		assert.NoError(ct, c.Delete(ctx, &pod, client.GracePeriodSeconds(0)))
	}, 2*time.Minute, 2*time.Second)

	// Wait for Broker to report Stuck (pod recreated but can't schedule due to PV affinity).
	t.Log("waiting for Broker to report Stuck")
	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseStuck)

	// Create the replacement k3d node and import the suite's images BEFORE
	// granting: pod recreation is not grant-gated, so the moment
	// remediation deletes the pod its replacement schedules — if the fresh
	// node's image import is still streaming at that point, the kubelet's
	// registry pull of localhost/redpanda-operator:dev fails hard and the
	// resulting ImagePullBackOff (capped at 5m) can outlast the recovery
	// wait. The stuck pod cannot unstick on the new node meanwhile: its PV
	// stays pinned to the deleted node's hostname until remediation, which
	// the grant below gates.
	t.Log("creating replacement k3d node")
	require.NoError(t, env.Host().CreateNode())
	require.NoError(t, env.Host().ImportImage(s.importImages...))

	// Grant a fresh roll-grant for the remediation.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	if target.Annotations == nil {
		target.Annotations = map[string]string{}
	}
	templateHash := target.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
	target.Annotations["operator.redpanda.com/roll-grant"] = feature.FormatRollGrant(templateHash, time.Now().Add(30*time.Minute))
	require.NoError(t, c.Patch(ctx, target, p))

	// Create a replacement k3d node so the pod has somewhere to schedule,
	// and re-import the suite's images: `k3d node create` starts an empty
	// node, and soft anti-affinity prefers it — without the operator image
	// present the sidecar would sit in ImagePullBackOff and the recovery
	// wait below would time out.
	t.Log("creating replacement k3d node")
	require.NoError(t, env.Host().CreateNode())
	require.NoError(t, env.Host().ImportImage(s.importImages...))

	// Wait for the broker to recover to Running. This leg gets a longer
	// budget than the default: it covers replacement-node startup, the image
	// import above racing the pod's first pull (a transient
	// ImagePullBackOff correctly reports Stuck), and a redpanda cold start —
	// which together can exceed 5 minutes on a loaded CI host.
	t.Log("waiting for Broker to recover to Running")
	s.waitForPhaseWithin(t, ctx, c, target, redpandav1alpha2.BrokerPhaseRunning, 12*time.Minute)

	// Verify remediation happened: the old PVs should have Retain policy.
	for pvcName, pvName := range originalPVNames {
		var pv corev1.PersistentVolume
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: pvName}, &pv),
			"original PV %q (from PVC %q) should still exist", pvName, pvcName)
		assert.Equal(t, corev1.PersistentVolumeReclaimRetain, pv.Spec.PersistentVolumeReclaimPolicy,
			"original PV %q should have been patched to Retain", pvName)
	}

	// The target pod should now be on a different node.
	var recoveredPod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &recoveredPod))
	assert.NotEqual(t, targetNode, recoveredPod.Spec.NodeName,
		"recovered pod should be on a different node")
	t.Logf("recovered pod %q is now on node %q (was %q)", recoveredPod.Name, recoveredPod.Spec.NodeName, targetNode)
}

// --- helpers ---

type brokerClusterOpts struct {
	replicas                int
	useVolumeClaimTemplates bool
	grantDuration           time.Duration
}

// setupBrokerCluster creates a Redpanda cluster, orphan-deletes its
// StatefulSet, pauses reconciliation, creates Broker CRs for each pod,
// grants roll-grants, and waits for all brokers to reach Running.
// Defaults to 3 replicas if opts.replicas is 0.
func (s *BrokerControllerSuite) setupBrokerCluster(t *testing.T, ctx context.Context, c client.Client, opts brokerClusterOpts) (*redpandav1alpha2.Redpanda, []*redpandav1alpha2.Broker) {
	replicas := opts.replicas
	if replicas == 0 {
		replicas = 3
	}
	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(replicas)
	s.applyAndWait(t, ctx, c, rp)

	// Pause reconciliation BEFORE orphan-deleting the StatefulSet: the
	// Redpanda controller would otherwise recreate the STS, which then
	// resurrects any pod the Broker controller later deletes (e.g. after a
	// decommission), corrupting the test.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	patch := client.MergeFrom(rp.DeepCopy())
	if rp.Annotations == nil {
		rp.Annotations = map[string]string{}
	}
	rp.Annotations["cluster.redpanda.com/managed"] = "false"
	require.NoError(t, c.Patch(ctx, rp, patch))

	var found appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &found))
	require.NoError(t, c.Delete(ctx, &found, client.PropagationPolicy(metav1.DeletePropagationOrphan)))

	// The orphan-delete leaves no STS behind; if a racing pre-pause
	// reconcile recreated it, fail fast with a clear message.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
		assert.True(ct, apierrors.IsNotFound(err), "StatefulSet still exists (recreated by a racing reconcile?)")
	}, time.Minute, time.Second)

	var brokers []*redpandav1alpha2.Broker
	for i := range replicas {
		podName := fmt.Sprintf("%s-%d", rp.Name, i)
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: podName, Namespace: rp.Namespace}, &pod))

		podSpec := pod.Spec
		podSpec.NodeName = ""

		// Use the checksum the chart stamped on the live pod: the adoption
		// backfill preserves a pod's existing checksum (overwriting would
		// swallow pending rotations), so a synthetic value here would leave
		// every adopted broker permanently PodOutdated — parked at the
		// rotation step waiting for a grant, never reaching the steps behind
		// it (e.g. the in-place metadata sync).
		checksum := pod.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation]
		if checksum == "" {
			specBytes, err := yaml.Marshal(podSpec)
			require.NoError(t, err)
			checksum = fmt.Sprintf("%x", sha256.Sum256(specBytes))
		}

		storage := redpandav1alpha2.BrokerStorage{}
		if opts.useVolumeClaimTemplates {
			for _, vol := range pod.Spec.Volumes {
				if vol.PersistentVolumeClaim == nil {
					continue
				}
				pvcName := vol.PersistentVolumeClaim.ClaimName
				vctName := strings.TrimSuffix(pvcName, "-"+podName)

				var pvc corev1.PersistentVolumeClaim
				require.NoError(t, c.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: rp.Namespace}, &pvc))

				pvcSpec := pvc.Spec
				pvcSpec.VolumeName = ""
				storage.VolumeClaimTemplates = append(storage.VolumeClaimTemplates, redpandav1alpha2.BrokerVolumeClaim{
					Name: vctName,
					Spec: pvcSpec,
				})
			}
		} else {
			for _, vol := range pod.Spec.Volumes {
				if vol.PersistentVolumeClaim != nil {
					storage.ExistingClaims = append(storage.ExistingClaims, redpandav1alpha2.ExistingClaim{
						Name: vol.PersistentVolumeClaim.ClaimName,
					})
				}
			}
		}

		broker := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", rp.Name, i),
				Labels: map[string]string{
					redpandav1alpha2.ClusterNameLabel: rp.Name,
				},
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef:   redpandav1alpha2.ClusterRef{Name: rp.Name},
				NetworkIndex: ptr.To(int32(i)),
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Labels:      pod.Labels,
					Annotations: map[string]string{"config.redpanda.com/checksum": checksum},
					Spec:        podSpec,
				},
				Storage: storage,
			},
		}
		// Stamp the rotation identity the way the cluster controller does;
		// grants are keyed on it. The adopted pods predate the Broker and
		// lack the annotation — the adoption backfill covers them.
		broker.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = broker.Spec.PodTemplate.Hash()
		require.NoError(t, c.Create(ctx, broker))
		brokers = append(brokers, broker)
	}

	// Grants are opt-in: pod creation and adoption are deliberately NOT
	// grant-gated (RFC Q5), so the steady state needs none. Tests that
	// exercise disruptive actions grant explicitly — a blanket grant here
	// would make it impossible to assert that ungranted brokers refuse to
	// rotate or remediate.
	if opts.grantDuration > 0 {
		deadline := time.Now().Add(opts.grantDuration)
		for _, b := range brokers {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(b), b))
			p := client.MergeFrom(b.DeepCopy())
			if b.Annotations == nil {
				b.Annotations = map[string]string{}
			}
			templateHash := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation]
			b.Annotations["operator.redpanda.com/roll-grant"] = feature.FormatRollGrant(templateHash, deadline)
			require.NoError(t, c.Patch(ctx, b, p))
		}
	}

	for _, b := range brokers {
		s.waitForPhase(t, ctx, c, b, redpandav1alpha2.BrokerPhaseRunning)
	}

	return rp, brokers
}

func (s *BrokerControllerSuite) minimalBroker(clusterName string) *redpandav1alpha2.Broker {
	return &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName + "-0",
			Labels: map[string]string{
				redpandav1alpha2.ClusterNameLabel: clusterName,
			},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: clusterName},
			NetworkIndex: ptr.To(int32(0)),
			PodTemplate: redpandav1alpha2.BrokerPodTemplate{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "redpanda",
						Image:   "busybox",
						Command: []string{"sleep", "3600"},
					}},
				},
			},
		},
	}
}

func (s *BrokerControllerSuite) minimalRP() *redpandav1alpha2.Redpanda {
	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rp-" + testenv.RandString(6),
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalRedpandaSpec(),
	}
	rp.Spec.ClusterSpec.Image.Repository = ptr.To(os.Getenv("TEST_REDPANDA_REPO"))
	rp.Spec.ClusterSpec.Image.Tag = ptr.To(os.Getenv("TEST_REDPANDA_VERSION"))
	return rp
}

func (s *BrokerControllerSuite) waitForFinalizer(t testing.TB, ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker) {
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(broker), broker))
		assert.True(ct, controllerutil.ContainsFinalizer(broker, "cluster.redpanda.com/broker-decommission"),
			"broker %q missing finalizer", broker.Name)
	}, 2*time.Minute, time.Second)
}

func (s *BrokerControllerSuite) waitForDeletion(t testing.TB, ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker) {
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, false, func(ctx context.Context) (bool, error) {
		err := c.Get(ctx, client.ObjectKeyFromObject(broker), broker)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		t.Logf("Broker %q still exists (phase=%s, finalizers=%v)", broker.Name, broker.Status.Phase, broker.Finalizers)
		return false, nil
	})
	require.NoError(t, err, "Broker %q was not deleted", broker.Name)
}

func (s *BrokerControllerSuite) waitForPhase(t testing.TB, ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker, phase redpandav1alpha2.BrokerPhase) {
	s.waitForPhaseWithin(t, ctx, c, broker, phase, 5*time.Minute)
}

func (s *BrokerControllerSuite) waitForPhaseWithin(t testing.TB, ctx context.Context, c client.Client, broker *redpandav1alpha2.Broker, phase redpandav1alpha2.BrokerPhase, timeout time.Duration) {
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(broker), broker))
		t.Logf("Broker %q phase=%s (want %s)", broker.Name, broker.Status.Phase, phase)
		assert.Equal(ct, phase, broker.Status.Phase)
	}, timeout, 5*time.Second)
}

func (s *BrokerControllerSuite) applyAndWait(t testing.TB, ctx context.Context, c client.Client, objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := c.GroupVersionKindFor(obj)
		require.NoError(t, err)
		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		require.NoError(t, c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO: migrate to client.Client.Apply()
	}
	for _, obj := range objs {
		switch obj := obj.(type) {
		case *redpandav1alpha2.Redpanda:
			err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, false, func(ctx context.Context) (bool, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
					return false, err
				}
				for _, cond := range obj.Status.Conditions {
					if cond.Type == "Stable" && cond.Status == metav1.ConditionTrue && cond.ObservedGeneration == obj.Generation {
						return true, nil
					}
				}
				t.Logf("waiting for Redpanda %q to stabilize (gen=%d)", obj.Name, obj.Generation)
				return false, nil
			})
			require.NoError(t, err)
		case *corev1.Secret, *corev1.ConfigMap, *corev1.ServiceAccount,
			*rbacv1.ClusterRole, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRoleBinding:
			// no wait needed
		default:
			t.Fatalf("unhandled object %T in applyAndWait", obj)
		}
	}
}
