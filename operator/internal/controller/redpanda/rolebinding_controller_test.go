// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRoleBindingReconcile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	reconciler := &RoleBindingReconciler{}
	environment := InitializeResourceReconcilerTest(t, ctx, reconciler)
	reconciler.client = environment.Factory.Client

	baseRole := &redpandav1alpha2.RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-role",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
		},
	}

	baseRoleBinding := &redpandav1alpha2.RedpandaRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.RedpandaRoleBindingSpec{
			RoleRef:    redpandav1alpha2.RoleRef{Name: "test-role"},
			Principals: []string{"User:alice", "User:bob"},
		},
	}

	unexpectedErrorCondition := metav1.Condition{
		Type:   redpandav1alpha2.ResourceConditionTypeSynced,
		Status: metav1.ConditionFalse,
		Reason: redpandav1alpha2.ResourceConditionReasonUnexpectedError,
	}

	for name, tt := range map[string]struct {
		mutateRole        func(role *redpandav1alpha2.RedpandaRole)
		mutateBinding     func(rb *redpandav1alpha2.RedpandaRoleBinding)
		skipRoleCreation  bool
		expectedCondition metav1.Condition
	}{
		"success - all principals synced": {
			expectedCondition: environment.SyncedCondition,
		},
		"error - role not found": {
			skipRoleCreation: true,
			mutateBinding: func(rb *redpandav1alpha2.RedpandaRoleBinding) {
				rb.Spec.RoleRef.Name = "non-existent-role"
			},
			expectedCondition: unexpectedErrorCondition,
		},
		"error - role not synced yet": {
			mutateRole: func(role *redpandav1alpha2.RedpandaRole) {
				apimeta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
					Type:               redpandav1alpha2.ResourceConditionTypeSynced,
					Status:             metav1.ConditionUnknown,
					Reason:             "Pending",
					Message:            "Role sync pending",
					ObservedGeneration: role.Generation,
				})
			},
			expectedCondition: unexpectedErrorCondition,
		},
		"error - principals not in role status": {
			mutateRole: func(role *redpandav1alpha2.RedpandaRole) {
				role.Status.Principals = []string{}
			},
			expectedCondition: unexpectedErrorCondition,
		},
		"error - partial principals missing": {
			mutateBinding: func(rb *redpandav1alpha2.RedpandaRoleBinding) {
				rb.Spec.Principals = []string{"User:alice", "User:charlie"}
			},
			expectedCondition: unexpectedErrorCondition,
		},
		"success - extra principals in role": {
			mutateRole: func(role *redpandav1alpha2.RedpandaRole) {
				role.Status.Principals = []string{"User:alice", "User:bob", "User:charlie"}
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - normalized principal matching": {
			mutateBinding: func(rb *redpandav1alpha2.RedpandaRoleBinding) {
				rb.Spec.Principals = []string{"alice", "bob"}
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - multiple principals": {
			mutateRole: func(role *redpandav1alpha2.RedpandaRole) {
				role.Status.Principals = []string{"User:user1", "User:user2", "User:user3", "User:user4", "User:user5", "User:user6", "User:user7", "User:user8", "User:user9", "User:user10"}
			},
			mutateBinding: func(rb *redpandav1alpha2.RedpandaRoleBinding) {
				rb.Spec.Principals = []string{"User:user1", "User:user2", "User:user3", "User:user4", "User:user5", "User:user6", "User:user7", "User:user8", "User:user9", "User:user10"}
			},
			expectedCondition: environment.SyncedCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			role := baseRole.DeepCopy()
			role.Name = "role-" + strconv.Itoa(int(time.Now().UnixNano()))

			rb := baseRoleBinding.DeepCopy()
			rb.Name = "binding-" + strconv.Itoa(int(time.Now().UnixNano()))
			rb.Spec.RoleRef.Name = role.Name

			if tt.mutateBinding != nil {
				tt.mutateBinding(rb)
			}

			// Create role unless test says to skip
			if !tt.skipRoleCreation {
				require.NoError(t, environment.Factory.Create(ctx, role))

				// CRITICAL: Get role to obtain correct resourceVersion before status update
				require.NoError(t, environment.Factory.Get(ctx, client.ObjectKeyFromObject(role), role))

				// Set default status: synced with principals
				role.Status.ObservedGeneration = role.Generation
				role.Status.Principals = []string{"User:alice", "User:bob"}
				apimeta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
					Type:               redpandav1alpha2.ResourceConditionTypeSynced,
					Status:             metav1.ConditionTrue,
					Reason:             redpandav1alpha2.ResourceConditionReasonSynced,
					Message:            "Role synced",
					ObservedGeneration: role.Generation,
				})

				// Apply mutations after setting default status
				if tt.mutateRole != nil {
					tt.mutateRole(role)
				}

				require.NoError(t, environment.Factory.Status().Update(ctx, role))
			}

			key := client.ObjectKeyFromObject(rb)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, environment.Factory.Create(ctx, rb))
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, environment.Factory.Get(ctx, key, rb))
			require.Len(t, rb.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, rb.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, rb.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, rb.Status.Conditions[0].Reason)

			// Cleanup
			require.NoError(t, environment.Factory.Delete(ctx, rb))
			if !tt.skipRoleCreation {
				require.NoError(t, environment.Factory.Delete(ctx, role))
			}
		})
	}
}
