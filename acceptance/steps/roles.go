// Copyright 2026 Redpanda Data, Inc.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// parseMembersList parses a members string like "alice" or "alice" and "bob" into a slice.
func parseMembersList(members string) []string {
	if strings.Contains(members, " and ") {
		parts := strings.Split(members, " and ")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.Trim(part, `"`)
			if part != "" {
				result = append(result, part)
			}
		}
		return result
	}
	return []string{strings.Trim(members, `"`)}
}

// getEffectiveRoleName fetches the RedpandaRole and returns its effective name.
func getEffectiveRoleName(ctx context.Context, t framework.TestingT, roleName string) string {
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(roleName), &roleObject))
	return roleObject.GetEffectiveRoleName()
}

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
	effectiveRoleName := getEffectiveRoleName(ctx, t, role)
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, effectiveRoleName)
}

func thereShouldBeNoRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	t.Logf("Checking that role %q does not exist in cluster %q", role, cluster)

	// Get the K8s role object to compute the effective role name
	var roleObject redpandav1alpha2.RedpandaRole
	err := t.Get(ctx, t.ResourceKey(role), &roleObject)
	if err != nil {
		// If the K8s role object doesn't exist, check that no role exists with the K8s name
		// This handles the case where the role was deleted entirely
		require.Eventually(t, func() bool {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Recovered from panic during role check: %v", r)
				}
			}()
			versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, role)
			return true
		}, 60*time.Second, 5*time.Second, "Role %q should be deleted from cluster %q", role, cluster)
		return
	}

	// If the K8s role object exists, check that no role exists with the effective name
	effectiveRoleName := roleObject.GetEffectiveRoleName()
	require.Eventually(t, func() bool {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic during role check: %v", r)
			}
		}()
		versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, effectiveRoleName)
		return true
	}, 60*time.Second, 5*time.Second, "Role %q should be deleted from cluster %q", effectiveRoleName, cluster)
}

func roleShouldHaveMembersAndInCluster(ctx context.Context, t framework.TestingT, role string, members string, version, cluster string) {
	effectiveRoleName := getEffectiveRoleName(ctx, t, role)

	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	expectedMembers := parseMembersList(members)

	// Get role members from Redpanda using the effective role name
	membersResp, err := adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		t.Fatalf("Failed to get members for role %q (effective name %q): %v", role, effectiveRoleName, err)
	}

	// Check that expected members are present
	actualMembers := make(map[string]bool)
	for _, member := range membersResp.Members {
		actualMembers[member.Name] = true
	}

	// Verify all expected members are present
	for _, expectedMember := range expectedMembers {
		require.True(t, actualMembers[expectedMember],
			"Expected member %q not found in role %q (effective name %q)", expectedMember, role, effectiveRoleName)
	}

	// Verify we have exactly the expected number of members
	require.Equal(t, len(expectedMembers), len(actualMembers),
		"Role %q (effective name %q) should have exactly %d members, got %d", role, effectiveRoleName, len(expectedMembers), len(actualMembers))
}

func roleShouldNotHaveMemberInCluster(ctx context.Context, t framework.TestingT, role, member, version, cluster string) {
	effectiveRoleName := getEffectiveRoleName(ctx, t, role)

	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Get role members from Redpanda using the effective role name
	membersResp, err := adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		t.Fatalf("Failed to get members for role %q (effective name %q): %v", role, effectiveRoleName, err)
	}

	// Check that the member is not present
	for _, m := range membersResp.Members {
		require.NotEqual(t, member, m.Name,
			"Member %q should not be in role %q (effective name %q)", member, role, effectiveRoleName)
	}
}

func roleShouldHaveACLsForTopicPatternInCluster(ctx context.Context, t framework.TestingT, role, pattern, version, cluster string) {
	t.Logf("Checking ACLs for role %q in cluster %q", role, cluster)

	// Add a small delay to ensure cluster is fully ready
	time.Sleep(5 * time.Second)

	// Get the K8s role object to compute the effective role name and proper principal
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(role), &roleObject))

	clients := versionedClientsForCluster(ctx, version, cluster)
	t.Logf("Created cluster clients for %q", cluster)

	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()
	t.Logf("Created ACL client for cluster %q", cluster)

	// List ACLs for the role using the proper principal (which includes effective role name)
	principal := roleObject.GetPrincipal()
	t.Logf("Listing ACLs for role %q principal %q", role, principal)
	rules, err := aclClient.ListACLs(ctx, principal)
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q (principal %q): %v", role, principal, err)
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
	// Get the K8s role object to compute the proper principal
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(role), &roleObject))

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// List ACLs for the role using the proper principal (which includes effective role name)
	principal := roleObject.GetPrincipal()
	rules, err := aclClient.ListACLs(ctx, principal)
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q (principal %q): %v", role, principal, err)
	}
	require.Empty(t, rules, "Role %q should have no managed ACLs", role)
}

func thereShouldBeNoACLsForRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	// Get the K8s role object to compute the proper principal
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(role), &roleObject))

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	// List ACLs for the role using the proper principal (which includes effective role name)
	principal := roleObject.GetPrincipal()
	rules, err := aclClient.ListACLs(ctx, principal)
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q (principal %q): %v", role, principal, err)
	}
	require.Empty(t, rules, "There should be no ACLs for role %q", role)
}

func thereIsNoRole(ctx context.Context, effectiveRoleName, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, effectiveRoleName)
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

func thereShouldStillBeRole(ctx context.Context, effectiveRoleName, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, effectiveRoleName)
}

func roleShouldHaveNoMembersInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	effectiveRoleName := getEffectiveRoleName(ctx, t, role)

	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	// Get role members from Redpanda using the effective role name
	membersResp, err := adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		t.Fatalf("Failed to get members for role %q (effective name %q): %v", role, effectiveRoleName, err)
	}

	// Check that there are no members
	require.Empty(t, membersResp.Members, "Role %q (effective name %q) should have no members", role, effectiveRoleName)
}

func redpandaRoleShouldHaveStatusFieldSetTo(ctx context.Context, t framework.TestingT, roleName, field, value string) {
	var roleObject redpandav1alpha2.RedpandaRole
	require.NoError(t, t.Get(ctx, t.ResourceKey(roleName), &roleObject))

	switch field {
	case "managedPrincipals":
		expectedValue := value == "true"
		require.Equal(t, expectedValue, roleObject.Status.ManagedPrincipals,
			"RedpandaRole %q status.managedPrincipals should be %v", roleName, expectedValue)
	case "managedRole":
		expectedValue := value == "true"
		require.Equal(t, expectedValue, roleObject.Status.ManagedRole,
			"RedpandaRole %q status.managedRole should be %v", roleName, expectedValue)
	case "managedAcls":
		expectedValue := value == "true"
		require.Equal(t, expectedValue, roleObject.Status.ManagedACLs,
			"RedpandaRole %q status.managedAcls should be %v", roleName, expectedValue)
	case "effectiveRoleName":
		require.Equal(t, value, roleObject.Status.EffectiveRoleName,
			"RedpandaRole %q status.effectiveRoleName should be %q", roleName, value)
	default:
		t.Fatalf("Unknown status field %q", field)
	}
}

// Step functions for direct testing with effective role names

func roleShouldExistInClusterWithEffectiveName(ctx context.Context, t framework.TestingT, roleName, version, cluster, effectiveRoleName string) {
	// This step directly tests the effective role name in Redpanda
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, effectiveRoleName)
}

func thereShouldBeNoRoleInClusterWithEffectiveName(ctx context.Context, t framework.TestingT, roleName, version, cluster, effectiveRoleName string) {
	t.Logf("Checking that role %q does not exist in cluster %q", effectiveRoleName, cluster)

	// Add retry logic for role deletion timing issues
	require.Eventually(t, func() bool {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic during role check: %v", r)
			}
		}()
		versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, effectiveRoleName)
		return true
	}, 60*time.Second, 5*time.Second, "Role %q should be deleted from cluster %q", effectiveRoleName, cluster)
}

func roleShouldHaveMembersWithEffectiveName(ctx context.Context, t framework.TestingT, roleName, members, version, cluster, effectiveRoleName string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()

	expectedMembers := parseMembersList(members)

	// Get role members from Redpanda using the effective role name directly
	membersResp, err := adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		t.Fatalf("Failed to get members for effective role %q: %v", effectiveRoleName, err)
	}

	// Check that expected members are present
	actualMembers := make(map[string]bool)
	for _, member := range membersResp.Members {
		actualMembers[member.Name] = true
	}

	// Verify all expected members are present
	for _, expectedMember := range expectedMembers {
		require.True(t, actualMembers[expectedMember],
			"Expected member %q not found in effective role %q", expectedMember, effectiveRoleName)
	}

	// Verify we have exactly the expected number of members
	require.Equal(t, len(expectedMembers), len(actualMembers),
		"Effective role %q should have exactly %d members, got %d", effectiveRoleName, len(expectedMembers), len(actualMembers))
}
