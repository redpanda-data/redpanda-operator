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

		t.Run("InitiallyNotExists", func(t *testing.T) {
			has, err := rolesClient.Has(ctx, role)
			require.NoError(t, err)
			require.False(t, has)
		})

		t.Run("CreateRole", func(t *testing.T) {
			principals, err := rolesClient.Create(ctx, role)
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
			principals, err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:newuser1", "User:newuser2", "User:newuser3"}, principals)
		})

		t.Run("UpdateRemoveAllMembers", func(t *testing.T) {
			// Remove all principals - should clean up previously managed ones
			role.Spec.Principals = []string{}
			role.Status.Principals = []string{"User:newuser1", "User:newuser2", "User:newuser3"} // Previously synced
			principals, err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
			require.Empty(t, principals, "Should return empty slice when all principals removed")
			// Verify principals were actually removed from Redpanda
			members, err := rpadminClient.RoleMembers(ctx, roleName)
			require.NoError(t, err)
			require.Empty(t, members.Members, "All previously managed principals should be removed from Redpanda")
		})

		t.Run("RemoveManagedPrincipalsProtectsManual", func(t *testing.T) {
			// Update role to add operator-managed principals
			role.Spec.Principals = []string{"User:operator1", "User:operator2"}
			principals, err := rolesClient.Update(ctx, role)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"User:operator1", "User:operator2"}, principals)

			// Update status to reflect what we just synced (simulating controller behavior)
			role.Status.Principals = principals

			// Manually add a principal directly to Redpanda (outside operator control)
			_, err = rpadminClient.UpdateRoleMembership(
				ctx,
				roleName,
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
			principals, err = rolesClient.Update(ctx, role)
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
