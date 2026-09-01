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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
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
	// mgr is the shared testenv's multicluster manager; tests that need a
	// reconciler with DIFFERENT flags than the suite's (e.g. the flag-off
	// downgrade scenario) construct one around it and drive Reconcile by hand.
	mgr multicluster.Manager
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
	s.env, s.clientFactory, s.mgr = s.newEnv(t, "")
}

// newEnv builds a testenv (shared k3d cluster when clusterName is empty, a
// dedicated one otherwise) with the Broker/Redpanda/NodePool controllers and
// RBAC set up. Tests that disrupt cluster infrastructure (node deletion) use
// a dedicated cluster so concurrently-running test PACKAGES sharing the
// default cluster don't lose pods and node-pinned PVCs.
func (s *BrokerControllerSuite) newEnv(t *testing.T, clusterName string) (*testenv.Env, internalclient.ClientFactory, multicluster.Manager) {
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
	var manager multicluster.Manager

	env.SetupManager(s.setupRBAC(ctx, env), func(mgr multicluster.Manager) error {
		manager = mgr
		dialer := kube.NewPodDialer(mgr.GetLocalManager().GetConfig())
		clientFactory = internalclient.NewFactory(mgr, nil).WithDialer(dialer.DialContext)

		require.NoError(t, (&redpanda.NodePoolReconciler{
			Manager: mgr,
			// Matches the production wiring (run.go): with --enable-broker,
			// NodePool status is derived from Broker CRs for pools that have
			// no StatefulSet.
			BrokerCREnabled: true,
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
			// Inert for the manually-choreographed tests: their clusters are
			// paused before Broker CRs are hand-built.
			BrokerCREnabled: true,
		}).SetupWithManager(ctx, mgr, ""))

		return redpanda.SetupBrokerController(ctx, mgr, clientFactory, "", 60*time.Second)
	})

	return env, clientFactory, manager
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
	target.SetRollGrant(newTemplateHash, time.Now().Add(10*time.Minute))
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

// TestDiskLost verifies the dead-node detection path against a real cluster:
// delete a k3d node under a broker, force-delete the stranded pod, and assert
// the Broker controller reports Stuck first (detection timeout not elapsed),
// then marks the Broker DiskLost — with no roll-grant involved — dismantles
// its pod and PVCs, releases the network index, and keeps the dead identity
// immutable. The real scheduler's volume-affinity message and the real Node
// deletion are the point: unit tests fake both. What follows the release —
// the owning engine creating a replacement at the index and decommissioning
// the dead id through the tombstone — is engine logic covered by unit tests
// (brokerset) and deliberately not replayed here: this suite runs no engine,
// and hand-simulating it made the test hostage to k3d's flaky image imports
// onto freshly added nodes.
func (s *BrokerControllerSuite) TestDiskLost() {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 20*time.Minute)
	defer cancel()

	// Dedicated k3d cluster: this test deletes a node. Serializing within
	// this suite is not enough — test PACKAGES run concurrently and other
	// packages' testenvs share the default cluster; their pods and
	// node-pinned PVCs would be stranded by the deletion.
	env, _, _ := s.newEnv(t, "broker-pv-"+strings.ToLower(testenv.RandString(4)))
	ns := env.CreateTestNamespace(t)
	c := ns.Client

	// No grants anywhere: disk-loss handling is deliberately grant-free —
	// marking is a status write and the only destructive act against the
	// cluster rides the decommission.
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
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	require.NotNil(t, target.Status.BrokerID, "target broker must be registered")
	deadID := *target.Status.BrokerID
	t.Logf("target pod %q (node_id %d) is on node %q", targetPod.Name, deadID, targetNode)

	// Record the original PVC names: dismantle must delete them.
	var originalPVCNames []string
	for _, vol := range targetPod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			originalPVCNames = append(originalPVCNames, vol.PersistentVolumeClaim.ClaimName)
		}
	}
	require.NotEmpty(t, originalPVCNames, "target pod must have PVCs")

	// Delete the k3d node. No restoration cleanup is needed: the dedicated
	// cluster is torn down with the env at test end.
	t.Logf("deleting k3d node %q", targetNode)
	require.NoError(t, env.Host().DeleteNode(targetNode))

	// Delete the Kubernetes Node object so the dead-node proof holds.
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

	// The recreated pod cannot schedule (PV pinned to the dead node): Stuck
	// first — the detection timeout (60s in this suite) has not elapsed.
	t.Log("waiting for Broker to report Stuck")
	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseStuck)

	// After the timeout the dead-node proof marks the broker DiskLost and
	// the dismantle releases the network index: pod and PVCs confirmed gone.
	t.Log("waiting for DiskLost marking and dismantle")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(target), target)) {
			return
		}
		assert.Equal(ct, redpandav1alpha2.BrokerPhaseDiskLost, target.Status.Phase)
		assert.True(ct, target.DiskLostReleased(), "pod and PVCs must be confirmed gone")
	}, 5*time.Minute, 2*time.Second)

	// Identity is immutable: the tombstone keeps its node_id, holds no
	// roll-grant, and its resources are gone for good (pod-ensure disabled).
	require.NotNil(t, target.Status.BrokerID)
	assert.Equal(t, deadID, *target.Status.BrokerID, "a dead incarnation never changes identity")
	assert.Empty(t, target.Annotations["operator.redpanda.com/roll-grant"], "disk-loss handling is grant-free")
	var gone corev1.Pod
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &gone)))
	for _, name := range originalPVCNames {
		var pvc corev1.PersistentVolumeClaim
		assert.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: name, Namespace: target.Namespace}, &pvc)),
			"dismantle must delete PVC %q", name)
	}
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
			b.SetRollGrant(templateHash, deadline)
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

// TestOrphanedPodAdoptionIsEventDriven checks whether the broker controller
// reconciles on ownerless pods that can be potentially adopted.
func (s *BrokerControllerSuite) TestOrphanedPodAdoptionIsEventDriven() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{})
	target := brokers[0]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))

	// A live foreign owner for the pod. A ConfigMap suffices as a controller
	// reference and, unlike a synthetic StatefulSet UID, it exists — so the
	// garbage collector has no reason to delete the pod mid-test.
	fakeOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "fake-group-controller", Namespace: target.Namespace},
	}
	require.NoError(t, c.Create(ctx, fakeOwner))

	// Re-enter shadow mode: hand the pod to the foreign controller. This
	// update event still reaches the Broker (the OLD object carried its
	// ownerRef), which is exactly how it learns to stand down.
	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod))
	p := client.MergeFrom(pod.DeepCopy())
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         "v1",
		Kind:               "ConfigMap",
		Name:               fakeOwner.Name,
		UID:                fakeOwner.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}
	require.NoError(t, c.Patch(ctx, &pod, p))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var b redpandav1alpha2.Broker
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(target), &b)) {
			return
		}
		assert.Equal(ct, redpandav1alpha2.BrokerPhasePending, b.Status.Phase,
			"foreign-owned pod should park the Broker in shadow mode")
	}, time.Minute, time.Second)

	// The pod becomes ownerless (what GC does after the StatefulSet's
	// orphan-delete).
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &pod))
	p = client.MergeFrom(pod.DeepCopy())
	pod.OwnerReferences = nil
	require.NoError(t, c.Patch(ctx, &pod, p))

	// Adoption must be event-driven: well under the 3-minute periodic
	// requeue that a missed event would fall back to.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var adopted corev1.Pod
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &adopted)) {
			return
		}
		owner := metav1.GetControllerOf(&adopted)
		if !assert.NotNil(ct, owner, "pod should have been adopted") {
			return
		}
		assert.Equal(ct, "Broker", owner.Kind)
		assert.Equal(ct, target.Name, owner.Name)
	}, time.Minute, time.Second,
		"orphaned pod was not adopted within a minute — adoption is waiting for the periodic requeue instead of the orphaning event")
}

// TestV2MigrationAndRollback exercises the annotation-driven StatefulSet →
// Broker CR migration end to end: adoption in place, scaling without a
// StatefulSet, and rollback — all without pod recreation.
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

	// Adoption copied the live checksums, so nothing may be pending a
	// rotation and no roll grant may be outstanding.
	generations := map[string]int64{}
	for _, b := range s.listBrokers(t, ctx, c, rp) {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: b.PodName(), Namespace: rp.Namespace}, &pod))
		require.False(t, b.PodOutdated(&pod), "broker %q must not be pending a rotation after adoption", b.Name)
		require.False(t, b.HasRollGrant(), "broker %q must not hold a roll grant after adoption", b.Name)
		generations[b.Name] = b.Generation
	}

	// A desired render that doesn't compare equal to the stored
	// (API-defaulted) object would generation-bump every Broker CR on every
	// pass — force a reconcile and verify specs don't churn.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	poke := client.MergeFrom(rp.DeepCopy())
	rp.Annotations["test.redpanda.com/quiescence-poke"] = "1"
	require.NoError(t, c.Patch(ctx, rp, poke))
	require.Never(t, func() bool {
		for _, b := range s.listBrokers(t, ctx, c, rp) {
			if b.Generation != generations[b.Name] {
				t.Logf("broker %q generation %d -> %d", b.Name, generations[b.Name], b.Generation)
				return true
			}
		}
		return false
	}, 20*time.Second, 2*time.Second, "Broker specs churned across reconciles on a converged cluster")

	// Scale up without a StatefulSet.
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

	// Scale down: the excess broker decommissions and its CR is deleted.
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

	// Rollback.
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

// TestV2BrokerModeFlagOffOperatorDoesNotCreateStatefulSet pins downgrade
// safety: a reconciler without --enable-broker is blind to broker-backed
// pools, and unguarded it would render a fresh StatefulSet that fights the
// Broker controller over the pods.
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

	// Identical wiring, --enable-broker off.
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

// TestV2RollbackAdoptsBrokerCreatedPodsWithoutRoll: pods the Broker
// controller created carry no controller-revision-hash, so rollback must
// stamp them with the restored StatefulSet's revision — otherwise the
// revision-based roll loop restarts the fleet right after rollback.
func (s *BrokerControllerSuite) TestV2RollbackAdoptsBrokerCreatedPodsWithoutRoll() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(2)
	s.applyAndWait(t, ctx, c, rp)

	// On failure, dump the revision bookkeeping before the namespace goes
	// away — every spurious-roll hypothesis lives or dies on it.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		dumpCtx, dumpCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dumpCancel()
		var revs appsv1.ControllerRevisionList
		if err := c.List(dumpCtx, &revs, client.InNamespace(rp.Namespace)); err == nil {
			for _, r := range revs.Items {
				t.Logf("POSTMORTEM: controllerrevision %s revision=%d owner=%v", r.Name, r.Revision, metav1.GetControllerOf(&r))
			}
		}
		var sts appsv1.StatefulSet
		if err := c.Get(dumpCtx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts); err == nil {
			t.Logf("POSTMORTEM: sts currentRevision=%s updateRevision=%s generation=%d observedGeneration=%d",
				sts.Status.CurrentRevision, sts.Status.UpdateRevision, sts.Generation, sts.Status.ObservedGeneration)
		}
		var pods corev1.PodList
		if err := c.List(dumpCtx, &pods, client.InNamespace(rp.Namespace)); err == nil {
			for _, p := range pods.Items {
				t.Logf("POSTMORTEM: pod %s uid=%s revision-label=%q", p.Name, p.UID, p.Labels[appsv1.StatefulSetRevisionLabel])
			}
		}
	})

	// Migrate.
	s.setMigrationAnnotation(t, ctx, c, rp, ptr.To("true"))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
		assert.True(ct, apierrors.IsNotFound(err), "StatefulSet should be orphan-deleted")
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 2) {
			return
		}
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase)
		}
	}, 5*time.Minute, 5*time.Second)

	// Replace one pod in broker mode: index 1's pod becomes
	// Broker-controller-created, with no controller-revision-hash.
	var condemned *redpandav1alpha2.Broker
	for _, b := range s.listBrokers(t, ctx, c, rp) {
		if ptr.Deref(b.Spec.NetworkIndex, -1) == 1 {
			condemned = &b
			break
		}
	}
	require.NotNil(t, condemned, "no Broker at index 1")
	patch := client.MergeFrom(condemned.DeepCopy())
	condemned.Spec.Decommission = true
	require.NoError(t, c.Patch(ctx, condemned, patch))

	// This UID is the discriminating signal: the buggy behavior ends in the
	// same world state except the roll loop got there by deleting this pod
	// and letting the StatefulSet recreate it.
	var replacedPodUID types.UID
	replacedPodName := fmt.Sprintf("%s-1", rp.Name)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 2) {
			return
		}
		for _, b := range brokers {
			if !assert.False(ct, b.Spec.Decommission, "the condemned Broker should have been replaced") {
				return
			}
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase)
		}
		var pod corev1.Pod
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: replacedPodName, Namespace: rp.Namespace}, &pod)) {
			return
		}
		if !assert.NotContains(ct, pod.Labels, appsv1.StatefulSetRevisionLabel,
			"precondition: a Broker-created pod carries no controller-revision-hash") {
			return
		}
		replacedPodUID = pod.UID
	}, 10*time.Minute, 5*time.Second)
	require.NotEmpty(t, replacedPodUID)

	// Roll back.
	s.setMigrationAnnotation(t, ctx, c, rp, nil)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		assert.Empty(ct, brokers, "all Broker CRs should be removed on rollback")
		var sts appsv1.StatefulSet
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)) {
			return
		}
		assert.Equal(ct, int32(2), sts.Status.ReadyReplicas, "restored StatefulSet should re-adopt all pods")
	}, 5*time.Minute, 5*time.Second)

	// Same pod (UID from before rollback), stamped with the restored
	// StatefulSet's revision — a recreated pod would also end up labeled, so
	// the UID equality is what separates adoption in place from a restart.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)) {
			return
		}
		var pod corev1.Pod
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: replacedPodName, Namespace: rp.Namespace}, &pod)) {
			return
		}
		if !assert.Equal(ct, replacedPodUID, pod.UID,
			"the Broker-created pod was recreated during/after rollback — the roll loop rolled an adopted pod") {
			return
		}
		assert.NotEmpty(ct, sts.Status.UpdateRevision)
		assert.Equal(ct, sts.Status.UpdateRevision, pod.Labels[appsv1.StatefulSetRevisionLabel],
			"rollback must stamp the restored StatefulSet's revision onto Broker-created pods")
	}, 2*time.Minute, 5*time.Second)

	// Nothing changed in the desired state, so no pod may be restarted after
	// rollback; on a violation, dump the state that explains it before
	// failing.
	require.Never(t, func() bool {
		var pod corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: replacedPodName, Namespace: rp.Namespace}, &pod))
		violation := pod.UID != replacedPodUID || !pod.DeletionTimestamp.IsZero()
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.RestartCount != 0 {
				t.Logf("VIOLATION: container %q restarted %d time(s)", cs.Name, cs.RestartCount)
				violation = true
			}
		}
		if !violation {
			return false
		}
		t.Logf("VIOLATION: pod %s uid=%s (want %s) deletionTimestamp=%v", pod.Name, pod.UID, replacedPodUID, pod.DeletionTimestamp)
		var sts appsv1.StatefulSet
		if err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts); err == nil {
			t.Logf("VIOLATION state: sts currentRevision=%s updateRevision=%s", sts.Status.CurrentRevision, sts.Status.UpdateRevision)
		}
		var revs appsv1.ControllerRevisionList
		if err := c.List(ctx, &revs, client.InNamespace(rp.Namespace)); err == nil {
			for _, r := range revs.Items {
				t.Logf("VIOLATION state: controllerrevision %s revision=%d", r.Name, r.Revision)
			}
		}
		var pods corev1.PodList
		if err := c.List(ctx, &pods, client.InNamespace(rp.Namespace)); err == nil {
			for _, p := range pods.Items {
				t.Logf("VIOLATION state: pod %s uid=%s deletionTimestamp=%v revision-label=%q",
					p.Name, p.UID, p.DeletionTimestamp, p.Labels[appsv1.StatefulSetRevisionLabel])
			}
		}
		return true
	}, 45*time.Second, 2*time.Second,
		"the Broker-created pod was restarted, recreated, or deleted after rollback")
}

// TestV2BrokerBornRollbackKeepsPodsRollable covers a broker-born cluster
// (created with the annotation already set): it must provision Broker CRs
// with VolumeClaimTemplates and never a StatefulSet, and its rollback — the
// path with no migration backup — must give the rendered StatefulSet's
// adopted pods revision bookkeeping, or the roll planner skips them forever
// and future template changes silently never apply.
func (s *BrokerControllerSuite) TestV2BrokerBornRollbackKeepsPodsRollable() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[redpandav1alpha2.AnnotationUseBrokerCR] = "true"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWait(t, ctx, c, rp)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		err := c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)
		assert.True(ct, apierrors.IsNotFound(err), "a fresh broker-mode cluster must never create a StatefulSet")
		brokers := s.listBrokers(ct, ctx, c, rp)
		if !assert.Len(ct, brokers, 3) {
			return
		}
		for _, b := range brokers {
			assert.Equal(ct, redpandav1alpha2.BrokerPhaseRunning, b.Status.Phase)
			assert.NotEmpty(ct, b.Spec.Storage.VolumeClaimTemplates, "fresh brokers use volume claim templates, not existing claims")
		}
	}, 5*time.Minute, 5*time.Second)

	// Roll back the broker-born cluster.
	s.setMigrationAnnotation(t, ctx, c, rp, nil)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		brokers := s.listBrokers(ct, ctx, c, rp)
		assert.Empty(ct, brokers, "all Broker CRs should be removed on rollback")
		var sts appsv1.StatefulSet
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)) {
			return
		}
		assert.Equal(ct, int32(3), sts.Status.ReadyReplicas, "rendered StatefulSet should adopt all pods")
	}, 5*time.Minute, 5*time.Second)

	// Adopted pods must carry the StatefulSet's revision.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var sts appsv1.StatefulSet
		if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &sts)) {
			return
		}
		if !assert.NotEmpty(ct, sts.Status.UpdateRevision) {
			return
		}
		for i := 0; i < 3; i++ {
			var pod corev1.Pod
			if !assert.NoError(ct, c.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%d", rp.Name, i), Namespace: rp.Namespace}, &pod)) {
				return
			}
			assert.Equalf(ct, sts.Status.UpdateRevision, pod.Labels[appsv1.StatefulSetRevisionLabel],
				"pod %q must be adopted carrying the StatefulSet's revision — an unlabeled adopted pod is invisible to every future roll", pod.Name)
		}
	}, 3*time.Minute, 5*time.Second)

	// A template change after the rollback must actually roll the pods.
	uids := s.brokerPodUIDs(t, ctx, c, rp, 3)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(rp), rp))
	patch := client.MergeFrom(rp.DeepCopy())
	rp.Spec.ClusterSpec.Config = &redpandav1alpha2.Config{
		Node: &runtime.RawExtension{Raw: []byte(`{"crash_loop_limit": 7}`)},
	}
	require.NoError(t, c.Patch(ctx, rp, patch))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		rolled := 0
		for name, uid := range uids {
			var pod corev1.Pod
			if err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: rp.Namespace}, &pod); err != nil {
				return
			}
			if pod.UID != uid {
				rolled++
			}
		}
		assert.Equalf(ct, 3, rolled,
			"a post-rollback config change must roll every adopted pod; %d of 3 rolled — unrolled pods are running stale config silently", rolled)
	}, 8*time.Minute, 5*time.Second)
}

// TestV2NodePoolBrokers covers the V2 + NodePools ownership path:
// Brokers of NodePool pools point their clusterRef at the
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
