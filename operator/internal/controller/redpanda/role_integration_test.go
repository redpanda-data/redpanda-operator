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
	"fmt"
	"strconv"
	"strings"
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

func TestRoleIntegration(t *testing.T) { // nolint:funlen // Comprehensive integration test
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Test multiple role configurations for comprehensive coverage
	testCases := []struct {
		name         string
		roleSpec     redpandav1alpha2.RoleSpec
		expectedACLs int
		description  string
	}{
		{
			name: "single-topic-role",
			roleSpec: redpandav1alpha2.RoleSpec{
				ClusterSource: environment.ClusterSourceValid,
				Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "test-topic",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
							redpandav1alpha2.ACLOperationWrite,
						},
					}},
				},
			},
			expectedACLs: 2, // Read + Write = 2 ACL entries
			description:  "Role with single topic ACL",
		},
		{
			name: "multi-resource-role",
			roleSpec: redpandav1alpha2.RoleSpec{
				ClusterSource: environment.ClusterSourceValid,
				Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeTopic,
								Name: "topic1",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeGroup,
								Name: "group1",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
					},
				},
			},
			expectedACLs: 2,
			description:  "Role with multiple resource ACLs",
		},
		{
			name: "complex-permissions-role",
			roleSpec: redpandav1alpha2.RoleSpec{
				ClusterSource: environment.ClusterSourceValid,
				Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "admin-topic",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
							redpandav1alpha2.ACLOperationWrite,
							redpandav1alpha2.ACLOperationCreate,
							redpandav1alpha2.ACLOperationDelete,
							redpandav1alpha2.ACLOperationDescribe,
						},
					}},
				},
			},
			expectedACLs: 5, // Read + Write + Create + Delete + Describe = 5 ACL entries
			description:  "Role with comprehensive topic permissions",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := &redpandav1alpha2.Role{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      fmt.Sprintf("role-%s-%d", tc.name, time.Now().UnixNano()),
				},
				Spec: tc.roleSpec,
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			// Create the role
			require.NoError(t, environment.Factory.Create(ctx, role),
				"Failed to create role: %s", tc.description)

			// Initial reconciliation
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err, "Failed initial reconciliation for: %s", tc.description)

			// Verify role is properly configured
			require.NoError(t, environment.Factory.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status,
				"Role should be synced for: %s", tc.description)
			require.True(t, role.Status.ManagedACLs,
				"Role should have managed ACLs for: %s", tc.description)

			// Verify ACLs are created in Redpanda
			syncer, err := environment.Factory.ACLs(ctx, role)
			require.NoError(t, err)
			defer syncer.Close()

			acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, tc.expectedACLs,
				"Unexpected number of ACLs for: %s", tc.description)

			// Test principal generation
			expectedPrincipal := "RedpandaRole:" + role.Name
			require.Equal(t, expectedPrincipal, role.GetPrincipal(),
				"Principal should match expected format for: %s", tc.description)

			// Clean up
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))

			// Verify ACLs are cleaned up
			acls, err = syncer.ListACLs(ctx, role.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, 0,
				"ACLs should be cleaned up after deletion for: %s", tc.description)
		})
	}
}

func TestRoleLifecycleManagement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	role := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "lifecycle-test-role-" + strconv.Itoa(int(time.Now().UnixNano())),
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
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
			},
		},
	}

	key := client.ObjectKeyFromObject(role)
	req := ctrl.Request{NamespacedName: key}

	// Phase 1: Create role with ACLs
	t.Run("create_with_acls", func(t *testing.T) {
		require.NoError(t, environment.Factory.Create(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.Status.ManagedACLs)
		require.True(t, role.ShouldManageACLs())

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1)
	})

	// Phase 2: Update role to add more ACLs
	t.Run("update_add_acls", func(t *testing.T) {
		// Get fresh state before update
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Authorization.ACLs = append(role.Spec.Authorization.ACLs,
			redpandav1alpha2.ACLRule{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeGroup,
					Name: "lifecycle-group",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			})

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 2, "Should have 2 ACLs after update")
	})

	// Phase 3: Remove ACL management
	t.Run("remove_acl_management", func(t *testing.T) {
		// Get fresh state before update
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Authorization = nil
		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.False(t, role.Status.ManagedACLs)
		require.False(t, role.ShouldManageACLs())

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0, "ACLs should be removed when management is disabled")
	})

	// Phase 4: Delete role
	t.Run("delete_role", func(t *testing.T) {
		require.NoError(t, environment.Factory.Delete(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
	})
}

func TestRoleACLValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	// Test various ACL configurations
	testCases := []struct {
		name     string
		aclRules []redpandav1alpha2.ACLRule
		valid    bool
	}{
		{
			name: "valid_topic_acl",
			aclRules: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTopic,
					Name: "valid-topic",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			}},
			valid: true,
		},
		{
			name: "valid_group_acl",
			aclRules: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeGroup,
					Name: "valid-group",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			}},
			valid: true,
		},
		{
			name: "valid_transactional_id_acl",
			aclRules: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTransactionalID,
					Name: "valid-txn-id",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationWrite,
				},
			}},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := &redpandav1alpha2.Role{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      fmt.Sprintf("acl-test-%s-%d", strings.ReplaceAll(tc.name, "_", "-"), time.Now().UnixNano()),
				},
				Spec: redpandav1alpha2.RoleSpec{
					ClusterSource: environment.ClusterSourceValid,
					Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
						ACLs: tc.aclRules,
					},
				},
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, environment.Factory.Create(ctx, role))
			_, err := environment.Reconciler.Reconcile(ctx, req)

			if tc.valid {
				require.NoError(t, err, "Valid ACL configuration should not error")

				require.NoError(t, environment.Factory.Get(ctx, key, role))
				require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status)

				syncer, err := environment.Factory.ACLs(ctx, role)
				require.NoError(t, err)
				defer syncer.Close()

				acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
				require.NoError(t, err)
				// Count total operations across all rules
				expectedACLCount := 0
				for _, rule := range tc.aclRules {
					expectedACLCount += len(rule.Operations)
				}
				require.Len(t, acls, expectedACLCount)
			} else {
				// For invalid configurations, we might still reconcile but with an error condition
				require.NoError(t, environment.Factory.Get(ctx, key, role))
				// Could check for specific error conditions here based on validation logic
			}

			// Clean up
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
		})
	}
}

func TestRolePrincipalsOnlyMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	testCases := []struct {
		name        string
		principals  []string
		description string
	}{
		{
			name:        "single-user-principal",
			principals:  []string{"User:john"},
			description: "Role with single user principal",
		},
		{
			name:        "multiple-user-principals",
			principals:  []string{"User:alice", "User:bob", "User:charlie"},
			description: "Role with multiple user principals",
		},
		{
			name:        "mixed-format-principals",
			principals:  []string{"User:explicit", "implicit"}, // implicit defaults to User:implicit
			description: "Role with mixed principal formats",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := &redpandav1alpha2.Role{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      fmt.Sprintf("principals-only-%s-%d", tc.name, time.Now().UnixNano()),
				},
				Spec: redpandav1alpha2.RoleSpec{
					ClusterSource: environment.ClusterSourceValid,
					Principals:    tc.principals,
					// No Authorization section - ACLs managed separately
				},
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			// Create the role
			require.NoError(t, environment.Factory.Create(ctx, role),
				"Failed to create role: %s", tc.description)

			// Initial reconciliation
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err, "Failed initial reconciliation for: %s", tc.description)

			// Verify role is properly configured
			require.NoError(t, environment.Factory.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status,
				"Role should be synced for: %s", tc.description)

			// Verify role has principals but doesn't manage ACLs
			require.Equal(t, tc.principals, role.Spec.Principals,
				"Principals should match for: %s", tc.description)
			require.Nil(t, role.Spec.Authorization,
				"Authorization should be nil for principals-only mode: %s", tc.description)
			require.False(t, role.ShouldManageACLs(),
				"Role should not manage ACLs in principals-only mode: %s", tc.description)
			require.False(t, role.Status.ManagedACLs,
				"Role should not have managed ACLs status for: %s", tc.description)

			// Verify no ACLs are created in Redpanda for this role principal
			syncer, err := environment.Factory.ACLs(ctx, role)
			require.NoError(t, err)
			defer syncer.Close()

			acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, 0,
				"No ACLs should be created for principals-only mode: %s", tc.description)

			// Test principal generation
			expectedPrincipal := "RedpandaRole:" + role.Name
			require.Equal(t, expectedPrincipal, role.GetPrincipal(),
				"Principal should match expected format for: %s", tc.description)

			// Clean up
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
		})
	}
}

func TestRoleCombinedMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	testCases := []struct {
		name         string
		principals   []string
		aclRules     []redpandav1alpha2.ACLRule
		expectedACLs int
		description  string
	}{
		{
			name:       "single-principal-single-acl",
			principals: []string{"User:developer"},
			aclRules: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTopic,
					Name: "dev-topic",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
					redpandav1alpha2.ACLOperationWrite,
				},
			}},
			expectedACLs: 2,
			description:  "Role with single principal and topic ACL",
		},
		{
			name:       "multiple-principals-multiple-acls",
			principals: []string{"User:team-lead", "User:senior-dev", "junior-dev"}, // mixed format
			aclRules: []redpandav1alpha2.ACLRule{
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
			expectedACLs: 3, // 2 topic operations + 1 group operation
			description:  "Role with multiple principals and ACLs",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := &redpandav1alpha2.Role{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      fmt.Sprintf("combined-%s-%d", tc.name, time.Now().UnixNano()),
				},
				Spec: redpandav1alpha2.RoleSpec{
					ClusterSource: environment.ClusterSourceValid,
					Principals:    tc.principals,
					Authorization: &redpandav1alpha2.RoleAuthorizationSpec{
						ACLs: tc.aclRules,
					},
				},
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			// Create the role
			require.NoError(t, environment.Factory.Create(ctx, role),
				"Failed to create role: %s", tc.description)

			// Initial reconciliation
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err, "Failed initial reconciliation for: %s", tc.description)

			// Verify role is properly configured
			require.NoError(t, environment.Factory.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, role.Status.Conditions[0].Status,
				"Role should be synced for: %s", tc.description)

			// Verify role has both principals and manages ACLs
			require.Equal(t, tc.principals, role.Spec.Principals,
				"Principals should match for: %s", tc.description)
			require.NotNil(t, role.Spec.Authorization,
				"Authorization should be present for combined mode: %s", tc.description)
			require.True(t, role.ShouldManageACLs(),
				"Role should manage ACLs in combined mode: %s", tc.description)
			require.True(t, role.Status.ManagedACLs,
				"Role should have managed ACLs status for: %s", tc.description)

			// Verify ACLs are created in Redpanda
			syncer, err := environment.Factory.ACLs(ctx, role)
			require.NoError(t, err)
			defer syncer.Close()

			acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, tc.expectedACLs,
				"Unexpected number of ACLs for: %s", tc.description)

			// Test principal generation
			expectedPrincipal := "RedpandaRole:" + role.Name
			require.Equal(t, expectedPrincipal, role.GetPrincipal(),
				"Principal should match expected format for: %s", tc.description)

			// Verify ACL retrieval
			roleACLs := role.GetACLs()
			require.Len(t, roleACLs, len(tc.aclRules),
				"Role should return correct ACL rules: %s", tc.description)

			// Clean up
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))

			// Verify ACLs are cleaned up
			acls, err = syncer.ListACLs(ctx, role.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, 0,
				"ACLs should be cleaned up after deletion for: %s", tc.description)
		})
	}
}

func TestRoleModeTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	role := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      fmt.Sprintf("mode-transitions-%d", time.Now().UnixNano()),
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Principals:    []string{"User:transition-user"},
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
		require.False(t, role.ShouldManageACLs())
		require.False(t, role.Status.ManagedACLs)
		require.Equal(t, []string{"User:transition-user"}, role.Spec.Principals)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0, "No ACLs in principals-only mode")
	})

	// Phase 2: Transition to combined mode by adding authorization
	t.Run("transition_to_combined_mode", func(t *testing.T) {
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Authorization = &redpandav1alpha2.RoleAuthorizationSpec{
			ACLs: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTopic,
					Name: "transition-topic",
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
		require.True(t, role.ShouldManageACLs())
		require.True(t, role.Status.ManagedACLs)
		require.Equal(t, []string{"User:transition-user"}, role.Spec.Principals)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1, "Should have 1 ACL in combined mode")
	})

	// Phase 3: Update principals while in combined mode
	t.Run("update_principals_in_combined_mode", func(t *testing.T) {
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Principals = []string{"User:transition-user", "User:additional-user"}

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.True(t, role.ShouldManageACLs())
		require.True(t, role.Status.ManagedACLs)
		require.Equal(t, []string{"User:transition-user", "User:additional-user"}, role.Spec.Principals)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1, "ACLs should remain after principal update")
	})

	// Phase 4: Transition back to principals-only mode
	t.Run("transition_back_to_principals_only", func(t *testing.T) {
		require.NoError(t, environment.Factory.Get(ctx, key, role))
		role.Spec.Authorization = nil

		require.NoError(t, environment.Factory.Update(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Get(ctx, key, role))
		require.False(t, role.ShouldManageACLs())
		require.False(t, role.Status.ManagedACLs)
		require.Equal(t, []string{"User:transition-user", "User:additional-user"}, role.Spec.Principals)

		syncer, err := environment.Factory.ACLs(ctx, role)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0, "ACLs should be removed when transitioning back")
	})

	// Phase 5: Clean up
	t.Run("cleanup", func(t *testing.T) {
		require.NoError(t, environment.Factory.Delete(ctx, role))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
	})
}
