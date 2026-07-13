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
	"strconv"
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
}

var _ suite.SetupAllSuite = (*BrokerControllerSuite)(nil)

func (s *BrokerControllerSuite) setup() (*testing.T, context.Context, context.CancelFunc, client.Client) {
	t := s.T()
	t.Parallel()
	return s.setupNamespace(t)
}

// setupSerial is setup without t.Parallel, for tests that disrupt shared
// cluster infrastructure (e.g. deleting k3d nodes): running those alongside
// parallel tests strands the other tests' pods and PVCs on the dead node.
func (s *BrokerControllerSuite) setupSerial() (*testing.T, context.Context, context.CancelFunc, client.Client) {
	return s.setupNamespace(s.T())
}

func (s *BrokerControllerSuite) setupNamespace(t *testing.T) (*testing.T, context.Context, context.CancelFunc, client.Client) {
	ctx, cancel := context.WithTimeout(trace.Test(t), 15*time.Minute)
	ns := s.env.CreateTestNamespace(t)
	return t, ctx, cancel, ns.Client
}

func (s *BrokerControllerSuite) SetupSuite() {
	t := s.T()
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

	s.env = testenv.New(t, testenv.Options{
		Scheme:             controller.V2Scheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(ctx),
		SkipVCluster:       true,
		WatchAllNamespaces: true,
		ImportImages:       importImages,
	})

	suiteClient := s.env.Client()

	s.env.SetupManager(s.setupRBAC(ctx, suiteClient), func(mgr multicluster.Manager) error {
		dialer := kube.NewPodDialer(mgr.GetLocalManager().GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr, nil).WithDialer(dialer.DialContext)

		require.NoError(t, (&redpanda.NodePoolReconciler{
			Manager: mgr,
		}).SetupWithManager(ctx, mgr, ""))

		require.NoError(t, (&redpanda.RedpandaReconciler{
			Manager:       mgr,
			ClientFactory: s.clientFactory,
			LifecycleClient: lifecycle.NewResourceClient(mgr, lifecycle.V2ResourceManagers(
				lifecycle.Image{Repository: os.Getenv("TEST_REDPANDA_REPO"), Tag: os.Getenv("TEST_REDPANDA_VERSION")},
				lifecycle.Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
				lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false},
			)),
			UseNodePools: true,
		}).SetupWithManager(ctx, mgr, ""))

		return redpanda.SetupBrokerController(ctx, mgr, s.clientFactory, "", 60*time.Second)
	})
}

func (s *BrokerControllerSuite) setupRBAC(ctx context.Context, c client.Client) string {
	t := s.T()

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
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: name},
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
	require.NoError(t, c.Create(ctx, broker))

	s.waitForFinalizer(t, ctx, c, broker)

	require.NoError(t, c.Delete(ctx, broker))

	s.waitForDeletion(t, ctx, c, broker)
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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
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

	// Membership is untouched: still 3 brokers.
	bs, err := admin.Brokers(ctx)
	require.NoError(t, err)
	assert.Len(t, bs, 3)
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
func (s *BrokerControllerSuite) TestPodRotationWithoutGrant() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	_, brokers := s.setupBrokerCluster(t, ctx, c, brokerClusterOpts{})

	target := brokers[0]
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))

	var podBefore corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: target.PodName(), Namespace: target.Namespace}, &podBefore))

	// Change the desired checksum so the pod is outdated; no grant exists.
	newChecksum := "deliberately-changed-checksum"
	p := client.MergeFrom(target.DeepCopy())
	target.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"] = newChecksum
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

	// Issue a grant matching the new checksum: the rotation may now proceed.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p = client.MergeFrom(target.DeepCopy())
	if target.Annotations == nil {
		target.Annotations = map[string]string{}
	}
	target.Annotations["operator.redpanda.com/roll-grant"] = newChecksum + "/" + strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
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
	// Serial: this test deletes a shared k3d node; parallel neighbors would
	// lose pods and node-pinned PVCs with it.
	t, ctx, cancel, c := s.setupSerial()
	defer cancel()

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

	// Delete the k3d node. Ensure a replacement is always created, even on
	// test failure, so other tests sharing this k3d cluster are not impacted.
	var nodesBefore corev1.NodeList
	require.NoError(t, s.env.Client().List(ctx, &nodesBefore))
	expectedNodeCount := len(nodesBefore.Items)

	t.Logf("deleting k3d node %q", targetNode)
	require.NoError(t, s.env.Host().DeleteNode(targetNode))
	t.Cleanup(func() {
		ctx := context.Background()
		// Remove the stale Node object if the test failed before doing so
		// itself: a dead node lingering in the API would both skew the count
		// below and confuse later tests.
		var staleNode corev1.Node
		if err := s.env.Client().Get(ctx, client.ObjectKey{Name: targetNode}, &staleNode); err == nil {
			if err := s.env.Client().Delete(ctx, &staleNode); err != nil {
				t.Logf("WARNING: failed to delete stale Node object %q during cleanup: %v", targetNode, err)
			}
		}
		var nodes corev1.NodeList
		if err := s.env.Client().List(ctx, &nodes); err != nil {
			t.Logf("WARNING: failed to list nodes during cleanup: %v", err)
			return
		}
		ready := 0
		for _, node := range nodes.Items {
			if node.Name == targetNode {
				continue
			}
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					ready++
					break
				}
			}
		}
		if ready >= expectedNodeCount {
			return
		}
		t.Logf("cluster has %d ready nodes (expected %d), creating replacement", ready, expectedNodeCount)
		if err := s.env.Host().CreateNode(); err != nil {
			t.Logf("WARNING: failed to create replacement k3d node during cleanup: %v", err)
		}
	})

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

	// Grant a fresh roll-grant for the remediation.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(target), target))
	p := client.MergeFrom(target.DeepCopy())
	if target.Annotations == nil {
		target.Annotations = map[string]string{}
	}
	checksum := target.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]
	target.Annotations["operator.redpanda.com/roll-grant"] = checksum + "/" + strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10)
	require.NoError(t, c.Patch(ctx, target, p))

	// Create a replacement k3d node so the pod has somewhere to schedule.
	t.Log("creating replacement k3d node")
	require.NoError(t, s.env.Host().CreateNode())

	// Wait for the broker to recover to Running.
	t.Log("waiting for Broker to recover to Running")
	s.waitForPhase(t, ctx, c, target, redpandav1alpha2.BrokerPhaseRunning)

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

		specBytes, err := yaml.Marshal(podSpec)
		require.NoError(t, err)
		checksum := fmt.Sprintf("%x", sha256.Sum256(specBytes))

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
		require.NoError(t, c.Create(ctx, broker))
		brokers = append(brokers, broker)
	}

	// Grants are opt-in: pod creation and adoption are deliberately NOT
	// grant-gated (RFC Q5), so the steady state needs none. Tests that
	// exercise disruptive actions grant explicitly — a blanket grant here
	// would make it impossible to assert that ungranted brokers refuse to
	// rotate or remediate.
	if opts.grantDuration > 0 {
		deadline := strconv.FormatInt(time.Now().Add(opts.grantDuration).Unix(), 10)
		for _, b := range brokers {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(b), b))
			p := client.MergeFrom(b.DeepCopy())
			if b.Annotations == nil {
				b.Annotations = map[string]string{}
			}
			checksum := b.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]
			b.Annotations["operator.redpanda.com/roll-grant"] = checksum + "/" + deadline
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
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(broker), broker))
		t.Logf("Broker %q phase=%s (want %s)", broker.Name, broker.Status.Phase, phase)
		assert.Equal(ct, phase, broker.Status.Phase)
	}, 5*time.Minute, 5*time.Second)
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
