// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

func deleteTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))
	return scheme
}

// deleteTestBroker returns a finalized Broker with one VolumeClaimTemplate,
// its (unowned) claim, and its (Broker-controlled) pod.
func deleteTestBroker(t *testing.T, scheme *runtime.Scheme, ownerRef *metav1.OwnerReference) (*redpandav1alpha2.Broker, *corev1.Pod, *corev1.PersistentVolumeClaim) {
	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rp-broker-0", Namespace: "test", UID: "broker-uid",
			Finalizers: []string{brokerFinalizerName},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(0)),
			Storage: redpandav1alpha2.BrokerStorage{
				VolumeClaimTemplates: []redpandav1alpha2.BrokerVolumeClaim{{Name: "datadir"}},
			},
		},
	}
	if ownerRef != nil {
		broker.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: broker.PodName(), Namespace: "test"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
	}
	require.NoError(t, controllerutil.SetControllerReference(broker, pod, scheme))
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "datadir-" + broker.PodName(), Namespace: "test"},
	}
	return broker, pod, pvc
}

// TestReconcileDeleteIdempotentRelease pins the re-entry convergence of the
// release path: a pass that released the pod but failed to remove the
// finalizer must, on re-entry, land in the SAME branch and converge — the
// old design branched on the pod's ownership and, re-entering after the
// release, mistook itself for a rollback and left the claims to the GC.
func TestReconcileDeleteIdempotentRelease(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	broker, pod, pvc := deleteTestBroker(t, scheme, nil) // no owner: policy resolves to release

	failNextBrokerUpdate := true
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker, pod, pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*redpandav1alpha2.Broker); ok && failNextBrokerUpdate {
					failNextBrokerUpdate = false
					return apierrors.NewConflict(schema.GroupResource{Group: redpandav1alpha2.GroupVersion.Group, Resource: "brokers"}, obj.GetName(), nil)
				}
				return cl.Update(ctx, obj, opts...)
			},
		}).Build()
	r := &BrokerReconciler{}

	// Pass 1: releases the pod, then fails removing the finalizer.
	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", broker, broker.PodName())
	require.Error(t, err)

	var released corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &released))
	require.Nil(t, metav1.GetControllerOf(&released), "pass 1 must have released the pod")

	// Pass 2 (re-entry): same intent, same branch, converges.
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	_, err = r.reconcileDelete(ctx, logr.Discard(), c, "", &b, b.PodName())
	require.NoError(t, err)

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	require.Empty(t, b.Finalizers, "re-entry must remove the finalizer")

	var keptPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pvc), &keptPVC),
		"the claim must survive a release, no matter how many re-entries it takes")
	require.Empty(t, keptPVC.OwnerReferences, "claims are never owned")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &released))
	require.Nil(t, metav1.GetControllerOf(&released))
}

// TestReconcileDeleteCascadeDeletesClaimsExplicitly pins the teardown
// semantics: with the owning cluster gone and the default (cascade) policy,
// the finalizer deletes the claims itself — the GC cannot, because Brokers
// do not own them.
func TestReconcileDeleteCascadeDeletesClaimsExplicitly(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	// Controller-owned by a Redpanda that does not exist => owner tearing down.
	broker, pod, pvc := deleteTestBroker(t, scheme, &metav1.OwnerReference{
		APIVersion: "cluster.redpanda.com/v1alpha2", Kind: redpandav1alpha2.RedpandaKind,
		Name: "rp", UID: "owner-uid", Controller: ptr.To(true),
	})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker, pod, pvc).Build()
	r := &BrokerReconciler{}

	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", broker, broker.PodName())
	require.NoError(t, err)

	var gone corev1.PersistentVolumeClaim
	require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(pvc), &gone)),
		"cascade must delete the claim explicitly")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	require.Empty(t, b.Finalizers)
	// The pod is CR-owned; its deletion is the garbage collector's job.
	var keptPod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &keptPod))
}

// TestReconcileDeleteTeardownOrphanKeepsClaims pins the escape hatch: at
// teardown, the orphan policy retains DATA only. The claims survive, but the
// pod's ownerRef stays intact so the GC removes it with the CR — no
// unmanaged pod may outlive its cluster.
func TestReconcileDeleteTeardownOrphanKeepsClaims(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	// Controller-owned by a Redpanda that does not exist => owner tearing down.
	broker, pod, pvc := deleteTestBroker(t, scheme, &metav1.OwnerReference{
		APIVersion: "cluster.redpanda.com/v1alpha2", Kind: redpandav1alpha2.RedpandaKind,
		Name: "rp", UID: "owner-uid", Controller: ptr.To(true),
	})
	broker.SetBrokerDeletionPolicy(redpandav1alpha2.BrokerDeletionPolicyOrphan)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker, pod, pvc).Build()
	r := &BrokerReconciler{}

	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", broker, broker.PodName())
	require.NoError(t, err)

	var kept corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pvc), &kept),
		"orphan policy must keep the claims at teardown")
	var keptPod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &keptPod))
	require.True(t, metav1.IsControlledBy(&keptPod, broker),
		"the pod must stay CR-owned so the GC deletes it with the CR")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	require.Empty(t, b.Finalizers)
}

// TestReconcileDeleteOwnerAliveIgnoresCascadeAnnotation pins the release
// invariant: deleting a single Broker CR while its cluster is alive always
// releases the pod and keeps the claims, regardless of the deletion-policy
// annotation. Rollback deletes Broker CRs with the owner alive — an
// annotation honored here would destroy data on rollback.
func TestReconcileDeleteOwnerAliveIgnoresCascadeAnnotation(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	broker, pod, pvc := deleteTestBroker(t, scheme, &metav1.OwnerReference{
		APIVersion: "cluster.redpanda.com/v1alpha2", Kind: redpandav1alpha2.RedpandaKind,
		Name: "rp", UID: "owner-uid", Controller: ptr.To(true),
	})
	broker.SetBrokerDeletionPolicy(redpandav1alpha2.BrokerDeletionPolicyCascade)
	owner := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, broker, pod, pvc).Build()
	r := &BrokerReconciler{}

	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", broker, broker.PodName())
	require.NoError(t, err)

	var kept corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pvc), &kept),
		"the claims must survive any single-CR deletion while the owner is alive")
	var released corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &released))
	require.Nil(t, metav1.GetControllerOf(&released), "the pod must be released")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	require.Empty(t, b.Finalizers)
}

// TestReconcileDeleteTombstoneKeepsReplacementClaims pins the DiskLost
// guard: a tombstone's claim NAMES belong to its replacement Broker at the
// same network index, and claims are unowned — so the tombstone must never
// delete them, under any policy.
func TestReconcileDeleteTombstoneKeepsReplacementClaims(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	broker, pod, pvc := deleteTestBroker(t, scheme, &metav1.OwnerReference{
		APIVersion: "cluster.redpanda.com/v1alpha2", Kind: redpandav1alpha2.RedpandaKind,
		Name: "rp", UID: "owner-uid", Controller: ptr.To(true),
	})
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker, pod, pvc).Build()
	r := &BrokerReconciler{}

	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", broker, broker.PodName())
	require.NoError(t, err)

	var kept corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pvc), &kept),
		"a tombstone must never delete claims at its (reused) names")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(broker), &b))
	require.Empty(t, b.Finalizers)
}

// TestReconcileDeleteTombstoneNeverDecommissionsReplacement pins the K8S-976
// regression: a DiskLost tombstone whose dead node_id already finished
// decommissioning (BrokerID cleared on completion) is deleted while its pod
// NAME — matched against cluster membership — already belongs to the
// replacement Broker at the same network index. Resolving identity through
// the pod decommissioned the live replacement; resolveBroker must refuse to
// resolve a tombstone through its pod, so the deletion completes as a
// terminal no-op.
func TestReconcileDeleteTombstoneNeverDecommissionsReplacement(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	tombstone, _, _ := deleteTestBroker(t, scheme, nil)
	tombstone.Spec.Decommission = true
	tombstone.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
	tombstone.Status.Phase = redpandav1alpha2.BrokerPhaseDecommissioned

	// The replacement Broker at the same index: it owns the pod at the shared
	// name and is registered in the cluster as node_id 3.
	replacement, pod, _ := deleteTestBroker(t, scheme, nil)
	replacement.Name, replacement.UID = "rp-broker-0-repl", "replacement-uid"
	pod.OwnerReferences = nil
	require.NoError(t, controllerutil.SetControllerReference(replacement, pod, scheme))
	pod.Status.PodIP = "10.0.0.3"

	var calls adminCalls
	srv := fakeAdminServer(t, replacementMembership(tombstone.PodName()), &calls)

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&redpandav1alpha2.Broker{}).
		WithObjects(tombstone, replacement, pod).Build()
	r := &BrokerReconciler{ClientFactory: stubAdminFactory{url: srv.URL}}

	_, err := r.reconcileDelete(ctx, logr.Discard(), c, "", tombstone, tombstone.PodName())
	require.NoError(t, err)

	require.Empty(t, calls.decommissions,
		"a tombstone with no recorded node_id must never decommission anything")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(tombstone), &b))
	require.Empty(t, b.Finalizers, "the tombstone's deletion must complete")
	require.Nil(t, b.Status.BrokerID, "the replacement's identity must never contaminate the tombstone")
	var keptPod corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &keptPod))
	require.True(t, metav1.IsControlledBy(&keptPod, replacement),
		"the replacement's pod must survive the tombstone's deletion untouched")
}

// TestResolveBroker pins the tombstone rule of K8S-976: a DiskLost Broker
// resolves only by its pinned node_id — never through its pod name or IP,
// which belong to the replacement from network-index release onward.
func TestResolveBroker(t *testing.T) {
	podName := "rp-broker-0"
	for _, tc := range []struct {
		name       string
		diskLost   bool
		brokerID   *int32
		wantFound  bool
		wantNodeID int
	}{
		{"live broker matches by pod name", false, nil, true, 3},
		{"tombstone with no recorded id resolves nothing", true, nil, false, 0},
		{"tombstone resolves its pinned id, not the pod-name match", true, ptr.To(int32(1)), true, 1},
		{"tombstone whose pinned id left the membership", true, ptr.To(int32(9)), false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls adminCalls
			srv := fakeAdminServer(t, replacementMembership(podName), &calls)
			broker := &redpandav1alpha2.Broker{}
			broker.Status.BrokerID = tc.brokerID
			if tc.diskLost {
				broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}
			}
			r := &BrokerReconciler{ClientFactory: stubAdminFactory{url: srv.URL}}

			resolved, found, err := r.resolveBroker(t.Context(), "", broker, nil, podName)
			require.NoError(t, err)
			require.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.Equal(t, tc.wantNodeID, resolved.NodeID)
			}
		})
	}
}

// TestExecuteDecommission pins the liveness guard on the K8S-976 upgrade
// path: a disk-lost incarnation's node cannot be alive, so an alive
// membership entry at the pinned node_id proves the id belongs to a live
// node (the replacement, adopted by a pre-fix operator) and must park in
// Stuck instead of decommissioning. Dead, liveness-unreported, and
// non-DiskLost targets must keep decommissioning.
func TestExecuteDecommission(t *testing.T) {
	member := func(id int, alive *bool) rpadmin.Broker {
		return rpadmin.Broker{NodeID: id, MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: alive}
	}
	for _, tc := range []struct {
		name            string
		diskLost        bool
		target          rpadmin.Broker
		wantPhase       redpandav1alpha2.BrokerPhase
		wantDecommCalls int
	}{
		{"tombstone refuses its alive pinned id", true, member(3, ptr.To(true)), redpandav1alpha2.BrokerPhaseStuck, 0},
		{"tombstone decommissions its dead pinned id", true, member(3, ptr.To(false)), redpandav1alpha2.BrokerPhaseDecommissioning, 1},
		{"tombstone decommissions when liveness is unreported", true, member(3, nil), redpandav1alpha2.BrokerPhaseDecommissioning, 1},
		{"live broker decommissions while alive", false, member(3, ptr.To(true)), redpandav1alpha2.BrokerPhaseDecommissioning, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls adminCalls
			srv := fakeAdminServer(t, []rpadmin.Broker{member(1, ptr.To(true)), tc.target}, &calls)
			broker := &redpandav1alpha2.Broker{}
			broker.Status.BrokerID = ptr.To(int32(3))
			if tc.diskLost {
				broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}
			}
			r := &BrokerReconciler{ClientFactory: stubAdminFactory{url: srv.URL}}

			result, err := r.executeDecommission(t.Context(), "", broker)
			require.NoError(t, err)
			require.Equal(t, tc.wantPhase, result.phase)
			require.Len(t, calls.decommissions, tc.wantDecommCalls)
		})
	}
}

// TestReconcileDeleteContaminatedTombstoneHoldsDeletion pins the K8S-976
// upgrade path end to end: a pre-fix operator persisted the live
// replacement's node_id into the tombstone's Status.BrokerID, so the
// deletion path skips resolveBroker and goes straight to the decommission.
// The liveness guard must refuse it and hold the deletion in Stuck for a
// human (manual recommission, manual finalizer removal) instead of
// re-decommissioning the replacement on every pass.
func TestReconcileDeleteContaminatedTombstoneHoldsDeletion(t *testing.T) {
	ctx := context.Background()
	scheme := deleteTestScheme(t)
	tombstone, _, _ := deleteTestBroker(t, scheme, nil)
	tombstone.Spec.Decommission = true
	tombstone.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
	tombstone.Status.BrokerID = ptr.To(int32(3)) // contaminated: the live replacement's id

	var calls adminCalls
	srv := fakeAdminServer(t, replacementMembership(tombstone.PodName()), &calls)

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&redpandav1alpha2.Broker{}).
		WithObjects(tombstone).Build()
	r := &BrokerReconciler{ClientFactory: stubAdminFactory{url: srv.URL}}

	res, err := r.reconcileDelete(ctx, logr.Discard(), c, "", tombstone, tombstone.PodName())
	require.NoError(t, err)
	require.Equal(t, periodicRequeue, res.RequeueAfter, "a held deletion must keep re-checking")

	require.Empty(t, calls.decommissions, "an alive pinned id must never be decommissioned")
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(tombstone), &b))
	require.Contains(t, b.Finalizers, brokerFinalizerName, "the deletion must hold for a human")
	require.Equal(t, redpandav1alpha2.BrokerPhaseStuck, b.Status.Phase)
}

// adminCalls records the decommission PUTs the fake admin server received.
type adminCalls struct {
	decommissions []string
}

// replacementMembership is the post-disk-loss cluster: the dead node already
// decommissioned and gone, its replacement (node 3) registered at the
// tombstone's released pod name.
func replacementMembership(podName string) []rpadmin.Broker {
	alive := ptr.To(true)
	return []rpadmin.Broker{
		{NodeID: 1, InternalRPCAddress: "rp-1.rp.test.svc.cluster.local", MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: alive},
		{NodeID: 2, InternalRPCAddress: "rp-2.rp.test.svc.cluster.local", MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: alive},
		{NodeID: 3, InternalRPCAddress: podName + ".rp.test.svc.cluster.local", MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: alive},
	}
}

// stubAdminFactory implements only RedpandaAdminClientForCluster, pointing
// every client at one fake admin server; the embedded nil interface panics on
// any other method.
type stubAdminFactory struct {
	internalclient.ClientFactory
	url string
}

func (s stubAdminFactory) RedpandaAdminClientForCluster(context.Context, any, string) (*rpadmin.AdminAPI, error) {
	return rpadmin.NewAdminAPI([]string{s.url}, new(rpadmin.NopAuth), nil)
}

// fakeAdminServer serves the given cluster membership, answers any
// decommission status probe with "not decommissioning" (the reply that makes
// the controller initiate one), and records every decommission PUT it
// receives into calls.
func fakeAdminServer(t *testing.T, brokers []rpadmin.Broker, calls *adminCalls) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/brokers":
			require.NoError(t, json.NewEncoder(w).Encode(brokers))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/decommission"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "the node is not decommissioning", "code": 400}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/decommission"):
			calls.decommissions = append(calls.decommissions, r.URL.Path)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
