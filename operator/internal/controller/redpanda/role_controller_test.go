// Copyright 2025 Redpanda Data, Inc.
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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRoleReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
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
			Namespace: metav1.NamespaceDefault,
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
	} {
		t.Run(name, func(t *testing.T) {
			role := baseRole.DeepCopy()
			role.Name = "role" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(role)
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, environment.Factory.Create(ctx, role))
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, environment.Factory.Get(ctx, key, role))
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
						require.NoError(t, environment.Factory.Update(ctx, role))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, environment.Factory.Get(ctx, key, role))
						require.True(t, role.Status.ManagedRole)
						require.ElementsMatch(t, []string{"User:newuser1", "User:newuser2"}, role.Status.Principals)
					}

					if role.ShouldManageACLs() {
						// now clear out any managed ACLs and re-check
						role.Spec.Authorization = nil
						require.NoError(t, environment.Factory.Update(ctx, role))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, environment.Factory.Get(ctx, key, role))
						require.False(t, role.Status.ManagedACLs)
					}

					// make sure we no longer have acls
					acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, environment.Factory.Delete(ctx, role))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))

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
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
		})
	}
}

func TestRolePrincipalsAndACLs(t *testing.T) { // nolint:funlen // Comprehensive test coverage
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Test different role configurations
	testCases := []struct {
		name             string
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
			role := &redpandav1alpha2.RedpandaRole{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-role-" + strconv.Itoa(int(time.Now().UnixNano())),
				},
				Spec: redpandav1alpha2.RoleSpec{
					ClusterSource: environment.ClusterSourceValid,
					Principals:    tt.principals,
					Authorization: tt.authorization,
				},
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			// Create and reconcile
			require.NoError(t, environment.Factory.Create(ctx, role))
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			// Verify status
			require.NoError(t, environment.Factory.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status)
			require.ElementsMatch(t, tt.principals, role.Status.Principals, tt.description)

			// Verify management flags
			require.Equal(t, tt.shouldManageRole, role.ShouldManageRole(), tt.description)
			require.Equal(t, tt.shouldManageACLs, role.ShouldManageACLs(), tt.description)
			require.Equal(t, tt.shouldManageRole, role.Status.ManagedRole, tt.description)
			require.Equal(t, tt.shouldManageACLs, role.Status.ManagedACLs, tt.description)

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
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
		})
	}
}

func TestRoleLifecycleTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	role := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "lifecycle-role-" + strconv.Itoa(int(time.Now().UnixNano())),
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:lifecycle-user"},
			// Start in principals-only mode
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := ctrl.Request{NamespacedName: key}

	// Phase 1: Create in principals-only mode
	t.Run("create_principals_only", func(t *testing.T) {
		require.NoError(t, environment.Factory.Create(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.False(t, role.ShouldManageACLs())
		require.True(t, role.Status.ManagedRole)
		require.False(t, role.Status.ManagedACLs)

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
		require.NoError(t, environment.Factory.Get(ctx, key, role))
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

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.True(t, role.ShouldManageACLs())
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedACLs)

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
		principals := []string{"User:lifecycle-user", "User:additional-user"}
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Principals = principals

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.Status.ManagedRole)
		require.True(t, role.Status.ManagedACLs)
		// spec shouldn't change during reconcile
		require.Equal(t, principals, role.Spec.Principals)
		// status should match updated principals
		require.ElementsMatch(t, principals, role.Status.Principals)

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
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Authorization = nil

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.ShouldManageRole())
		require.False(t, role.ShouldManageACLs())
		require.True(t, role.Status.ManagedRole)
		require.False(t, role.Status.ManagedACLs)

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
		require.NoError(t, environment.Factory.Delete(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
	})
}
