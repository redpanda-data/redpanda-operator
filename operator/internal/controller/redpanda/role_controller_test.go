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
	"github.com/twmb/franz-go/pkg/kgo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRoleReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &RoleReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	authorizationSpec := &redpandav1alpha2.RoleAuthorizationSpec{
		ACLs: []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeGroup,
				Name: "group",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationDescribe,
			},
		}},
	}

	baseRole := &redpandav1alpha2.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.RoleSpec{
			ClusterSource: environment.ClusterSourceValid,
			Authorization: authorizationSpec,
		},
	}

	for name, tt := range map[string]struct {
		mutate            func(role *redpandav1alpha2.Role)
		expectedCondition metav1.Condition
		onlyCheckDeletion bool
	}{
		"success - authorization": {
			expectedCondition: environment.SyncedCondition,
		},
		"success - authorization deletion cleanup": {
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization only": {
			mutate: func(role *redpandav1alpha2.Role) {
				// Keep authorization as is - roles only manage ACLs
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - authorization only deletion cleanup": {
			mutate: func(role *redpandav1alpha2.Role) {
				// Keep authorization as is - roles only manage ACLs
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - no authorization": {
			mutate: func(role *redpandav1alpha2.Role) {
				role.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - no authorization deletion cleanup": {
			mutate: func(role *redpandav1alpha2.Role) {
				role.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"error - invalid cluster ref": {
			mutate: func(role *redpandav1alpha2.Role) {
				role.Spec.ClusterSource = environment.ClusterSourceInvalidRef
			},
			expectedCondition: environment.InvalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(role *redpandav1alpha2.Role) {
				role.Spec.ClusterSource = environment.ClusterSourceNoSASL
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(role *redpandav1alpha2.Role) {
				role.Spec.ClusterSource = environment.ClusterSourceBadPassword
			},
			expectedCondition: environment.ClientErrorCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			role := baseRole.DeepCopy()
			role.Name = "role" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(role)
			}

			key := client.ObjectKeyFromObject(role)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, environment.Factory.Create(ctx, role))
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, environment.Factory.Get(ctx, key, role))
			require.Equal(t, []string{FinalizerKey}, role.Finalizers)
			require.Len(t, role.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, role.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, role.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, role.Status.Conditions[0].Reason)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				syncer, err := environment.Factory.ACLs(ctx, role)
				require.NoError(t, err)
				defer syncer.Close()

				// if we're supposed to have synced, then check to make sure we properly
				// set the management flags
				require.Equal(t, role.ShouldManageACLs(), role.Status.ManagedACLs)

				if role.ShouldManageACLs() {
					// make sure we actually have ACLs
					acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 1)
				}

				if !tt.onlyCheckDeletion {
					if role.ShouldManageACLs() {
						// now clear out any managed ACLs and re-check
						role.Spec.Authorization = nil
						require.NoError(t, environment.Factory.Update(ctx, role))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, environment.Factory.Get(ctx, key, role))
						require.False(t, role.Status.ManagedACLs)
					}

					// make sure we no longer have acls
					acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, environment.Factory.Delete(ctx, role))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))

				// make sure we no longer have acls
				acls, err := syncer.ListACLs(ctx, role.GetPrincipal())
				require.NoError(t, err)
				require.Len(t, acls, 0)

				return
			}

			// clean up and make sure we properly delete everything
			require.NoError(t, environment.Factory.Delete(ctx, role))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, role)))
		})
	}
}
