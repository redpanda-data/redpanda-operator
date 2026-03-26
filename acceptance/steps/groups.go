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
	"fmt"
	"time"

	"github.com/redpanda-data/common-go/rpsr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func groupIsSuccessfullySynced(ctx context.Context, t framework.TestingT, group string) {
	var groupObject redpandav1alpha2.Group
	require.NoError(t, t.Get(ctx, t.ResourceKey(group), &groupObject))

	waitForSyncedCondition(ctx, t, &groupObject, func() []metav1.Condition {
		return groupObject.Status.Conditions
	})

	t.Cleanup(func(ctx context.Context) {
		t.Logf("Deleting group %q", group)
		err := t.Get(ctx, t.ResourceKey(group), &groupObject)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return
			}
			t.Fatalf("Error deleting group %q: %v", group, err)
		}
		require.NoError(t, t.Delete(ctx, &groupObject))
	})
}

func iDeleteTheCRDGroup(ctx context.Context, t framework.TestingT, group string) {
	var groupObject redpandav1alpha2.Group

	t.Logf("Deleting group %q", group)
	err := t.Get(ctx, t.ResourceKey(group), &groupObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			t.Logf("Group %q already deleted", group)
			return
		}
		t.Fatalf("Error deleting group %q: %v", group, err)
	}

	t.Logf("Found group %q, deleting it", group)
	require.NoError(t, t.Delete(ctx, &groupObject))
	t.Logf("Successfully deleted group %q CRD", group)

	// Wait for the finalizer to complete and the object to be fully removed
	require.Eventually(t, func() bool {
		err := t.Get(ctx, t.ResourceKey(group), &groupObject)
		return apierrors.IsNotFound(err)
	}, 60*time.Second, 2*time.Second, "Group %q should be fully deleted", group)
}

func groupShouldHaveNACLsForTopicPatternInCluster(ctx context.Context, t framework.TestingT, group string, count int, pattern, version, cluster string) {
	t.Logf("Checking that group %q has %d ACLs for topic pattern %q in cluster %q", group, count, pattern, cluster)

	var groupObject redpandav1alpha2.Group
	require.NoError(t, t.Get(ctx, t.ResourceKey(group), &groupObject))

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	principal := groupObject.GetPrincipal()
	rules, err := aclClient.ListACLs(ctx, principal)
	if err != nil {
		t.Fatalf("Failed to list ACLs for group %q (principal %q): %v", group, principal, err)
	}

	var matched int
	for _, rule := range rules {
		if rule.Resource.Type == redpandav1alpha2.ResourceTypeTopic &&
			rule.Resource.Name == pattern &&
			ptr.Deref(rule.Resource.PatternType, redpandav1alpha2.PatternTypeLiteral) == redpandav1alpha2.PatternTypePrefixed {
			matched++
		}
	}
	require.Equal(t, count, matched, "Group %q should have %d ACLs for topic pattern %q, got %d", group, count, pattern, matched)

	// Also check SR ACLs if the group has subject-type ACL rules
	if groupObject.Spec.Authorization != nil && hasSubjectACLRules(groupObject.Spec.Authorization.ACLs) {
		srClient := clients.SchemaRegistryACLs(ctx)
		srACLs, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: principal})
		if err != nil {
			t.Fatalf("Failed to list SR ACLs for group %q (principal %q): %v", group, principal, err)
		}
		require.NotEmpty(t, srACLs, "Group %q should have SR ACLs", group)
	}
}

func groupShouldHaveACLsInCluster(ctx context.Context, t framework.TestingT, group, version, cluster string) {
	t.Logf("Checking that group %q has ACLs in cluster %q", group, cluster)

	// Get the K8s group object
	var groupObject redpandav1alpha2.Group
	require.NoError(t, t.Get(ctx, t.ResourceKey(group), &groupObject))

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	principal := groupObject.GetPrincipal()
	rules, err := aclClient.ListACLs(ctx, principal)
	if err != nil {
		t.Fatalf("Failed to list ACLs for group %q (principal %q): %v", group, principal, err)
	}
	require.NotEmpty(t, rules, "Group %q should have Kafka ACLs", group)

	// Also check SR ACLs if the group has subject-type ACL rules
	if groupObject.Spec.Authorization != nil && hasSubjectACLRules(groupObject.Spec.Authorization.ACLs) {
		srClient := clients.SchemaRegistryACLs(ctx)
		srACLs, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: principal})
		if err != nil {
			t.Fatalf("Failed to list SR ACLs for group %q (principal %q): %v", group, principal, err)
		}
		require.NotEmpty(t, srACLs, "Group %q should have SR ACLs", group)
	}
}

func thereShouldBeNoACLsForGroupInCluster(ctx context.Context, t framework.TestingT, group, version, cluster string) {
	t.Logf("Checking that group %q has no ACLs in cluster %q", group, cluster)

	clients := versionedClientsForCluster(ctx, version, cluster)
	aclClient := clients.ACLs(ctx)
	defer aclClient.Close()

	principal := fmt.Sprintf("Group:%s", group)
	require.Eventually(t, func() bool {
		rules, err := aclClient.ListACLs(ctx, principal)
		if err != nil {
			t.Logf("Error listing ACLs for group %q: %v", group, err)
			return false
		}
		return len(rules) == 0
	}, 60*time.Second, 2*time.Second, "Group %q should have no Kafka ACLs", group)

	// Also check SR ACLs are cleaned up
	srClient := clients.SchemaRegistryACLs(ctx)
	require.Eventually(t, func() bool {
		srACLs, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: principal})
		if err != nil {
			t.Logf("Error listing SR ACLs for group %q: %v", group, err)
			return false
		}
		return len(srACLs) == 0
	}, 60*time.Second, 2*time.Second, "Group %q should have no SR ACLs", group)
}
