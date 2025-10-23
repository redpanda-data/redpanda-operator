// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package roles

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

const redpandaTestContainerImage = "docker.redpanda.com/redpandadata/redpanda:"

func getTestImage() string {
	containerTag := os.Getenv("TEST_REDPANDA_VERSION")
	return redpandaTestContainerImage + containerTag
}

func TestClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = redpandav1alpha2.Install(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	test := func(t *testing.T, container *redpanda.Container) {
		admin, err := container.AdminAPIAddress(ctx)
		require.NoError(t, err)

		rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
			Username: "admin",
			Password: "admin-password",
		}, nil)
		require.NoError(t, err)

		rolesClient, err := NewClient(ctx, rpadminClient)
		require.NoError(t, err)
		defer rolesClient.Close()

		// Test role lifecycle
		roleName := "test-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:testuser1", "User:testuser2"},
			},
		}

		emptyBindings := []*redpandav1alpha2.RedpandaRoleBinding{}

		t.Run("InitiallyNotExists", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.False(t, has)
		})

		t.Run("CreateRole", func(t *testing.T) {
			principals, err := rolesClient.Create(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:testuser1", "User:testuser2"}, principals)
		})

		t.Run("ExistsAfterCreate", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.True(t, has)
		})

		t.Run("UpdateRoleMembership", func(t *testing.T) {
			// Update principals and set status to simulate what controller would do
			role.Spec.Principals = []string{"User:newuser1", "User:newuser2", "User:newuser3"}
			role.Status.Principals = []string{"User:testuser1", "User:testuser2"} // Previously synced
			principals, err := rolesClient.Update(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:newuser1", "User:newuser2", "User:newuser3"}, principals)
		})

		t.Run("UpdateRemoveAllMembers", func(t *testing.T) {
			// Remove all principals - should clean up previously managed ones
			role.Spec.Principals = []string{}
			role.Status.Principals = []string{"User:newuser1", "User:newuser2", "User:newuser3"} // Previously synced
			principals, err := rolesClient.Update(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.Empty(t, principals, "Should return empty slice when all principals removed")

			// Verify principals were actually removed from Redpanda
			members, err := rpadminClient.RoleMembers(ctx, roleName)
			require.NoError(t, err)
			require.Empty(t, members.Members, "All previously managed principals should be removed from Redpanda")
		})

		t.Run("UpdateAddMembersAgain", func(t *testing.T) {
			// Add principals back after manual management mode
			role.Spec.Principals = []string{"User:finaluser"}
			role.Status.Principals = []string{} // Was in manual mode, so empty
			principals, err := rolesClient.Update(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:finaluser"}, principals)
		})

		t.Run("RemoveManagedPrincipalsProtectsManual", func(t *testing.T) {
			// Update role to add operator-managed principals
			role.Spec.Principals = []string{"User:operator1", "User:operator2"}
			role.Status.Principals = []string{"User:finaluser"} // Previously synced
			principals, err := rolesClient.Update(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:operator1", "User:operator2"}, principals)

			// Update status to reflect what we just synced (simulating controller behavior)
			role.Status.Principals = principals

			// Manually add a principal directly to Redpanda (outside operator control)
			_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
				[]rpadmin.RoleMember{{Name: "manual-user", PrincipalType: "User"}},
				[]rpadmin.RoleMember{},
				false,
			)
			require.NoError(t, err)

			// Verify all 3 principals exist in Redpanda
			members, err := rpadminClient.RoleMembers(ctx, roleName)
			require.NoError(t, err)
			require.Len(t, members.Members, 3, "Should have operator1, operator2, and manual-user")

			// Remove all operator-managed principals (transition to manual mode)
			role.Spec.Principals = []string{}
			// Status still tracks what we previously managed
			role.Status.Principals = []string{"User:operator1", "User:operator2"}

			// Update should remove only the managed principals
			principals, err = rolesClient.Update(ctx, role, emptyBindings)
			require.NoError(t, err)
			require.Empty(t, principals, "Should return empty when entering manual mode")

			// Verify only manual-user remains, operator principals removed
			members, err = rpadminClient.RoleMembers(ctx, roleName)
			require.NoError(t, err)
			require.Len(t, members.Members, 1, "Only manual-user should remain")
			require.Equal(t, "manual-user", members.Members[0].Name, "Manual principal should be protected")
		})

		t.Run("DeleteRole", func(t *testing.T) {
			err := rolesClient.Delete(ctx, role)
			require.NoError(t, err)
		})

		t.Run("NotExistsAfterDelete", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.False(t, has)

			// Verify directly with admin API that the role doesn't exist in Redpanda
			_, err = rpadminClient.Role(ctx, roleName)
			require.Error(t, err, "Role should not exist in Redpanda")
			// Verify it's a 404 error (role not found)
			var httpErr *rpadmin.HTTPResponseError
			require.True(t, errors.As(err, &httpErr), "Error should be HTTPResponseError")
			require.Equal(t, http.StatusNotFound, httpErr.Response.StatusCode, "Should be 404 Not Found")
		})

		t.Run("DeleteNonExistentRole", func(t *testing.T) {
			// Should not error when deleting non-existent role
			err := rolesClient.Delete(ctx, role)
			require.NoError(t, err)
		})
	}

	t.Run("default test image", func(t *testing.T) {
		container, err := redpanda.Run(ctx, getTestImage(),
			redpanda.WithEnableKafkaAuthorization(),
			redpanda.WithEnableSASL(),
			redpanda.WithSuperusers("admin"),
			redpanda.WithNewServiceAccount("admin", "admin-password"),
		)

		require.NoError(t, err)
		defer func() {
			require.NoError(t, container.Terminate(ctx))
		}()

		test(t, container)
	})

	t.Run("v24.1.1 release", func(t *testing.T) {
		container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v24.1.1",
			redpanda.WithEnableKafkaAuthorization(),
			redpanda.WithEnableSASL(),
			redpanda.WithSuperusers("admin"),
			redpanda.WithNewServiceAccount("admin", "admin-password"),
		)

		require.NoError(t, err)
		defer func() {
			require.NoError(t, container.Terminate(ctx))
		}()

		test(t, container)
	})

	t.Run("v25.2.1 latest release", func(t *testing.T) {
		container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v25.2.1",
			redpanda.WithEnableKafkaAuthorization(),
			redpanda.WithEnableSASL(),
			redpanda.WithSuperusers("admin"),
			redpanda.WithNewServiceAccount("admin", "admin-password"),
		)

		require.NoError(t, err)
		defer func() {
			require.NoError(t, container.Terminate(ctx))
		}()

		test(t, container)
	})
}

// Test aggregatePrincipals function
func TestAggregatePrincipals(t *testing.T) {
	tests := []struct {
		name         string
		role         *redpandav1alpha2.RedpandaRole
		roleBindings []*redpandav1alpha2.RedpandaRoleBinding
		expected     []string
		expectedLen  int // 0 means don't check length (use ElementsMatch only)
	}{
		{
			name: "Role with only inline principals",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{"User:alice", "User:bob"},
				},
			},
			roleBindings: nil,
			expected:     []string{"User:alice", "User:bob"},
		},
		{
			name: "Role with RoleBinding",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{"User:alice"},
				},
			},
			roleBindings: []*redpandav1alpha2.RedpandaRoleBinding{
				{
					Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
						Principals: []string{"User:charlie", "User:dave"},
					},
				},
			},
			expected: []string{"User:alice", "User:charlie", "User:dave"},
		},
		{
			name: "Role with multiple RoleBindings",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{"User:alice"},
				},
			},
			roleBindings: []*redpandav1alpha2.RedpandaRoleBinding{
				{
					Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
						Principals: []string{"User:bob"},
					},
				},
				{
					Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
						Principals: []string{"User:charlie", "User:dave"},
					},
				},
			},
			expected: []string{"User:alice", "User:bob", "User:charlie", "User:dave"},
		},
		{
			name: "Deduplication of principals",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{"User:alice", "User:bob"},
				},
			},
			roleBindings: []*redpandav1alpha2.RedpandaRoleBinding{
				{
					Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
						Principals: []string{"User:bob", "User:charlie"},
					},
				},
			},
			expected:    []string{"User:alice", "User:bob", "User:charlie"},
			expectedLen: 3, // Verify no duplicates
		},
		{
			name: "Role with no principals and no bindings",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{},
				},
			},
			roleBindings: nil,
			expected:     []string{},
		},
		{
			name: "Normalization: alice and User:alice treated as same principal",
			role: &redpandav1alpha2.RedpandaRole{
				Spec: redpandav1alpha2.RoleSpec{
					Principals: []string{"alice", "User:alice", "User:bob"},
				},
			},
			roleBindings: nil,
			expected:     []string{"User:alice", "User:bob"},
			expectedLen:  2, // Verify alice only appears once (normalized)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principals := aggregatePrincipals(tt.role, tt.roleBindings)
			require.ElementsMatch(t, tt.expected, principals)
			if tt.expectedLen > 0 {
				require.Len(t, principals, tt.expectedLen, "Verify exact length (e.g., no duplicates)")
			}
		})
	}
}

// TestManualMembershipManagement tests that roles without principals can be managed manually
func TestManualMembershipManagement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = redpandav1alpha2.Install(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	container, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("admin"),
		redpanda.WithNewServiceAccount("admin", "admin-password"),
	)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, container.Terminate(ctx))
	}()

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
		Username: "admin",
		Password: "admin-password",
	}, nil)
	require.NoError(t, err)

	rolesClient, err := NewClient(ctx, rpadminClient)
	require.NoError(t, err)
	defer rolesClient.Close()

	emptyBindings := []*redpandav1alpha2.RedpandaRoleBinding{}

	t.Run("Authorization-only role allows manual membership", func(t *testing.T) {
		// Create a role with NO principals (authorization-only)
		roleName := "authz-only-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{}, // Empty - no managed principals
			},
		}

		// Create the role (with no principals or bindings)
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.Empty(t, principals, "Should return empty when no principals defined")

		// Manually add a principal via admin API
		_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
			[]rpadmin.RoleMember{{Name: "manual-user", PrincipalType: "User"}},
			[]rpadmin.RoleMember{},
			false,
		)
		require.NoError(t, err)

		// Verify manual principal was added
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1)
		require.Equal(t, "manual-user", members.Members[0].Name)

		// Call Update() with no principals/bindings - should NOT remove the manual principal
		role.Status.Principals = []string{} // No previously managed principals
		principals, err = rolesClient.Update(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.Empty(t, principals, "Should return empty in manual mode")

		// Verify manual principal still exists
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1, "Manual principal should be preserved")
		require.Equal(t, "manual-user", members.Members[0].Name)

		// Cleanup
		err = rolesClient.Delete(ctx, role)
		require.NoError(t, err)
	})

	t.Run("Role with principals protects manual members", func(t *testing.T) {
		// Create a role with managed principals
		roleName := "managed-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:managed-user"},
			},
		}

		// Create the role
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:managed-user"}, principals)

		// Manually add another principal outside the operator
		_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
			[]rpadmin.RoleMember{{Name: "manual-user", PrincipalType: "User"}},
			[]rpadmin.RoleMember{},
			false,
		)
		require.NoError(t, err)

		// Verify both principals exist
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2)

		// Call Update() with status set to what we previously synced
		role.Status.Principals = []string{"User:managed-user"} // Only track what we synced
		principals, err = rolesClient.Update(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:managed-user"}, principals)

		// Verify manual principal is PROTECTED (not removed) since it's not in status.principals
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2, "Manual principal should be protected")

		// Verify both users are present
		memberNames := []string{members.Members[0].Name, members.Members[1].Name}
		require.ElementsMatch(t, []string{"managed-user", "manual-user"}, memberNames)

		// Cleanup
		err = rolesClient.Delete(ctx, role)
		require.NoError(t, err)
	})

	t.Run("Mixed principals with and without User prefix", func(t *testing.T) {
		// Test that both "User:username" and "username" formats work correctly
		roleName := "mixed-principals-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:prefixed-user", "unprefixed-user"},
			},
		}

		// Create the role
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:prefixed-user", "User:unprefixed-user"}, principals) // Normalized

		// Verify both principals were added correctly
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2)

		// Check that both usernames are present (without checking order)
		memberNames := make([]string, len(members.Members))
		for i, member := range members.Members {
			memberNames[i] = member.Name
			require.Equal(t, "User", member.PrincipalType)
		}
		require.ElementsMatch(t, []string{"prefixed-user", "unprefixed-user"}, memberNames)

		// Cleanup
		err = rolesClient.Delete(ctx, role)
		require.NoError(t, err)
	})

	t.Run("Principals with email-like special characters", func(t *testing.T) {
		// Test that principals with @ and . characters (like emails) work correctly
		roleName := "email-principals-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:alice@test.foo", "bob@example.com", "User:charlie@domain.co.uk"},
			},
		}

		// Create the role
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:alice@test.foo", "User:bob@example.com", "User:charlie@domain.co.uk"}, principals) // Normalized

		// Verify all principals were added correctly to Redpanda
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 3, "Should have exactly 3 members")

		// Extract member names and verify they match
		memberNames := make([]string, len(members.Members))
		for i, member := range members.Members {
			memberNames[i] = member.Name
			require.Equal(t, "User", member.PrincipalType, "All members should have User principal type")
		}
		require.ElementsMatch(t, []string{"alice@test.foo", "bob@example.com", "charlie@domain.co.uk"}, memberNames,
			"Redpanda should preserve email-like usernames with @ and . characters")

		// Update to test that special characters work in updates too
		role.Spec.Principals = []string{"User:new-user@test.org"}
		role.Status.Principals = []string{"User:alice@test.foo", "User:bob@example.com", "User:charlie@domain.co.uk"} // Normalized
		principals, err = rolesClient.Update(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:new-user@test.org"}, principals)

		// Verify only the new principal exists (old ones removed)
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1, "Should have exactly 1 member after update")
		require.Equal(t, "new-user@test.org", members.Members[0].Name)

		// Cleanup
		err = rolesClient.Delete(ctx, role)
		require.NoError(t, err)
	})

	t.Run("Role with RoleBinding manages principals", func(t *testing.T) {
		// Create a role with NO inline principals
		roleName := "role-with-binding-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{}, // No inline principals
			},
		}

		// Create the role (with no principals)
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.Empty(t, principals, "Should return empty when no principals")

		// Create a RoleBinding that references this role
		rb := &redpandav1alpha2.RedpandaRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding-for-" + roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
				RoleRef: redpandav1alpha2.RoleRef{
					Name: roleName,
				},
				Principals: []string{"User:binding-user"},
			},
		}
		require.NoError(t, c.Create(ctx, rb))

		// Manually add a principal
		_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
			[]rpadmin.RoleMember{{Name: "manual-user", PrincipalType: "User"}},
			[]rpadmin.RoleMember{},
			false,
		)
		require.NoError(t, err)

		// Call Update() with RoleBinding - should sync to RoleBinding principals
		// Note: manual principal is protected because it's not in status.principals
		role.Status.Principals = []string{} // No previously managed principals
		roleBindings := []*redpandav1alpha2.RedpandaRoleBinding{rb}
		principals, err = rolesClient.Update(ctx, role, roleBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:binding-user"}, principals)

		// Verify both RoleBinding principal and manual principal exist (manual is protected)
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2, "Both RoleBinding and manual principals should exist")
		memberNames := []string{members.Members[0].Name, members.Members[1].Name}
		require.ElementsMatch(t, []string{"binding-user", "manual-user"}, memberNames)

		// Cleanup
		require.NoError(t, c.Delete(ctx, rb))
		err = rolesClient.Delete(ctx, role)
		require.NoError(t, err)
	})
}

// Test parsePrincipal function
func TestParsePrincipal(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType string
		expectedName string
	}{
		{
			name:         "principal with User prefix",
			input:        "User:alice",
			expectedType: "User",
			expectedName: "alice",
		},
		{
			name:         "principal without prefix defaults to User",
			input:        "alice",
			expectedType: "User",
			expectedName: "alice",
		},
		{
			name:         "principal with User and special chars in name",
			input:        "User:alice.smith@example.com",
			expectedType: "User",
			expectedName: "alice.smith@example.com",
		},
		{
			name:         "principal without prefix with special chars",
			input:        "alice.smith@example.com",
			expectedType: "User",
			expectedName: "alice.smith@example.com",
		},
		{
			name:         "principal with User and dash in name",
			input:        "User:alice-smith",
			expectedType: "User",
			expectedName: "alice-smith",
		},
		{
			name:         "principal without prefix with dash",
			input:        "alice-smith",
			expectedType: "User",
			expectedName: "alice-smith",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principalType, name := parsePrincipal(tt.input)
			require.Equal(t, tt.expectedType, principalType, "principal type should match")
			require.Equal(t, tt.expectedName, name, "principal name should match")
		})
	}
}

// TestStatusPrincipalsTracking tests that the returned principals match what was synced
// and that status-driven protection works correctly
func TestStatusPrincipalsTracking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = redpandav1alpha2.Install(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	container, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("admin"),
		redpanda.WithNewServiceAccount("admin", "admin-password"),
	)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, container.Terminate(ctx))
	}()

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
		Username: "admin",
		Password: "admin-password",
	}, nil)
	require.NoError(t, err)

	rolesClient, err := NewClient(ctx, rpadminClient)
	require.NoError(t, err)
	defer rolesClient.Close()

	emptyBindings := []*redpandav1alpha2.RedpandaRoleBinding{}

	t.Run("Create returns synced principals", func(t *testing.T) {
		roleName := "create-tracking-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:alice", "User:bob"},
			},
		}

		// Create should return the principals that were synced
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:alice", "User:bob"}, principals)

		// Verify they actually exist in Redpanda with correct names
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2, "Should have exactly 2 members in Redpanda")

		// Extract actual member names from Redpanda
		actualMembers := make([]string, len(members.Members))
		for i, member := range members.Members {
			actualMembers[i] = member.Name
			require.Equal(t, "User", member.PrincipalType, "All members should have User principal type")
		}
		require.ElementsMatch(t, []string{"alice", "bob"}, actualMembers, "Redpanda should have alice and bob")

		// Cleanup
		require.NoError(t, rolesClient.Delete(ctx, role))
	})

	t.Run("Update removes only previously managed principals", func(t *testing.T) {
		roleName := "update-tracking-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:alice", "User:bob"},
			},
		}

		// Create with initial principals
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:alice", "User:bob"}, principals)

		// Simulate what controller would do - store synced principals in status
		role.Status.Principals = principals

		// Manually add a principal outside the operator
		_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
			[]rpadmin.RoleMember{{Name: "manual-charlie", PrincipalType: "User"}},
			[]rpadmin.RoleMember{},
			false,
		)
		require.NoError(t, err)

		// Verify all three exist
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 3)

		// Update to different principals - should only remove what we managed
		role.Spec.Principals = []string{"User:dave"}
		principals, err = rolesClient.Update(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:dave"}, principals)

		// Verify: alice and bob removed (were in status.principals),
		// manual-charlie preserved (not in status.principals),
		// dave added
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 2, "Should have dave and manual-charlie")
		memberNames := []string{members.Members[0].Name, members.Members[1].Name}
		require.ElementsMatch(t, []string{"dave", "manual-charlie"}, memberNames)

		// Cleanup
		require.NoError(t, rolesClient.Delete(ctx, role))
	})

	t.Run("Manual management mode returns empty principals", func(t *testing.T) {
		roleName := "manual-mode-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{}, // No principals - manual mode
			},
		}

		// Create with no principals
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.Empty(t, principals, "Should return empty in manual mode")

		// Verify role exists in Redpanda but has no members
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Empty(t, members.Members, "Redpanda role should have no members in manual mode")

		// Manually add a principal to simulate manual management
		_, err = rpadminClient.UpdateRoleMembership(ctx, roleName,
			[]rpadmin.RoleMember{{Name: "manual-admin", PrincipalType: "User"}},
			[]rpadmin.RoleMember{},
			false,
		)
		require.NoError(t, err)

		// Verify manual principal was added
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1)
		require.Equal(t, "manual-admin", members.Members[0].Name)

		// Update with no principals - should NOT touch the manual principal
		role.Status.Principals = []string{}
		principals, err = rolesClient.Update(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.Empty(t, principals, "Should return empty in manual mode")

		// Verify manual principal is still there (not removed)
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1, "Manual principal should not be removed in manual mode")
		require.Equal(t, "manual-admin", members.Members[0].Name)

		// Cleanup
		require.NoError(t, rolesClient.Delete(ctx, role))
	})

	t.Run("RoleBinding principals included in tracking", func(t *testing.T) {
		roleName := "binding-tracking-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:alice"}, // Inline principal
			},
		}

		// Create with inline principal
		principals, err := rolesClient.Create(ctx, role, emptyBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:alice"}, principals)

		// Verify alice exists in Redpanda
		members, err := rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 1)
		require.Equal(t, "alice", members.Members[0].Name)

		// Create a RoleBinding
		rb := &redpandav1alpha2.RedpandaRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding-" + roleName,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
				RoleRef: redpandav1alpha2.RoleRef{
					Name: roleName,
				},
				Principals: []string{"User:bob", "User:charlie"},
			},
		}
		require.NoError(t, c.Create(ctx, rb))

		// Update should aggregate principals from both Role and RoleBinding
		role.Status.Principals = []string{"User:alice"}
		roleBindings := []*redpandav1alpha2.RedpandaRoleBinding{rb}
		principals, err = rolesClient.Update(ctx, role, roleBindings)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"User:alice", "User:bob", "User:charlie"}, principals)

		// Verify all three exist in Redpanda with correct names
		members, err = rpadminClient.RoleMembers(ctx, roleName)
		require.NoError(t, err)
		require.Len(t, members.Members, 3, "Should have exactly 3 members in Redpanda")

		// Extract actual member names from Redpanda
		actualMembers := make([]string, len(members.Members))
		for i, member := range members.Members {
			actualMembers[i] = member.Name
			require.Equal(t, "User", member.PrincipalType, "All members should have User principal type")
		}
		require.ElementsMatch(t, []string{"alice", "bob", "charlie"}, actualMembers,
			"Redpanda should have alice, bob, and charlie")

		// Cleanup
		require.NoError(t, c.Delete(ctx, rb))
		require.NoError(t, rolesClient.Delete(ctx, role))
	})
}

// Test utility functions
func TestCalculateMembershipChanges(t *testing.T) {
	tests := []struct {
		name              string
		current           []string
		desired           []string
		previouslyManaged []string
		expectedAdd       []string
		expectedRemove    []string
	}{
		{
			name:              "no changes needed",
			current:           []string{"User:alice", "User:bob"},
			desired:           []string{"User:alice", "User:bob"},
			previouslyManaged: []string{"User:alice", "User:bob"},
			expectedAdd:       []string{},
			expectedRemove:    []string{},
		},
		{
			name:              "add new members",
			current:           []string{"User:alice"},
			desired:           []string{"User:alice", "User:bob", "User:charlie"},
			previouslyManaged: []string{"User:alice"},
			expectedAdd:       []string{"User:bob", "User:charlie"},
			expectedRemove:    []string{},
		},
		{
			name:              "remove previously managed members",
			current:           []string{"User:alice", "User:bob", "User:charlie"},
			desired:           []string{"User:alice"},
			previouslyManaged: []string{"User:alice", "User:bob", "User:charlie"},
			expectedAdd:       []string{},
			expectedRemove:    []string{"User:bob", "User:charlie"},
		},
		{
			name:              "protect manual members (not in previouslyManaged)",
			current:           []string{"User:alice", "User:bob", "User:manual"},
			desired:           []string{"User:alice"},
			previouslyManaged: []string{"User:alice", "User:bob"}, // manual not in list
			expectedAdd:       []string{},
			expectedRemove:    []string{"User:bob"}, // Only remove bob, protect manual
		},
		{
			name:              "replace managed members",
			current:           []string{"User:alice", "User:bob"},
			desired:           []string{"User:charlie", "User:dave"},
			previouslyManaged: []string{"User:alice", "User:bob"},
			expectedAdd:       []string{"User:charlie", "User:dave"},
			expectedRemove:    []string{"User:alice", "User:bob"},
		},
		{
			name:              "empty to some",
			current:           []string{},
			desired:           []string{"User:alice"},
			previouslyManaged: []string{},
			expectedAdd:       []string{"User:alice"},
			expectedRemove:    []string{},
		},
		{
			name:              "some to empty - only remove managed",
			current:           []string{"User:alice", "User:manual"},
			desired:           []string{},
			previouslyManaged: []string{"User:alice"}, // manual was never managed
			expectedAdd:       []string{},
			expectedRemove:    []string{"User:alice"}, // Only remove managed, protect manual
		},
		{
			name:              "transition from manual to managed",
			current:           []string{"User:manual1", "User:manual2"},
			desired:           []string{"User:managed1"},
			previouslyManaged: []string{}, // No previously managed
			expectedAdd:       []string{"User:managed1"},
			expectedRemove:    []string{}, // Don't remove manual ones
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toAdd, toRemove := calculateMembershipChanges(tt.current, tt.desired, tt.previouslyManaged)

			require.ElementsMatch(t, tt.expectedAdd, toAdd, "toAdd should match expected")
			require.ElementsMatch(t, tt.expectedRemove, toRemove, "toRemove should match expected")
		})
	}
}
