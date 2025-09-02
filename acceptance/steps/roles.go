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

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func roleIsSuccessfullySynced(ctx context.Context, t framework.TestingT, role string) {
	var roleObject redpandav1alpha2.Role
	require.NoError(t, t.Get(ctx, t.ResourceKey(role), &roleObject))

	// make sure the resource is stable
	checkStableResource(ctx, t, &roleObject)

	// make sure it's synchronized
	t.RequireCondition(metav1.Condition{
		Type:   redpandav1alpha2.ResourceConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.ResourceConditionReasonSynced,
	}, roleObject.Status.Conditions)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("Deleting role %q", role)
		err := t.Get(ctx, t.ResourceKey(role), &roleObject)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return
			}
			t.Fatalf("Error deleting role %q: %v", role, err)
		}
		require.NoError(t, t.Delete(ctx, &roleObject))
	})
}

func iDeleteTheCRDRole(ctx context.Context, t framework.TestingT, role string) {
	var roleObject redpandav1alpha2.Role

	t.Logf("Deleting role %q", role)
	err := t.Get(ctx, t.ResourceKey(role), &roleObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		t.Fatalf("Error deleting role %q: %v", role, err)
	}
	require.NoError(t, t.Delete(ctx, &roleObject))
}

func roleShouldExistInCluster(ctx context.Context, t framework.TestingT, role, cluster string) {
	clientsForCluster(ctx, cluster).ExpectRole(ctx, role)
}

func thereShouldBeNoRoleInCluster(ctx context.Context, t framework.TestingT, role, cluster string) {
	clientsForCluster(ctx, cluster).ExpectNoRole(ctx, role)
}

func roleShouldHaveMembersAndInCluster(ctx context.Context, t framework.TestingT, role, cluster string, members ...string) {
	clients := clientsForCluster(ctx, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Get role members from Redpanda
	membersResp, err := adminClient.RoleMembers(ctx, role)
	if err != nil {
		t.Fatalf("Failed to get members for role %q: %v", role, err)
	}

	// Check that expected members are present
	actualMembers := make(map[string]bool)
	for _, member := range membersResp.Members {
		actualMembers[member.Name] = true
	}

	// Verify all expected members are present
	for _, expectedMember := range members {
		require.True(t, actualMembers[expectedMember],
			"Expected member %q not found in role %q", expectedMember, role)
	}

	// Verify we have exactly the expected number of members
	require.Equal(t, len(members), len(actualMembers),
		"Role %q should have exactly %d members, got %d", role, len(members), len(actualMembers))
}

func roleShouldNotHaveMemberInCluster(ctx context.Context, t framework.TestingT, role, cluster, member string) {
	clients := clientsForCluster(ctx, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Get role members from Redpanda
	membersResp, err := adminClient.RoleMembers(ctx, role)
	if err != nil {
		t.Fatalf("Failed to get members for role %q: %v", role, err)
	}

	// Check that the member is not present
	for _, m := range membersResp.Members {
		require.NotEqual(t, member, m.Name,
			"Member %q should not be in role %q", member, role)
	}
}

func roleShouldHaveACLsForTopicPatternInCluster(ctx context.Context, t framework.TestingT, role, cluster, pattern string) {
	clients := clientsForCluster(ctx, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.NotEmpty(t, rules, "Role %q should have ACLs", role)

	// Check for topic pattern ACL
	found := false
	for _, rule := range rules {
		if rule.Resource.Type == redpandav1alpha2.ResourceTypeTopic &&
			rule.Resource.Name == pattern &&
			ptr.Deref(rule.Resource.PatternType, redpandav1alpha2.PatternTypeLiteral) == redpandav1alpha2.PatternTypePrefixed {
			found = true
			break
		}
	}
	require.True(t, found, "Role %q should have ACL for topic pattern %q", role, pattern)
}

func roleShouldHaveNoManagedACLsInCluster(ctx context.Context, t framework.TestingT, role, cluster string) {
	clients := clientsForCluster(ctx, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.Empty(t, rules, "Role %q should have no managed ACLs", role)
}

func thereShouldBeNoACLsForRoleInCluster(ctx context.Context, t framework.TestingT, cluster, role string) {
	clients := clientsForCluster(ctx, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.Empty(t, rules, "There should be no ACLs for role %q", role)
}

func thereIsNoRole(ctx context.Context, role, cluster string) {
	clientsForCluster(ctx, cluster).ExpectNoRole(ctx, role)
}
