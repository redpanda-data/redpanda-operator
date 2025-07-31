// Copyright 2025 Redpanda Data, Inc.
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
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

func userIsSuccessfullySynced(ctx context.Context, t framework.TestingT, user string) {
	var userObject redpandav1alpha2.User
	require.NoError(t, t.Get(ctx, t.ResourceKey(user), &userObject))

	// make sure the resource is stable
	checkStableResource(ctx, t, &userObject)

	// make sure it's synchronized
	t.RequireCondition(metav1.Condition{
		Type:   redpandav1alpha2.ResourceConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.ResourceConditionReasonSynced,
	}, userObject.Status.Conditions)
}

func iCreateCRDbasedUsers(ctx context.Context, t framework.TestingT, cluster string, users *godog.Table) {
	for _, user := range usersFromFullTable(t, cluster, users) {
		user := user

		t.Logf("Creating user %q", user.Name)
		require.NoError(t, t.Create(ctx, user))

		// make sure the resource is stable
		checkStableResource(ctx, t, user)

		// make sure it's synchronized
		t.RequireCondition(metav1.Condition{
			Type:   redpandav1alpha2.ResourceConditionTypeSynced,
			Status: metav1.ConditionTrue,
			Reason: redpandav1alpha2.ResourceConditionReasonSynced,
		}, user.Status.Conditions)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting user %q", user.Name)
			err := t.Get(ctx, t.ResourceKey(user.Name), user)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return
				}
				t.Fatalf("Error deleting user %q: %v", user.Name, err)
			}
			require.NoError(t, t.Delete(ctx, user))
		})
	}
}

func iUpdateCRDbasedUsers(ctx context.Context, t framework.TestingT, cluster string, users *godog.Table) {
	for _, user := range usersFromFullTable(t, cluster, users) {
		user := user

		var oldUser redpandav1alpha2.User
		require.NoError(t, t.Get(ctx, t.ResourceKey(user.Name), &oldUser))

		t.Logf("Updating user %q", user.Name)
		user.ObjectMeta.ResourceVersion = oldUser.ObjectMeta.ResourceVersion
		require.NoError(t, t.Update(ctx, user))

		// make sure the resource is stable
		checkStableResource(ctx, t, user)

		// make sure it's synchronized
		t.RequireCondition(metav1.Condition{
			Type:   redpandav1alpha2.ResourceConditionTypeSynced,
			Status: metav1.ConditionTrue,
			Reason: redpandav1alpha2.ResourceConditionReasonSynced,
		}, user.Status.Conditions)
	}
}

func iDeleteTheCRDUser(ctx context.Context, t framework.TestingT, user string) {
	var userObject redpandav1alpha2.User

	t.Logf("Deleting user %q", user)
	err := t.Get(ctx, t.ResourceKey(user), &userObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		t.Fatalf("Error deleting user %q: %v", user, err)
	}
	require.NoError(t, t.Delete(ctx, &userObject))
}

func thereAreAlreadyTheFollowingACLsInCluster(ctx context.Context, t framework.TestingT, cluster string, acls *godog.Table) {
	clients := clientsForCluster(ctx, cluster)
	aclClient := clients.ACLs(ctx)
	// throw this in a cleanup instead of a defer since we use it in a cleanup
	// below and it needs to stay alive until then
	t.Cleanup(func(_ context.Context) {
		aclClient.Close()
	})

	for _, user := range usersFromACLTable(t, cluster, acls) {
		user := user

		t.Logf("Creating acls in cluster %q for %q", cluster, user.Name)
		require.NoError(t, aclClient.Sync(ctx, user))

		// make sure they now exist
		rules, err := aclClient.ListACLs(ctx, user.GetPrincipal())
		require.NoError(t, err)
		require.NotEmpty(t, rules)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting acls in cluster %q for %q", cluster, user.Name)
			require.NoError(t, aclClient.DeleteAll(ctx, user))
		})
	}
}

func thereAreTheFollowingPreexistingUsersInCluster(ctx context.Context, t framework.TestingT, cluster string, users *godog.Table) {
	clients := clientsForCluster(ctx, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	// throw this in a cleanup instead of a defer since we use it in a cleanup
	// below and it needs to stay alive until then
	t.Cleanup(func(_ context.Context) {
		adminClient.Close()
	})

	usersClient := clients.Users(ctx)
	defer usersClient.Close()

	for _, user := range usersFromAuthTable(t, cluster, users) {
		user := user

		t.Logf("Creating user in cluster %q for %q", cluster, user.Name)
		require.NoError(t, adminClient.CreateUser(ctx, user.Name, user.Spec.Authentication.Password.Value, string(*user.Spec.Authentication.Type)))

		// make sure they now exist
		exists, err := usersClient.Has(ctx, user)
		require.NoError(t, err)
		require.True(t, exists, "User %q not found in cluster %q", user.Name, cluster)

		t.Cleanup(func(ctx context.Context) {
			t.Logf("Deleting user %q from cluster %q", user.Name, cluster)
			require.NoError(t, adminClient.DeleteUser(ctx, user.Name))
		})
	}
}

func shouldBeAbleToAuthenticateToTheClusterWithPasswordAndMechanism(ctx context.Context, t framework.TestingT, user, cluster, password, mechanism string) {
	clients := clientsForCluster(ctx, cluster).WithAuthentication(&client.UserAuth{
		Username:  user,
		Password:  password,
		Mechanism: mechanism,
	})
	users, err := clients.RedpandaAdmin(ctx).ListUsers(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, users)
}

func shouldExistAndBeAbleToAuthenticateToTheCluster(ctx context.Context, t framework.TestingT, user, cluster string) {
	clients := clientsForCluster(ctx, cluster)

	clients.ExpectUser(ctx, user)

	// Now we do the same check, but authenticated as the user

	var userObject redpandav1alpha2.User
	require.NoError(t, t.Get(ctx, t.ResourceKey(user), &userObject))

	clients.AsUser(ctx, &userObject).ExpectUser(ctx, user)
}

func thereShouldBeACLsInTheClusterForUser(ctx context.Context, t framework.TestingT, cluster, user string) {
	aclClient := clientsForCluster(ctx, cluster).ACLs(ctx)
	defer aclClient.Close()

	rules, err := aclClient.ListACLs(ctx, fmt.Sprintf("User:%s", user))
	require.NoError(t, err)
	require.NotEmpty(t, rules)
}

func thereIsNoUser(ctx context.Context, user, cluster string) {
	clientsForCluster(ctx, cluster).ExpectNoUser(ctx, user)
}

func iCreateSASLCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	key := t.ResourceKey(clusterName)
	image := &redpandav1alpha2.RedpandaImage{
		Tag:        ptr.To("dev"),
		Repository: ptr.To("localhost/redpanda-operator"),
	}

	require.NoError(t, t.Create(ctx, &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1),
					SideCars: &redpandav1alpha2.SideCars{
						Image: image,
						Controllers: &redpandav1alpha2.RPControllers{
							Image: image,
						},
					},
				},
			},
		},
	}))

	t.Cleanup(func(ctx context.Context) {
		t := framework.T(ctx)

		t.Log("cleaning up Redpanda cluster")
		require.NoError(t, t.Delete(ctx, &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}))

		var cluster redpandav1alpha2.Redpanda
		require.Eventually(t, func() bool {
			// this can take some time
			deleted := false
			if err := t.Get(ctx, key, &cluster); err != nil && apierrors.IsNotFound(err) {
				deleted = true
			}

			t.Logf("checking that Redpanda cluster %q is fully deleted: %v", clusterName, deleted)

			return deleted
		}, 2*time.Minute, 5*time.Second, `Cluster %q still exists`, clusterName)
	})
}

func iUpgradeCluster(ctx context.Context, t framework.TestingT, clusterName string) {
	var cluster redpandav1alpha2.Redpanda

	require.NoError(t, t.Get(ctx, t.ResourceKey(clusterName), &cluster))
	cluster.Spec.ClusterSpec.Image = &redpandav1alpha2.RedpandaImage{
		Repository: ptr.To("docker.redpanda.com/redpandadata/redpanda-unstable"),
		Tag:        ptr.To("v25.2.1-rc7"),
	}
	require.NoError(t, t.Update(ctx, &cluster))
}
