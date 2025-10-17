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
	"slices"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			"Member %q should not be in role %q", member, role)
	}
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

	// Then assign the travis user to the role
	// TODO: Add role membership assignment if needed for the test
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

func roleShouldHaveNoMembersInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	membersResp, err := adminClient.RoleMembers(ctx, role)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			t.Fatalf("Role %q not found when checking for no members: %v", role, err)
		}
		t.Fatalf("Failed to get members for role %q: %v", role, err)
	}

	require.Empty(t, membersResp.Members, "Role %q should have no members", role)
}

func iManuallyAssignMemberToRole(ctx context.Context, t framework.TestingT, member, role, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Assign member manually (User principal)
	_, err := adminClient.AssignRole(ctx, role, []rpadmin.RoleMember{{
		Name:          member,
		PrincipalType: "User",
	}})
	require.NoError(t, err, "Failed to manually assign member %q to role %q", member, role)
}

func roleShouldHaveStatusPrincipals(ctx context.Context, t framework.TestingT, role, expected string) {
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(role), &roleObject))

	// Split expected by commas and trim
	expected = strings.TrimSpace(expected)
	var expectedList []string
	if expected != "" {
		for _, p := range strings.Split(expected, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				expectedList = append(expectedList, p)
			}
		}
	}

	// Normalize expected (User: prefix if missing)
	for i, p := range expectedList {
		if !strings.Contains(p, ":") {
			expectedList[i] = "User:" + p
		}
	}

	// Sort both slices for comparison ignoring order
	slices.Sort(expectedList)
	if len(expectedList) == 0 {
		// normalize nil vs empty slice for comparison
		expectedList = []string{}
	}
	actual := append([]string{}, roleObject.Status.Principals...)
	slices.Sort(actual)

	require.Equal(t, expectedList, actual, "Status principals mismatch for role %q", role)
}
