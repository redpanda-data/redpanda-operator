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
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func roleBindingIsSuccessfullySynced(ctx context.Context, t framework.TestingT, roleBinding string) {
	var roleBindingObject redpandav1alpha2.RedpandaRoleBinding

	t.Logf("Waiting for RoleBinding %q to be created", roleBinding)

	// Wait for the RoleBinding to be created (handles race condition when multiple RoleBindings
	// are applied in the same manifest)
	require.Eventually(t, func() bool {
		err := t.Get(ctx, t.ResourceKey(roleBinding), &roleBindingObject)
		return err == nil
	}, 60*time.Second, 5*time.Second, "RoleBinding %q was not created", roleBinding)

	t.Logf("RoleBinding %q created, waiting for controller to reconcile", roleBinding)

	// Wait for the controller to reconcile and set Synced condition to True
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, t.ResourceKey(roleBinding), &roleBindingObject); err != nil {
			t.Logf("Error getting RoleBinding: %v", err)
			return false
		}

		syncCondition := apimeta.FindStatusCondition(roleBindingObject.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
		if syncCondition == nil {
			t.Logf("Synced condition not found yet")
			return false
		}

		if syncCondition.Status != metav1.ConditionTrue {
			t.Logf("Synced condition is %s (reason: %s, message: %s), waiting for True",
				syncCondition.Status, syncCondition.Reason, syncCondition.Message)
			return false
		}

		if syncCondition.Reason != redpandav1alpha2.ResourceConditionReasonSynced {
			t.Logf("Synced condition reason is %s, waiting for %s",
				syncCondition.Reason, redpandav1alpha2.ResourceConditionReasonSynced)
			return false
		}

		return true
	}, 60*time.Second, 2*time.Second, "RoleBinding %q was not successfully synced", roleBinding)

	t.Logf("RoleBinding %q is successfully synced", roleBinding)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("Deleting RoleBinding %q", roleBinding)
		err := t.Get(ctx, t.ResourceKey(roleBinding), &roleBindingObject)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("RoleBinding %q already deleted", roleBinding)
				return
			}
			t.Fatalf("Error deleting rolebinding %q: %v", roleBinding, err)
		}
		require.NoError(t, t.Delete(ctx, &roleBindingObject))
		t.Logf("Successfully deleted RoleBinding %q", roleBinding)
	})
}

func roleBindingConditionShouldBe(ctx context.Context, t framework.TestingT, roleBinding, conditionType, expectedStatus string) {
	var roleBindingObject redpandav1alpha2.RedpandaRoleBinding

	t.Logf("Waiting for RoleBinding %q condition %q to be %s", roleBinding, conditionType, expectedStatus)

	// Wait for condition to reach expected status
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, t.ResourceKey(roleBinding), &roleBindingObject); err != nil {
			t.Logf("Error getting RoleBinding: %v", err)
			return false
		}

		condition := apimeta.FindStatusCondition(roleBindingObject.Status.Conditions, conditionType)
		if condition == nil {
			availableTypes := getConditionTypes(roleBindingObject.Status.Conditions)
			t.Logf("Condition %q not found yet. Available: %v", conditionType, availableTypes)
			return false
		}

		if string(condition.Status) != expectedStatus {
			t.Logf("Condition %q is %s, waiting for %s. Message: %s",
				conditionType, condition.Status, expectedStatus, condition.Message)
			return false
		}

		return true
	}, 60*time.Second, 2*time.Second,
		"RoleBinding %q condition %q did not reach %s", roleBinding, conditionType, expectedStatus)

	// Get final state for logging
	require.NoError(t, t.Get(ctx, t.ResourceKey(roleBinding), &roleBindingObject))
	condition := apimeta.FindStatusCondition(roleBindingObject.Status.Conditions, conditionType)

	t.Logf("RoleBinding %q condition %q is %s as expected. Message: %s",
		roleBinding, conditionType, expectedStatus, condition.Message)
}

// getConditionTypes extracts condition type names from a conditions list.
// Returns a helpful message if no conditions exist.
func getConditionTypes(conditions []metav1.Condition) []string {
	if len(conditions) == 0 {
		return []string{"<no conditions>"}
	}
	types := make([]string, len(conditions))
	for i, c := range conditions {
		types[i] = c.Type
	}
	return types
}

func iDeleteTheCRDRoleBinding(ctx context.Context, t framework.TestingT, roleBinding string) {
	var rb redpandav1alpha2.RedpandaRoleBinding

	t.Logf("Deleting RoleBinding %q", roleBinding)
	err := t.Get(ctx, t.ResourceKey(roleBinding), &rb)
	if err != nil {
		if apierrors.IsNotFound(err) {
			t.Logf("RoleBinding %q already deleted", roleBinding)
			return
		}
		t.Fatalf("Error getting rolebinding %q: %v", roleBinding, err)
	}
	require.NoError(t, t.Delete(ctx, &rb))
	t.Logf("Successfully deleted RoleBinding %q", roleBinding)
}
