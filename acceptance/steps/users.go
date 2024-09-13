// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func isSuccessfullySynced(ctx context.Context, user string) error {
	t := framework.T(ctx)

	var userObject redpandav1alpha2.User
	require.NoError(t, t.Get(ctx, t.ResourceKey(user), &userObject))

	// make sure the resource is stable
	checkStableResource(ctx, t, &userObject)

	// make sure it's synchronized
	t.RequireCondition(metav1.Condition{
		Type:   redpandav1alpha2.UserConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.UserConditionReasonSynced,
	}, userObject.Status.Conditions)

	return nil
}

func iCreateCRDbasedUsers(ctx context.Context, cluster string, users *godog.Table) error {
	t := framework.T(ctx)

	for _, user := range usersFromFullTable(t, cluster, users) {
		user := user

		t.Logf("Creating user %q", user.Name)
		require.NoError(t, t.Create(ctx, user))

		// make sure the resource is stable
		checkStableResource(ctx, t, user)

		// make sure it's synchronized
		t.RequireCondition(metav1.Condition{
			Type:   redpandav1alpha2.UserConditionTypeSynced,
			Status: metav1.ConditionTrue,
			Reason: redpandav1alpha2.UserConditionReasonSynced,
		}, user.Status.Conditions)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting user %q", user.Name)
			err := t.Get(ctx, t.ResourceKey(user.Name), user)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return
				}
				t.Errorf("Error deleting user %q: %v", user.Name, err)
				return
			}
			require.NoError(t, t.Delete(ctx, user))
		})
	}

	return nil
}

func iDeleteTheCRDUser(ctx context.Context, user string) error {
	t := framework.T(ctx)

	var userObject redpandav1alpha2.User

	t.Logf("Deleting user %q", user)
	err := t.Get(ctx, t.ResourceKey(user), &userObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		t.Errorf("Error deleting user %q: %v", user, err)
		return nil
	}
	require.NoError(t, t.Delete(ctx, &userObject))

	return nil
}

func thereAreAlreadyTheFollowingACLsInCluster(ctx context.Context, cluster string, acls *godog.Table) error {
	t := framework.T(ctx)

	clients := clientsForCluster(t, ctx, cluster, nil)

	for _, user := range usersFromACLTable(t, cluster, acls) {
		user := user

		t.Logf("Creating acls in cluster %q for %q", cluster, user.Name)
		require.NoError(t, clients.ACLs.Sync(ctx, user))

		// make sure they now exist
		rules, err := clients.ACLs.ListACLs(ctx, user.GetPrincipal())
		require.NoError(t, err)
		require.NotEmpty(t, rules)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting acls in cluster %q for %q", cluster, user.Name)
			require.NoError(t, clients.ACLs.DeleteAll(ctx, user))
		})
	}

	return nil
}

func thereAreTheFollowingPreexistingUsersInCluster(ctx context.Context, cluster string, users *godog.Table) error {
	t := framework.T(ctx)

	clients := clientsForCluster(t, ctx, cluster, nil)

	for _, user := range usersFromAuthTable(t, cluster, users) {
		user := user

		t.Logf("Creating user in cluster %q for %q", cluster, user.Name)
		require.NoError(t, clients.RedpandaAdmin.CreateUser(ctx, user.Name, user.Spec.Authentication.Password.Value, string(*user.Spec.Authentication.Type)))

		// make sure they now exist
		exists, err := clients.Users.Has(ctx, user)
		require.NoError(t, err)
		require.True(t, exists, "User %q not found in cluster %q", user.Name, cluster)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting user %q from cluster %q", user.Name, cluster)
			require.NoError(t, clients.RedpandaAdmin.DeleteUser(ctx, user.Name))
		})
	}

	return nil
}

func shouldBeAbleToAuthenticateToTheClusterWithPasswordAndMechanism(ctx context.Context, user, cluster, password, mechanism string) error {
	t := framework.T(ctx)

	clients := clientsForCluster(t, ctx, cluster, nil).AsUser(ctx, user, password, mechanism)
	users, err := clients.RedpandaAdmin.ListUsers(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, users)

	return nil
}

func shouldExistAndBeAbleToAuthenticateToTheCluster(ctx context.Context, user, cluster string) error {
	t := framework.T(ctx)

	clientsForCluster(t, ctx, cluster, nil).ExpectUser(ctx, user)

	// Now we do the same check, but authenticated as the user

	var userObject redpandav1alpha2.User
	require.NoError(t, t.Get(ctx, t.ResourceKey(user), &userObject))

	clientsForCluster(t, ctx, cluster, &userObject).ExpectUser(ctx, user)

	return nil
}

func thereShouldBeACLsInTheClusterForUser(ctx context.Context, cluster, user string) error {
	t := framework.T(ctx)

	rules, err := clientsForCluster(t, ctx, cluster, nil).ACLs.ListACLs(ctx, fmt.Sprintf("User:%s", user))
	require.NoError(t, err)
	require.NotEmpty(t, rules)

	return nil
}

func thereIsNoUser(ctx context.Context, user, cluster string) error {
	t := framework.T(ctx)

	clientsForCluster(t, ctx, cluster, nil).ExpectNoUser(ctx, user)

	return nil
}
