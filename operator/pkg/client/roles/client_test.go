// Copyright 2026 Redpanda Data, Inc.
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

func getTestImage() string {
	containerRepo := os.Getenv("TEST_REDPANDA_REPO")
	containerTag := os.Getenv("TEST_REDPANDA_VERSION")
	return containerRepo + ":" + containerTag
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

	test := func(t *testing.T, container *redpanda.Container, opts ...Option) {
		admin, err := container.AdminAPIAddress(ctx)
		require.NoError(t, err)

		rpadminClient, err := rpadmin.NewAdminAPI([]string{admin}, &rpadmin.BasicAuth{
			Username: "admin",
			Password: "admin-password",
		}, nil)
		require.NoError(t, err)

		rolesClient, err := NewClient(ctx, rpadminClient, opts...)
		require.NoError(t, err)
		defer rolesClient.Close()

		// Group principals are only supported when the v2 SecurityService API is available.
		// This covers both explicit WithV2Disabled() and clusters too old to support it.
		supportsGroups := rolesClient.SupportsGroups()

		// Test role lifecycle
		roleName := "test-role-" + strconv.Itoa(int(time.Now().UnixNano()))
		role := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: roleName,
			},
			Spec: redpandav1alpha2.RoleSpec{
				Principals: []string{"User:testuser1", "User:testuser2"},
			},
		}

		t.Run("InitiallyNotExists", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.False(t, has)
		})

		t.Run("CreateRole", func(t *testing.T) {
			err := rolesClient.Create(ctx, role)
			require.NoError(t, err)
		})

		t.Run("ExistsAfterCreate", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.True(t, has)
		})

		t.Run("UpdateRoleMembership", func(t *testing.T) {
			// Update principals
			role.Spec.Principals = []string{"User:newuser1", "User:newuser2", "User:newuser3"}
			err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
		})

		t.Run("UpdateRemoveAllMembers", func(t *testing.T) {
			// Remove all principals
			role.Spec.Principals = []string{}
			err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
		})

		t.Run("UpdateWithGroupPrincipals", func(t *testing.T) {
			if !supportsGroups {
				t.Skip("Skipping: v2 SecurityService API not available, Group principals not supported")
			}
			// Add group principals alongside user principals
			role.Spec.Principals = []string{"User:testuser1", "Group:engineering"}
			err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
		})

		t.Run("UpdateGroupOnlyPrincipals", func(t *testing.T) {
			if !supportsGroups {
				t.Skip("Skipping: v2 SecurityService API not available, Group principals not supported")
			}
			// Replace all with group-only principals
			role.Spec.Principals = []string{"Group:engineering", "Group:platform"}
			err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
		})

		t.Run("UpdateAddMembersAgain", func(t *testing.T) {
			// Add principals back
			role.Spec.Principals = []string{"User:finaluser"}
			err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
		})

		t.Run("DeleteRole", func(t *testing.T) {
			err := rolesClient.Delete(ctx, role)
			require.NoError(t, err)
		})

		t.Run("NotExistsAfterDelete", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.False(t, has)
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

		t.Run("v2", func(t *testing.T) {
			test(t, container)
		})

		t.Run("v1", func(t *testing.T) {
			test(t, container, WithV2Disabled())
		})
	})

	t.Run("v24.1.1 release", func(t *testing.T) {
		container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.1.1",
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
		container, err := redpanda.Run(ctx, "redpandadata/redpanda:v25.2.1",
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

func TestMembersToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		members  []rpadmin.RoleMember
		expected []string
	}{
		{
			name:     "empty members",
			members:  []rpadmin.RoleMember{},
			expected: []string{},
		},
		{
			name: "user members only",
			members: []rpadmin.RoleMember{
				{Name: "alice", PrincipalType: "User"},
				{Name: "bob", PrincipalType: "User"},
			},
			expected: []string{"User:alice", "User:bob"},
		},
		{
			name: "group members only",
			members: []rpadmin.RoleMember{
				{Name: "engineering", PrincipalType: "Group"},
				{Name: "platform", PrincipalType: "Group"},
			},
			expected: []string{"Group:engineering", "Group:platform"},
		},
		{
			name: "mixed user and group members",
			members: []rpadmin.RoleMember{
				{Name: "alice", PrincipalType: "User"},
				{Name: "engineering", PrincipalType: "Group"},
				{Name: "bob", PrincipalType: "User"},
			},
			expected: []string{"User:alice", "Group:engineering", "User:bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := membersToStringSlice(tt.members)
			require.Equal(t, tt.expected, result)
		})
	}
}
