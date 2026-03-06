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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

func TestDeleteSubResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	mgr := setupTestManagerWithScheme(t, ctx, cfg)
	c := mgr.GetLocalManager().GetClient()
	clusterName := ""

	// Register field indexes for the sub-resource types we need.
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "role", clusterName, &redpandav1alpha2.RedpandaRole{}, &redpandav1alpha2.RedpandaRoleList{})
	require.NoError(t, err)
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "user", clusterName, &redpandav1alpha2.User{}, &redpandav1alpha2.UserList{})
	require.NoError(t, err)
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "topic", clusterName, &redpandav1alpha2.Topic{}, &redpandav1alpha2.TopicList{})
	require.NoError(t, err)
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "group", clusterName, &redpandav1alpha2.Group{}, &redpandav1alpha2.GroupList{})
	require.NoError(t, err)
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "schema", clusterName, &redpandav1alpha2.Schema{}, &redpandav1alpha2.SchemaList{})
	require.NoError(t, err)
	_, err = controller.RegisterClusterSourceIndex(ctx, mgr, "shadow_link", clusterName, &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
	require.NoError(t, err)

	k8sClient := mgr.GetLocalManager().GetClient()

	// Create a Redpanda cluster CR.
	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.RedpandaSpec{},
	}
	require.NoError(t, c.Create(ctx, rp))

	clusterSource := &redpandav1alpha2.ClusterSource{
		ClusterRef: &redpandav1alpha2.ClusterRef{
			Name: rp.Name,
		},
	}

	reconciler := &RedpandaReconciler{Manager: mgr}

	t.Run("no sub-resources returns zero remaining", func(t *testing.T) {
		remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
		require.NoError(t, err)
		assert.Equal(t, 0, remaining)
	})

	t.Run("deletes referencing RedpandaRoles", func(t *testing.T) {
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-role",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.RoleSpec{
				ClusterSource: clusterSource,
			},
		}
		require.NoError(t, c.Create(ctx, role))

		// Wait for informer cache to sync the new resource.
		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")

		// Without finalizers in the test env, the role is deleted immediately.
		// Verify it's gone or has a deletion timestamp if still present.
		require.Eventually(t, func() bool {
			var fetched redpandav1alpha2.RedpandaRole
			err = c.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, &fetched)
			if apierrors.IsNotFound(err) {
				return true // Fully deleted
			}
			return err == nil && !fetched.DeletionTimestamp.IsZero() // Terminating
		}, 10*time.Second, 100*time.Millisecond, "role should be deleted or terminating")
	})

	t.Run("deletes referencing Users", func(t *testing.T) {
		user := &redpandav1alpha2.User{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-user",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.UserSpec{
				ClusterSource: clusterSource,
			},
		}
		require.NoError(t, c.Create(ctx, user))

		// Wait for informer cache to sync the new resource.
		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")
	})

	t.Run("deletes referencing Topics", func(t *testing.T) {
		topic := &redpandav1alpha2.Topic{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-topic",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.TopicSpec{
				ClusterSource: clusterSource,
			},
		}
		require.NoError(t, c.Create(ctx, topic))

		// Wait for informer cache to sync the new resource.
		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")
	})

	t.Run("deletes referencing Groups", func(t *testing.T) {
		group := &redpandav1alpha2.Group{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-group",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.GroupSpec{
				ClusterSource: clusterSource,
			},
		}
		require.NoError(t, c.Create(ctx, group))

		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")

		require.Eventually(t, func() bool {
			var fetched redpandav1alpha2.Group
			err = c.Get(ctx, types.NamespacedName{Name: group.Name, Namespace: group.Namespace}, &fetched)
			if apierrors.IsNotFound(err) {
				return true
			}
			return err == nil && !fetched.DeletionTimestamp.IsZero()
		}, 10*time.Second, 100*time.Millisecond, "group should be deleted or terminating")
	})

	t.Run("deletes referencing Schemas", func(t *testing.T) {
		schema := &redpandav1alpha2.Schema{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-schema",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.SchemaSpec{
				ClusterSource: clusterSource,
				Text:          "{}",
			},
		}
		require.NoError(t, c.Create(ctx, schema))

		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")

		require.Eventually(t, func() bool {
			var fetched redpandav1alpha2.Schema
			err = c.Get(ctx, types.NamespacedName{Name: schema.Name, Namespace: schema.Namespace}, &fetched)
			if apierrors.IsNotFound(err) {
				return true
			}
			return err == nil && !fetched.DeletionTimestamp.IsZero()
		}, 10*time.Second, 100*time.Millisecond, "schema should be deleted or terminating")
	})

	t.Run("deletes referencing ShadowLinks", func(t *testing.T) {
		shadowLink := &redpandav1alpha2.ShadowLink{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-shadow-link",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.ShadowLinkSpec{
				ShadowCluster: clusterSource,
				SourceCluster: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{
						Name: "some-source-cluster",
					},
				},
			},
		}
		require.NoError(t, c.Create(ctx, shadowLink))

		require.Eventually(t, func() bool {
			remaining, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
			return err == nil && remaining >= 1
		}, 10*time.Second, 100*time.Millisecond, "expected at least 1 remaining sub-resource after delete")

		require.Eventually(t, func() bool {
			var fetched redpandav1alpha2.ShadowLink
			err = c.Get(ctx, types.NamespacedName{Name: shadowLink.Name, Namespace: shadowLink.Namespace}, &fetched)
			if apierrors.IsNotFound(err) {
				return true
			}
			return err == nil && !fetched.DeletionTimestamp.IsZero()
		}, 10*time.Second, 100*time.Millisecond, "shadow link should be deleted or terminating")
	})

	t.Run("does not delete resources referencing a different cluster", func(t *testing.T) {
		otherCluster := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster",
				Namespace: "default",
			},
			Spec: redpandav1alpha2.RedpandaSpec{},
		}
		require.NoError(t, c.Create(ctx, otherCluster))

		otherRole := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-role",
				Namespace: rp.Namespace,
			},
			Spec: redpandav1alpha2.RoleSpec{
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{
						Name: otherCluster.Name,
					},
				},
			},
		}
		require.NoError(t, c.Create(ctx, otherRole))

		// Wait for the informer cache to index the new role before querying.
		require.Eventually(t, func() bool {
			items, err := controller.FromSourceCluster(ctx, k8sClient, "role", otherCluster, &redpandav1alpha2.RedpandaRoleList{})
			return err == nil && len(items) >= 1
		}, 10*time.Second, 100*time.Millisecond, "waiting for otherRole to appear in cache")

		// Delete sub-resources for the original cluster.
		_, err := reconciler.deleteSubResources(ctx, k8sClient, rp)
		require.NoError(t, err)

		// The role referencing other-cluster should still exist and NOT be deleted.
		var fetched redpandav1alpha2.RedpandaRole
		err = c.Get(ctx, types.NamespacedName{Name: otherRole.Name, Namespace: otherRole.Namespace}, &fetched)
		require.NoError(t, err)
		assert.True(t, fetched.DeletionTimestamp.IsZero(), "role for other cluster should NOT be deleted")
	})
}

func setupTestManagerWithScheme(t *testing.T, ctx context.Context, cfg *rest.Config) multicluster.Manager {
	t.Helper()

	mgr, err := multicluster.NewSingleClusterManager(cfg, manager.Options{
		Scheme:         controller.UnifiedScheme,
		LeaderElection: false,
	})
	require.NoError(t, err)
	go mgr.Start(ctx)
	<-mgr.Elected()

	return mgr
}
