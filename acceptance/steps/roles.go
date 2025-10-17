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
	"strings"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func roleIsSuccessfullySynced(ctx context.Context, t framework.TestingT, role string) {
	var roleObject redpandav1alpha2.RedpandaRole
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
	var roleObject redpandav1alpha2.RedpandaRole

	t.Logf("Deleting role %q", role)
	err := t.Get(ctx, t.ResourceKey(role), &roleObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			t.Logf("Role %q already deleted", role)
			return
		}
		t.Fatalf("Error deleting role %q: %v", role, err)
	}

	t.Logf("Found role %q, deleting it", role)
	require.NoError(t, t.Delete(ctx, &roleObject))
	t.Logf("Successfully deleted role %q CRD", role)
}

func roleShouldExistInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, role)
}

func thereShouldBeNoRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	t.Logf("Checking that role %q does not exist in cluster %q", role, cluster)

	// Add retry logic for role deletion timing issues
	require.Eventually(t, func() bool {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic during role check: %v", r)
			}
		}()
		versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, role)
		return true
	}, 60*time.Second, 5*time.Second, "Role %q should be deleted from cluster %q", role, cluster)
}

// principalsContainMember checks if a member exists in the principals list.
// Principals are in the format "User:name" or just "name", so we check if any principal contains the member name.
func principalsContainMember(principals []string, member string) bool {
	for _, p := range principals {
		if strings.Contains(p, member) {
			return true
		}
	}
	return false
}

func roleShouldHaveMembersAndInCluster(ctx context.Context, t framework.TestingT, role string, members string, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Parse the members string (e.g., "alice" or "alice" and "bob")
	var expectedMembers []string
	if strings.Contains(members, " and ") {
		// Handle "alice" and "bob" format
		parts := strings.Split(members, " and ")
		for _, part := range parts {
			part = strings.Trim(part, `"`)
			if part != "" {
				expectedMembers = append(expectedMembers, part)
			}
		}
	} else {
		// Handle single member
		expectedMembers = []string{strings.Trim(members, `"`)}
	}

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
	for _, expectedMember := range expectedMembers {
		require.True(t, actualMembers[expectedMember],
			"Expected member %q not found in role %q", expectedMember, role)
	}

	// Verify we have exactly the expected number of members
	require.Equal(t, len(expectedMembers), len(actualMembers),
		"Role %q should have exactly %d members, got %d", role, len(expectedMembers), len(actualMembers))
}

func roleShouldNotHaveMemberInCluster(ctx context.Context, t framework.TestingT, role, member, version, cluster string) {
	var roleObject redpandav1alpha2.RedpandaRole

	t.Logf("Verifying role %q does not have member %q", role, member)

	require.Eventually(t, func() bool {
		if err := t.Get(ctx, t.ResourceKey(role), &roleObject); err != nil {
			t.Logf("Error getting role: %v", err)
			return false
		}

		if principalsContainMember(roleObject.Status.Principals, member) {
			t.Logf("Member %q still in role %q status", member, role)
			return false
		}

		syncedCondition := apimeta.FindStatusCondition(roleObject.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
		if syncedCondition == nil || syncedCondition.Status != metav1.ConditionTrue {
			t.Logf("Role not synced yet, waiting...")
			return false
		}

		return true
	}, 60*time.Second, 5*time.Second, "Member %q should not be in role %q status", member, role)

	t.Logf("Verified member %q is not in role %q status", member, role)

	clients := versionedClientsForCluster(ctx, version, cluster)
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
			"Member %q should not be in role %q in Redpanda cluster", member, role)
	}

	t.Logf("Verified member %q is not in role %q in Redpanda cluster", member, role)
}

func roleShouldHaveACLsForTopicPatternInCluster(ctx context.Context, t framework.TestingT, role, pattern, version, cluster string) {
	t.Logf("Checking ACLs for role %q in cluster %q", role, cluster)

	// Add a small delay to ensure cluster is fully ready
	time.Sleep(5 * time.Second)

	clients := versionedClientsForCluster(ctx, version, cluster)
	t.Logf("Created cluster clients for %q", cluster)

	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()
	t.Logf("Created ACL client for cluster %q", cluster)

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	t.Logf("Listing ACLs for role %q principal %q", role, roleObj.GetPrincipal())
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

func roleShouldHaveNoManagedACLsInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.Empty(t, rules, "Role %q should have no managed ACLs", role)
}

func thereShouldBeNoACLsForRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// Create a role object for ACL checking
	roleObj := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{Name: role},
	}

	// List ACLs for the role
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.Empty(t, rules, "There should be no ACLs for role %q", role)
}

func thereIsNoRole(ctx context.Context, role, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, role)
}

func thereIsAPreExistingRole(ctx context.Context, role, version, cluster string) {
	t := framework.T(ctx)

	t.Logf("Creating pre-existing role %q in cluster %q", role, cluster)
	adminClient := versionedClientsForCluster(ctx, version, cluster).RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Create the role first
	_, err := adminClient.CreateRole(ctx, role)
	if err != nil {
		t.Fatalf("Failed to create pre-existing role %q: %v", role, err)
	}

	_, err = adminClient.AssignRole(ctx, role, []rpadmin.RoleMember{
		{
			Name:          "travis",
			PrincipalType: "User",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create pre-existing role %q: %v", role, err)
	}
	t.Logf("Successfully created pre-existing role %q", role)
}

func thereShouldStillBeRole(ctx context.Context, role, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, role)
}

func roleShouldHaveRemovedPrincipals(ctx context.Context, t framework.TestingT, role, members, version, cluster string) {
	var roleObject redpandav1alpha2.RedpandaRole

	t.Logf("Waiting for role %q to remove principals: %s", role, members)

	// Parse the members string (e.g., "alice and bob")
	var expectedRemovedMembers []string
	if strings.Contains(members, " and ") {
		parts := strings.Split(members, " and ")
		for _, part := range parts {
			part = strings.Trim(strings.TrimSpace(part), `"`)
			if part != "" {
				expectedRemovedMembers = append(expectedRemovedMembers, part)
			}
		}
	} else {
		expectedRemovedMembers = []string{strings.Trim(strings.TrimSpace(members), `"`)}
	}

	t.Logf("Expecting principals to be removed: %v", expectedRemovedMembers)

	// Wait for principals to be removed from Role status
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, t.ResourceKey(role), &roleObject); err != nil {
			t.Logf("Error getting role: %v", err)
			return false
		}

		// Check if all expected members are removed from status
		for _, member := range expectedRemovedMembers {
			if principalsContainMember(roleObject.Status.Principals, member) {
				t.Logf("Principal %q still in role status", member)
				return false
			}
		}

		// Verify role is synced after the change
		syncedCondition := apimeta.FindStatusCondition(roleObject.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
		if syncedCondition == nil || syncedCondition.Status != metav1.ConditionTrue {
			t.Logf("Role not synced yet, waiting...")
			return false
		}

		return true
	}, 60*time.Second, 2*time.Second, "Role %q did not remove principals %v", role, expectedRemovedMembers)

	t.Logf("Role %q status updated, principals removed: %v", role, expectedRemovedMembers)

	// Verify principals are actually removed from Redpanda cluster
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	membersResp, err := adminClient.RoleMembers(ctx, role)
	require.NoError(t, err, "Failed to get members for role %q", role)

	// Build map of actual members
	actualMembers := make(map[string]bool)
	for _, member := range membersResp.Members {
		actualMembers[member.Name] = true
	}

	// Verify each expected member is NOT in Redpanda
	for _, expectedMember := range expectedRemovedMembers {
		require.False(t, actualMembers[expectedMember],
			"Principal %q should be removed from role %q in Redpanda cluster", expectedMember, role)
	}

	t.Logf("Verified principals %v are removed from role %q in Redpanda", expectedRemovedMembers, role)
}
