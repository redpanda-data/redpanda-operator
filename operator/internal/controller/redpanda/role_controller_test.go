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
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func testRoleReconcile(t *testing.T, cfg *rest.Config) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	authorizationSpec := &redpandav1alpha2.RoleAuthorizationSpec{
		ACLs: []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeGroup,
				Name: "group",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationDescribe,
			},
		}},
	}

	baseRole := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:testuser1", "User:testuser2"},
			Authorization: authorizationSpec,
		},
	}

	for name, tt := range map[string]struct {
		mutate            func(role *redpandav1alpha2.RedpandaRole)
		expectedCondition metav1.Condition
		onlyCheckDeletion bool
	}{
		"success - role and authorization": {
			expectedCondition: environment.SyncedCondition,
		},
		"success - role and authorization deletion cleanup": {
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - role only (no authorization)": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - role only deletion cleanup": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization only (no principals)": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.Principals = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization only deletion cleanup": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.Principals = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"error - invalid cluster ref": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.ClusterSource = environment.ClusterSourceInvalidRef
			},
			expectedCondition: environment.InvalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.ClusterSource = environment.ClusterSourceNoSASL
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.ClusterSource = environment.ClusterSourceBadPassword
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"partial sync - SR ACLs without SR configured": {
			mutate: func(role *redpandav1alpha2.RedpandaRole) {
				role.Spec.ClusterSource = environment.ClusterSourceNoSchemaRegistry
				role.Spec.Authorization = &redpandav1alpha2.RoleAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeTopic,
								Name: "test-topic",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
								Name: "test-subject",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
					},
				}
			},
			expectedCondition: environment.PartiallySyncedCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			role := baseRole.DeepCopy()
			role.Name = "role" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(role)
			}

			k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(role)
			req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

			require.NoError(t, k8sClient.Create(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, k8sClient.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, role.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, role.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, role.Status.Conditions[0].Reason)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				syncer, err := environment.Factory.ACLs(ctx, role)
				require.NoError(t, err)
				defer syncer.Close()

				rolesClient, err := environment.Factory.Roles(ctx, role)
				require.NoError(t, err)
				defer rolesClient.Close()

				// if we're supposed to have synced, then check to make sure we properly
				// set the management flags
				require.Equal(t, role.ShouldManageACLs(), role.Status.ManagedACLs)
				require.Equal(t, role.ShouldManageRole(), role.Status.ManagedRole)
				require.Equal(t, role.ShouldManagePrincipals(), role.Status.ManagedPrincipals)

				if role.ShouldManageRole() {
					// make sure we actually have a role
					hasRole, err := rolesClient.Has(ctx, role)
					require.NoError(t, err)
					require.True(t, hasRole)
				}

				if role.ShouldManageACLs() {
					// make sure we actually have ACLs
					acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 1)
				}

				if !tt.onlyCheckDeletion {
					if role.ShouldManageRole() {
						// Test role updates by changing principals
						role.Spec.Principals = []string{"User:newuser1", "User:newuser2"}
						require.NoError(t, k8sClient.Update(ctx, role))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, k8sClient.Get(ctx, key, role))
						require.True(t, role.Status.ManagedRole)

					}

					if role.ShouldManageACLs() {
						// now clear out any managed ACLs and re-check
						role.Spec.Authorization = nil
						require.NoError(t, k8sClient.Update(ctx, role))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, k8sClient.Get(ctx, key, role))
						require.False(t, role.Status.ManagedACLs)
					}

					// make sure we no longer have acls
					acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, k8sClient.Delete(ctx, role))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))

				// make sure we no longer have a role
				hasRole, err := rolesClient.Has(ctx, role)
				require.NoError(t, err)
				require.False(t, hasRole)

				// make sure we no longer have acls
				acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
				require.NoError(t, err)
				require.Len(t, acls, 0)

				return
			}

			// clean up and make sure we properly delete everything
			require.NoError(t, k8sClient.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
		})
	}
}

func testRolePrincipalsAndACLs(t *testing.T, cfg *rest.Config) { // nolint:funlen // Comprehensive test coverage
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Probe v2 support once for all subtests. We need a temporary role object
	// to create a roles client via the factory.
	probeRole := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
			Name:      "probe-role",
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
		},
	}
	probeClient, err := environment.Factory.Roles(ctx, probeRole)
	require.NoError(t, err)
	supportsGroups := probeClient.SupportsGroups()
	probeClient.Close()

	// Test different role configurations
	testCases := []struct {
		name             string
		requiresGroups   bool // if true, skip when v2 SecurityService API is unavailable
		principals       []string
		authorization    *redpandav1alpha2.RoleAuthorizationSpec
		expectedACLs     int
		shouldManageRole bool
		shouldManageACLs bool
		description      string
	}{
		{
			name:             "principals-only-mode",
			principals:       []string{"User:alice", "User:bob"},
			authorization:    nil,
			expectedACLs:     0,
			shouldManageRole: true,
			shouldManageACLs: false,
			description:      "Role with principals only, no ACLs",
		},
		{
			name:             "group-principals-only-mode",
			requiresGroups:   true,
			principals:       []string{"Group:engineering", "Group:platform"},
			authorization:    nil,
			expectedACLs:     0,
			shouldManageRole: true,
			shouldManageACLs: false,
			description:      "Role with group principals only, no ACLs",
		},
		{
			name:             "mixed-user-group-principals",
			requiresGroups:   true,
			principals:       []string{"User:alice", "Group:engineering"},
			authorization:    nil,
			expectedACLs:     0,
			shouldManageRole: true,
			shouldManageACLs: false,
			description:      "Role with mixed user and group principals, no ACLs",
		},
		{
			name:       "acls-only-mode",
			principals: nil,
			authorization: &redpandav1alpha2.RoleAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeTopic,
						Name: "test-topic",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
					},
				}},
			},
			expectedACLs:     1,
			shouldManageRole: true,
			shouldManageACLs: true,
			description:      "Role with ACLs only, no principals",
		},
		{
			name:           "combined-mode-group-principals-with-acls",
			requiresGroups: true,
			principals:     []string{"Group:engineering", "User:alice"},
			authorization: &redpandav1alpha2.RoleAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeTopic,
						Name: "team-topic",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
					},
				}},
			},
			expectedACLs:     1,
			shouldManageRole: true,
			shouldManageACLs: true,
			description:      "Role with group and user principals plus ACLs",
		},
		{
			name:       "combined-mode",
			principals: []string{"User:charlie", "User:dave"},
			authorization: &redpandav1alpha2.RoleAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "team-topic",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
							redpandav1alpha2.ACLOperationWrite,
						},
					},
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeGroup,
							Name: "team-group",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
						},
					},
				},
			},
			expectedACLs:     3, // 2 topic + 1 group
			shouldManageRole: true,
			shouldManageACLs: true,
			description:      "Role with both principals and ACLs",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.requiresGroups && !supportsGroups {
				t.Skip("Skipping: v2 SecurityService API not available, Group principals not supported")
			}
			role := &redpandav1alpha2.RedpandaRole{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: environment.Namespace,
					Name:      "test-role-" + strconv.Itoa(int(time.Now().UnixNano())),
				},
				Spec: redpandav1alpha2.RoleSpec{
					ClusterSource: environment.ClusterSourceValid,
					Principals:    tt.principals,
					Authorization: tt.authorization,
				},
			}

			k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(role)
			req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

			// Create and reconcile
			require.NoError(t, k8sClient.Create(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			// Verify status
			require.NoError(t, k8sClient.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status)

			// Verify management flags
			require.Equal(t, tt.shouldManageRole, role.ShouldManageRole(), tt.description)
			require.Equal(t, tt.shouldManageACLs, role.ShouldManageACLs(), tt.description)
			require.Equal(t, tt.shouldManageRole, role.Status.ManagedRole, tt.description)
			require.Equal(t, tt.shouldManageACLs, role.Status.ManagedACLs, tt.description)
			require.Equal(t, len(tt.principals) > 0, role.Status.ManagedPrincipals, tt.description)

			// Verify role exists if managed
			if tt.shouldManageRole {
				rolesClient, err := environment.Factory.Roles(ctx, role)
				require.NoError(t, err)
				defer rolesClient.Close()

				hasRole, err := rolesClient.Has(ctx, role)
				require.NoError(t, err)
				require.True(t, hasRole, "Role should exist in Redpanda")
			}

			// Verify ACLs if managed
			if tt.shouldManageACLs {
				syncer, err := environment.Factory.ACLs(ctx, role)
				require.NoError(t, err)
				defer syncer.Close()

				acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
				require.NoError(t, err)
				require.Len(t, acls, tt.expectedACLs, tt.description)
			}

			// Clean up
			require.NoError(t, k8sClient.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
		})
	}
}

func testRoleLifecycleTransitions(t *testing.T, cfg *rest.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	role := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
			Name:      "lifecycle-role-" + strconv.Itoa(int(time.Now().UnixNano())),
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:lifecycle-user"},
			// Start in principals-only mode
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	// Phase 1: Create in principals-only mode
	t.Run("create_principals_only", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Create(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.False(t, role.ShouldManageACLs())
		require.True(t, role.ShouldManagePrincipals())
		require.True(t, role.Status.ManagedRole)
		require.False(t, role.Status.ManagedACLs)
		require.True(t, role.Status.ManagedPrincipals)

		// Verify role exists but no ACLs
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0)
	})

	// Phase 2: Transition to combined mode
	t.Run("add_authorization", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Authorization = &redpandav1alpha2.RoleAuthorizationSpec{
			ACLs: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTopic,
					Name: "lifecycle-topic",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			}},
		}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.True(t, role.ShouldManageACLs())
		require.True(t, role.ShouldManagePrincipals())
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedACLs)
		require.True(t, role.Status.ManagedPrincipals)

		// Verify both role and ACLs exist
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1)
	})

	// Phase 3: Update principals
	t.Run("update_principals", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:lifecycle-user", "User:additional-user"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedACLs)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:lifecycle-user", "User:additional-user"}, role.Spec.Principals)

		// Verify role still exists with updated principals and ACLs remain
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1)
	})

	// Phase 4: Remove authorization (back to principals-only)
	t.Run("remove_authorization", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Authorization = nil

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.False(t, role.ShouldManageACLs())
		require.True(t, role.ShouldManagePrincipals())
		require.True(t, role.Status.ManagedRole)
		require.False(t, role.Status.ManagedACLs)
		require.True(t, role.Status.ManagedPrincipals)

		// Verify role still exists but ACLs are removed
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0)
	})

	// Phase 5: Clean up
	t.Run("cleanup", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Delete(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
	})
}

func testRoleMembershipReconciliation(t *testing.T, cfg *rest.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Create a role with initial members
	role := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
			Name:      "membership-role-" + strconv.Itoa(int(time.Now().UnixNano())),
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:alice", "User:bob"},
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	// Initial creation
	t.Run("initial_creation", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Create(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:alice", "User:bob"}, role.Spec.Principals)

		// Verify role exists with correct members
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole)
	})

	// Test 1: Add a new member
	t.Run("add_member", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:alice", "User:bob", "User:charlie"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:alice", "User:bob", "User:charlie"}, role.Spec.Principals)

		// Verify the role still exists and reconciliation was triggered
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist after membership update")
	})

	// Test 2: Remove a member
	t.Run("remove_member", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:alice", "User:charlie"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:alice", "User:charlie"}, role.Spec.Principals)

		// Verify the role still exists after removing a member
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist after member removal")
	})

	// Test 3: Replace all members
	t.Run("replace_all_members", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:dave", "User:eve"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:dave", "User:eve"}, role.Spec.Principals)

		// Verify the role still exists after replacing all members
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist after member replacement")
	})

	// Test 4: Add group principals
	t.Run("add_group_principals", func(t *testing.T) {
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		supportsGroups := rolesClient.SupportsGroups()
		rolesClient.Close()

		if !supportsGroups {
			t.Skip("Skipping: v2 SecurityService API not available, Group principals not supported")
		}

		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:dave", "User:eve", "Group:engineering"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"User:dave", "User:eve", "Group:engineering"}, role.Spec.Principals)

		rolesClient, err = environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist after adding group principals")
	})

	// Test 5: Replace with group-only principals
	t.Run("group_only_principals", func(t *testing.T) {
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		supportsGroups := rolesClient.SupportsGroups()
		rolesClient.Close()

		if !supportsGroups {
			t.Skip("Skipping: v2 SecurityService API not available, Group principals not supported")
		}

		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = []string{"Group:engineering", "Group:platform"}

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedPrincipals)
		require.Equal(t, []string{"Group:engineering", "Group:platform"}, role.Spec.Principals)

		rolesClient, err = environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist with group-only principals")
	})

	// Test 6: Remove all members (empty principals list)
	t.Run("remove_all_members", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Principals = nil

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.False(t, role.Status.ManagedPrincipals) // No longer managing principals
		require.Empty(t, role.Spec.Principals)

		// Verify the role still exists even with no members
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should still exist with empty membership")
	})

	// Cleanup
	t.Run("cleanup", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Delete(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
	})
}

func testRoleRename(t *testing.T, cfg *rest.Config) {
	// Tests role rename happy path: K8s name → internal name → different internal name
	// Verifies old roles are deleted and new roles created without orphaning.
	// The interrupted-rename path (status must keep the previous effective
	// name until cleanup completes) is covered by testRoleRenameInterrupted.

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Create role with initial name (no internal name)
	role := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
			Name:      "updateable-role",
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:user1", "User:user2"},
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	// Phase 1: Create role without internal name
	t.Run("create_role_without_internal_name", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Create(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.Equal(t, "updateable-role", role.Status.EffectiveRoleName, "Status should track effective name")

		// Verify role exists in cluster with K8s name
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role should exist with name 'updateable-role'")
	})

	// Phase 2: Rename by setting internal flag
	t.Run("rename_via_internal_flag", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Internal = true

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.Equal(t, "__updateable-role", role.Status.EffectiveRoleName, "Status should update to new effective name")

		// Verify new role exists
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasNewRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasNewRole, "New role '__updateable-role' should exist")

		// Verify old role is deleted
		oldRole := role.DeepCopy()
		oldRole.Spec.Internal = false // This makes GetEffectiveRoleName return "updateable-role"
		hasOldRole, err := rolesClient.Has(ctx, oldRole)
		require.NoError(t, err)
		require.False(t, hasOldRole, "Old role 'updateable-role' should be deleted")
	})

	// Phase 3: Toggle internal flag back to verify multiple renames work
	t.Run("toggle_internal_back", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		role.Spec.Internal = false

		require.NoError(t, k8sClient.Update(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, role))
		require.Equal(t, "updateable-role", role.Status.EffectiveRoleName, "Status should update back to K8s name")

		// Verify role with K8s name exists
		rolesClient, err := environment.Factory.Roles(ctx, role)
		require.NoError(t, err)
		defer rolesClient.Close()

		hasRole, err := rolesClient.Has(ctx, role)
		require.NoError(t, err)
		require.True(t, hasRole, "Role 'updateable-role' should exist")

		// Check previous internal name is gone
		previousRole := role.DeepCopy()
		previousRole.Spec.Internal = true
		hasPreviousRole, err := rolesClient.Has(ctx, previousRole)
		require.NoError(t, err)
		require.False(t, hasPreviousRole, "Previous role '__updateable-role' should be deleted")
	})

	// Cleanup
	t.Run("cleanup", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Delete(ctx, role))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
	})
}

func testRoleRenameInterrupted(t *testing.T, cfg *rest.Config) {
	// A rename interrupted by a transient failure must not advance
	// status.EffectiveRoleName: the caller applies the status patch even when
	// SyncResource errors, and once status records the new name the old
	// role would never be revisited, permanently orphaning it in Redpanda.
	// The in-flight target is recorded in status.PendingEffectiveRoleName
	// instead, so cleanup also survives the spec reverting mid-rename.

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, cfg, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// The role must carry ACLs: the rename path only touches the
	// SASL-authenticated Kafka client through the ACL syncer (the dev
	// container's admin API is unauthenticated), and the interruptions below
	// work by breaking those credentials. That pins where every interrupted
	// pass dies: after the new role is created, before any cleanup.
	role := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: environment.Namespace,
			Name:      "interrupted-role",
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:user1"},
			Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeGroup,
						Name: "group",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationDescribe,
					},
				}},
			},
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
	require.NoError(t, err)

	// The unauthenticated admin API can inspect roles in Redpanda even while
	// the Kafka credentials are corrupted.
	adminClient, err := rpadmin.NewAdminAPI([]string{environment.AdminURL}, &rpadmin.NopAuth{}, nil)
	require.NoError(t, err)
	defer adminClient.Close()

	hasRole := func(name string) bool {
		t.Helper()
		if _, err := adminClient.Role(ctx, name); err != nil {
			var httpErr *rpadmin.HTTPResponseError
			require.ErrorAs(t, err, &httpErr)
			require.Equal(t, http.StatusNotFound, httpErr.Response.StatusCode)
			return false
		}
		return true
	}

	principals := func(name string) []string {
		t.Helper()
		resp, err := adminClient.RoleMembers(ctx, name)
		require.NoError(t, err)
		var out []string
		for _, member := range resp.Members {
			out = append(out, member.PrincipalType+":"+member.Name)
		}
		return out
	}

	secretKey := client.ObjectKey{Namespace: environment.Namespace, Name: "superuser"}
	setPassword := func(password string) {
		t.Helper()
		var secret corev1.Secret
		require.NoError(t, k8sClient.Get(ctx, secretKey, &secret))
		secret.Data["password"] = []byte(password)
		require.NoError(t, k8sClient.Update(ctx, &secret))
	}

	require.NoError(t, k8sClient.Create(ctx, role))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, k8sClient.Get(ctx, key, role))
	require.Equal(t, "interrupted-role", role.Status.EffectiveRoleName)

	// Interrupt a rename: flip the internal flag while the SASL password
	// secret referenced by the (immutable) ClusterSource is corrupted, so the
	// pass dies at ACL sync. Terminal client errors surface as a condition,
	// not a returned error, so the reconcile result is ignored here.
	setPassword("wrong")
	role.Spec.Internal = true
	require.NoError(t, k8sClient.Update(ctx, role))
	_, _ = environment.Reconciler.Reconcile(ctx, req)

	require.NoError(t, k8sClient.Get(ctx, key, role))
	synced := apimeta.FindStatusCondition(role.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
	require.NotNil(t, synced)
	require.Equal(t, metav1.ConditionFalse, synced.Status,
		"the interrupted reconcile must report not-synced; Synced=True here means the corrupted credentials never bit and no rename was interrupted")
	require.Equal(t, "interrupted-role", role.Status.EffectiveRoleName,
		"an interrupted rename must keep reporting the previous effective name")
	require.Equal(t, "__interrupted-role", role.Status.PendingEffectiveRoleName,
		"the in-flight target must be recorded for later cleanup")
	require.True(t, hasRole("interrupted-role"), "old role must survive until cleanup succeeds")
	require.True(t, hasRole("__interrupted-role"), "interruption must land after the new role is created")

	// Change principals between attempts: the retry finds the new role
	// already created and must update its membership rather than skip it.
	role.Spec.Principals = []string{"User:user2"}
	require.NoError(t, k8sClient.Update(ctx, role))

	setPassword("password")
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, key, role))
	require.Equal(t, "__interrupted-role", role.Status.EffectiveRoleName)
	require.Empty(t, role.Status.PendingEffectiveRoleName)
	require.False(t, hasRole("interrupted-role"), "old role must be deleted once the rename resumes")
	require.Equal(t, []string{"User:user2"}, principals("__interrupted-role"),
		"principals changed between attempts must land on the already-created role")

	// The completed rename must quiesce: another pass writes nothing.
	resourceVersion := role.ResourceVersion
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, k8sClient.Get(ctx, key, role))
	require.Equal(t, resourceVersion, role.ResourceVersion,
		"reconciling a completed rename must not write")

	// Revert mid-rename: interrupt a rename back to the plain name, then flip
	// the spec back before the retry lands. Status matches spec again, so
	// only PendingEffectiveRoleName still knows about the half-created role —
	// it must be cleaned up, not orphaned.
	setPassword("wrong")
	role.Spec.Internal = false
	require.NoError(t, k8sClient.Update(ctx, role))
	_, _ = environment.Reconciler.Reconcile(ctx, req)

	require.NoError(t, k8sClient.Get(ctx, key, role))
	require.Equal(t, "__interrupted-role", role.Status.EffectiveRoleName)
	require.Equal(t, "interrupted-role", role.Status.PendingEffectiveRoleName)
	require.True(t, hasRole("interrupted-role"), "interruption must land after the new role is created")

	setPassword("password")
	role.Spec.Internal = true
	require.NoError(t, k8sClient.Update(ctx, role))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, key, role))
	require.Equal(t, "__interrupted-role", role.Status.EffectiveRoleName)
	require.Empty(t, role.Status.PendingEffectiveRoleName)
	require.False(t, hasRole("interrupted-role"),
		"the reverted rename's half-created role must be cleaned up")
	require.True(t, hasRole("__interrupted-role"))

	require.NoError(t, k8sClient.Delete(ctx, role))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, role)))
	require.False(t, hasRole("__interrupted-role"))
}
