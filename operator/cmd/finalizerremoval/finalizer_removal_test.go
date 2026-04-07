// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package finalizerremoval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

// TestRemoveFinalizersForGVK exercises the post-delete finalizer removal logic
// that runs as a helm hook after the operator is uninstalled. The primary
// scenario is orphaned CRs: the operator has been removed but CRs still carry
// the operator.redpanda.com/finalizer, which blocks Kubernetes from garbage
// collecting them and prevents namespace deletion from completing.
//
// Covered scenarios:
//   - Orphaned CRs have their operator finalizer stripped so they no longer
//     block garbage collection.
//   - Multiple CR types (Topic, User, etc.) are handled independently,
//     reflecting real deployments where several resource kinds coexist.
//   - CRs that never had the operator finalizer are left untouched.
//   - GVKs whose CRDs are not installed on the cluster are skipped gracefully,
//     which happens when the operator manages types the cluster never used.
//   - After finalizer removal, CRs can be deleted and the namespace teardown
//     is no longer blocked.
func TestRemoveFinalizersForGVK(t *testing.T) {
	ctx := context.Background()
	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)

	k8sClient, err := client.New(cfg, client.Options{})
	require.NoError(t, err)

	topicCRD := crds.Topic()
	gvk := schema.GroupVersionKind{
		Group:   topicCRD.Spec.Group,
		Version: topicCRD.Spec.Versions[0].Name,
		Kind:    topicCRD.Spec.Names.Kind,
	}

	userCRD := crds.User()
	userGVK := schema.GroupVersionKind{
		Group:   userCRD.Spec.Group,
		Version: userCRD.Spec.Versions[0].Name,
		Kind:    userCRD.Spec.Names.Kind,
	}

	// Helper to create an unstructured CR with the operator finalizer.
	createCR := func(t *testing.T, gvk schema.GroupVersionKind, name, ns string) *unstructured.Unstructured {
		t.Helper()
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(name)
		obj.SetNamespace(ns)
		// Set required spec field so the API server accepts the object.
		_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{}, "spec")
		require.NoError(t, k8sClient.Create(ctx, obj))
		// Add finalizer via patch after creation.
		patch := client.MergeFrom(obj.DeepCopy())
		controllerutil.AddFinalizer(obj, finalizerKey)
		require.NoError(t, k8sClient.Patch(ctx, obj, patch))
		return obj
	}

	// Orphaned CR: the operator is gone but the CR still carries the finalizer.
	// The post-delete hook must strip it so the object is no longer stuck.
	t.Run("removes finalizer from orphaned CRs", func(t *testing.T) {
		topic := createCR(t, gvk, "orphaned-topic", "default")

		require.True(t, controllerutil.ContainsFinalizer(topic, finalizerKey))

		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, gvk))

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(topic), topic))
		require.False(t, controllerutil.ContainsFinalizer(topic, finalizerKey))
	})

	// Real deployments typically have several CR types (Topics, Users, Roles,
	// Schemas, etc.) all with orphaned finalizers after operator uninstall.
	// Each type must be handled independently.
	t.Run("removes finalizers from multiple CR types", func(t *testing.T) {
		topic := createCR(t, gvk, "multi-topic", "default")
		user := createCR(t, userGVK, "multi-user", "default")

		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, gvk))
		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, userGVK))

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(topic), topic))
		require.False(t, controllerutil.ContainsFinalizer(topic, finalizerKey))

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(user), user))
		require.False(t, controllerutil.ContainsFinalizer(user, finalizerKey))
	})

	// CRs that were created but never reconciled by the operator (or whose
	// finalizer was already removed) must not be modified.
	t.Run("skips CRs without the operator finalizer", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName("no-finalizer-topic")
		obj.SetNamespace("default")
		_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{}, "spec")
		require.NoError(t, k8sClient.Create(ctx, obj))

		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, gvk))

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		require.Empty(t, obj.GetFinalizers())
	})

	// The operator manages many CRD types but not all may be installed on a
	// given cluster. When the API server doesn't recognize a GVK the removal
	// should log and continue rather than failing the entire job.
	t.Run("skips unknown GVK without error", func(t *testing.T) {
		unknownGVK := schema.GroupVersionKind{
			Group:   "nonexistent.redpanda.com",
			Version: "v1",
			Kind:    "Fake",
		}
		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, unknownGVK))
	})

	// End-to-end: a CR with a finalizer cannot be garbage collected. After
	// the finalizer is removed the CR should be deletable, which unblocks
	// namespace deletion.
	t.Run("orphaned CRs deletable after finalizer removal", func(t *testing.T) {
		topic := createCR(t, gvk, "deletable-topic", "default")

		require.NoError(t, removeFinalizersForGVK(ctx, k8sClient, gvk))

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(topic), topic))
		require.False(t, controllerutil.ContainsFinalizer(topic, finalizerKey))

		// The object should now be deletable without the finalizer blocking.
		require.NoError(t, k8sClient.Delete(ctx, topic))

		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(topic), topic)
		require.True(t, client.IgnoreNotFound(err) == nil, "object should be deleted")
	})
}
