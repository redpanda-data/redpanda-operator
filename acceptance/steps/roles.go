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
		// unexpected error
		t.Fatalf("Error fetching role %q for deletion: %v", role, err)
	}

	t.Logf("Found role %q, deleting it", role)
	require.NoError(t, t.Delete(ctx, &roleObject))

	// Wait for the CRD to actually disappear to avoid leakage between scenarios
	require.Eventually(t, func() bool {
		// reuse the same object variable
		getErr := t.Get(ctx, t.ResourceKey(role), &roleObject)
		if apierrors.IsNotFound(getErr) {
			return true
		}
		if getErr != nil {
			// transient API errors: keep retrying
			return false
		}
		return false
	}, 30*time.Second, 1*time.Second, "Role %q was not deleted from the API server in time", role)

	t.Logf("Successfully deleted role %q CRD", role)
}

func roleShouldExistInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectRole(ctx, role)
}

func thereShouldBeNoRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	// Reuse existing polling logic inside ExpectNoRole instead of custom loop
	versionedClientsForCluster(ctx, version, cluster).ExpectNoRole(ctx, role)
}

// helper to retrieve list of role member names
func getRoleMembers(ctx context.Context, t framework.TestingT, role, version, cluster string) []string {
	clients := versionedClientsForCluster(ctx, version, cluster)
	adminClient := clients.RedpandaAdmin(ctx)
	defer adminClient.Close()
	resp, err := adminClient.RoleMembers(ctx, role)
	if err != nil {
		t.Fatalf("Failed to get members for role %q: %v", role, err)
	}
	var members []string
	for _, m := range resp.Members {
		members = append(members, m.Name)
	}
	return members
}

func roleShouldHaveMembersAndInCluster(ctx context.Context, t framework.TestingT, role string, members string, version, cluster string) {
	// Parse the members string (e.g., "alice" or "alice" and "bob")
	var expectedMembers []string
	if strings.Contains(members, " and ") {
		parts := strings.Split(members, " and ")
		for _, part := range parts {
			part = strings.Trim(part, `"`)
			if part != "" {
				expectedMembers = append(expectedMembers, part)
			}
		}
	} else {
		expectedMembers = []string{strings.Trim(members, `"`)}
	}

	require.Eventually(t, func() bool {
		actualMembers := getRoleMembers(ctx, t, role, version, cluster)
		if len(actualMembers) != len(expectedMembers) {
			return false
		}
		actualSet := make(map[string]struct{}, len(actualMembers))
		for _, m := range actualMembers {
			actualSet[m] = struct{}{}
		}
		for _, em := range expectedMembers {
			if _, ok := actualSet[em]; !ok {
				return false
			}
		}
		return true
	}, 30*time.Second, 1*time.Second, "Role %q did not have expected members %v in time", role, expectedMembers)
}

func roleShouldNotHaveMemberInCluster(ctx context.Context, t framework.TestingT, role, member, version, cluster string) {
	require.Eventually(t, func() bool {
		actualMembers := getRoleMembers(ctx, t, role, version, cluster)
		return !slices.Contains(actualMembers, member)
	}, 30*time.Second, 1*time.Second, "Member %q still present in role %q", member, role)
}

func roleShouldHaveNoMembersInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	require.Eventually(t, func() bool {
		actualMembers := getRoleMembers(ctx, t, role, version, cluster)
		return len(actualMembers) == 0
	}, 30*time.Second, 1*time.Second, "Role %q still has members", role)
}

func roleShouldHaveACLsForTopicPatternInCluster(ctx context.Context, t framework.TestingT, role, pattern, version, cluster string) {
	t.Logf("Checking ACLs for role %q in cluster %q", role, cluster)

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	roleObj := &redpandav1alpha2.RedpandaRole{ObjectMeta: metav1.ObjectMeta{Name: role}}

	require.Eventually(t, func() bool {
		rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
		if err != nil {
			return false
		}
		for _, rule := range rules {
			if rule.Resource.Type == redpandav1alpha2.ResourceTypeTopic &&
				rule.Resource.Name == pattern &&
				ptr.Deref(rule.Resource.PatternType, redpandav1alpha2.PatternTypeLiteral) == redpandav1alpha2.PatternTypePrefixed {
				return true
			}
		}
		return false
	}, 30*time.Second, 1*time.Second, "Role %q did not have ACL for topic pattern %q in time", role, pattern)
}

// unified helper for asserting zero ACLs for a role principal
func assertNoACLsForRole(ctx context.Context, t framework.TestingT, role, version, cluster, message string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	roleObj := &redpandav1alpha2.RedpandaRole{ObjectMeta: metav1.ObjectMeta{Name: role}}
	rules, err := aclClient.ListACLs(ctx, roleObj.GetPrincipal())
	if err != nil {
		t.Fatalf("Failed to list ACLs for role %q: %v", role, err)
	}
	require.Empty(t, rules, message, role)
}

func roleShouldHaveNoManagedACLsInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	assertNoACLsForRole(ctx, t, role, version, cluster, "Role %q should have no managed ACLs")
}

func thereShouldBeNoACLsForRoleInCluster(ctx context.Context, t framework.TestingT, role, version, cluster string) {
	assertNoACLsForRole(ctx, t, role, version, cluster, "There should be no ACLs for role %q")
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
	// Parse expected principals
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
	for i, p := range expectedList {
		if !strings.Contains(p, ":") {
			expectedList[i] = "User:" + p
		}
	}
	slices.Sort(expectedList)
	if len(expectedList) == 0 {
		expectedList = []string{}
	}

	require.Eventually(t, func() bool {
		var roleObject redpandav1alpha2.RedpandaRole
		if err := t.Get(ctx, t.ResourceKey(role), &roleObject); err != nil {
			return false
		}
		actual := append([]string{}, roleObject.Status.Principals...)
		slices.Sort(actual)
		if len(actual) != len(expectedList) {
			return false
		}
		for i := range actual {
			if actual[i] != expectedList[i] {
				return false
			}
		}
		return true
	}, 30*time.Second, 1*time.Second, "Status principals mismatch for role %q", role)
}
