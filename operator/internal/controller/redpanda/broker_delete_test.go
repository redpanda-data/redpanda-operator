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
	"testing"

	"github.com/go-logr/logr"
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
