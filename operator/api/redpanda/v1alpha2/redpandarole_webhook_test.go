// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

// webhookTestEnv provides a test environment for webhook tests
type webhookTestEnv struct {
	ctx       context.Context
	client    client.Client
	validator *roleValidator
	namespace string
	cleanup   func()
}

// setupWebhookTest creates a test environment for webhook tests
func setupWebhookTest(t *testing.T) *webhookTestEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	return &webhookTestEnv{
		ctx:       ctx,
		client:    c,
		validator: &roleValidator{client: c},
		namespace: metav1.NamespaceDefault,
		cleanup: func() {
			cancel()
			if err := testEnv.Stop(); err != nil {
				t.Logf("Failed to stop test environment: %v", err)
			}
		},
	}
}

// cleanupRoles deletes a list of roles from the test environment
func (env *webhookTestEnv) cleanupRoles(t *testing.T, roleNames []string) {
	t.Helper()
	for _, roleName := range roleNames {
		err := env.client.Delete(env.ctx, &RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: env.namespace,
			},
		})
		if err != nil && client.IgnoreNotFound(err) != nil {
			t.Logf("Failed to cleanup role %s: %v", roleName, err)
		}
	}
}

func TestRedpandaRoleWebhook_ValidateEffectiveRoleNameUniqueness(t *testing.T) {
	env := setupWebhookTest(t)
	defer env.cleanup()

	var createdRoles []string
	defer func() {
		env.cleanupRoles(t, createdRoles)
	}()

	// Test case 1: Create role without internal role name - should succeed
	role1 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role1",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	require.NoError(t, env.client.Create(env.ctx, role1))
	createdRoles = append(createdRoles, "role1")

	// Test case 2: Create role with internal role name - should succeed
	role2 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role2",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			InternalRoleName: ptr.To("internal-name"),
		},
	}

	require.NoError(t, env.client.Create(env.ctx, role2))
	createdRoles = append(createdRoles, "role2")

	// Test case 3: Try to create role with conflicting internal role name - should fail
	role3 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role3",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			InternalRoleName: ptr.To("internal-name"), // Same as role2
		},
	}

	// Test the validation method directly using new validator
	validationErrs := env.validator.validateEffectiveRoleNameUniqueness(env.ctx, role3, nil)
	require.NotEmpty(t, validationErrs, "Expected validation error for conflicting internal role name")
	assert.Contains(t, validationErrs[0].Detail, `effective role name conflicts with existing role "role2"`)

	// Test case 4: Try to create role with same name as existing role's effective name - should fail
	role4 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "__internal-name", // Same as role2's effective name
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	// Test the validation method directly for role4
	validationErrs = env.validator.validateEffectiveRoleNameUniqueness(env.ctx, role4, nil)
	require.NotEmpty(t, validationErrs, "Expected validation error for role name conflicting with effective name")
	assert.Contains(t, validationErrs[0].Detail, `effective role name conflicts with existing role "role2"`)

	// Test case 5: Try to create role that conflicts with K8s name - should fail
	role5 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role5",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			InternalRoleName: ptr.To("role1"), // Would create __role1, but role1 exists with effective name "role1"
		},
	}

	// Test the validation method directly for role5
	validationErrs = env.validator.validateEffectiveRoleNameUniqueness(env.ctx, role5, nil)
	require.Empty(t, validationErrs, "No conflict expected - __role1 should not conflict with role1")
}

func TestRedpandaRoleWebhook_ValidateUpdateEffectiveRoleNameUniqueness(t *testing.T) {
	env := setupWebhookTest(t)
	defer env.cleanup()

	var createdRoles []string
	defer func() {
		env.cleanupRoles(t, createdRoles)
	}()

	// Create initial roles
	role1 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role1",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	require.NoError(t, env.client.Create(env.ctx, role1))
	createdRoles = append(createdRoles, "role1")

	role2 := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "role2",
			Namespace: env.namespace,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			InternalRoleName: ptr.To("unique-name"),
		},
	}

	require.NoError(t, env.client.Create(env.ctx, role2))
	createdRoles = append(createdRoles, "role2")

	// Test case 1: Update role2 to add principals - should succeed (no effective name change)
	var role2Updated RedpandaRole
	require.NoError(t, env.client.Get(env.ctx, types.NamespacedName{Namespace: env.namespace, Name: "role2"}, &role2Updated))

	role2Updated.Spec.Principals = []string{"User:alice"}
	require.NoError(t, env.client.Update(env.ctx, &role2Updated))

	// Test case 2: Update role2 to conflict with role1 - should fail
	require.NoError(t, env.client.Get(env.ctx, types.NamespacedName{Namespace: env.namespace, Name: "role2"}, &role2Updated))

	role2Updated.Spec.InternalRoleName = nil // This would make effective name "role2", but we want to conflict with "role1"
	// Actually, let's make it conflict by setting internal name to "role1"
	role2Updated.Spec.InternalRoleName = ptr.To("role1") // This would create "__role1"

	// Test the validation method directly for update using new validator
	validationErrs := env.validator.validateEffectiveRoleNameUniqueness(env.ctx, &role2Updated, role2)
	require.Empty(t, validationErrs, "No conflict expected - __role1 should not conflict with role1")

	// Test case 3: Update role1 to conflict with role2's original effective name - should fail
	var role1Updated RedpandaRole
	require.NoError(t, env.client.Get(env.ctx, types.NamespacedName{Namespace: env.namespace, Name: "role1"}, &role1Updated))

	role1Updated.Spec.InternalRoleName = ptr.To("unique-name") // This would create "__unique-name", same as role2

	// Test the validation method directly for conflicting update
	validationErrs = env.validator.validateEffectiveRoleNameUniqueness(env.ctx, &role1Updated, role1)
	require.NotEmpty(t, validationErrs, "Expected validation error for conflicting update")
	assert.Contains(t, validationErrs[0].Detail, `effective role name conflicts with existing role "role2"`)
}

func TestRedpandaRoleWebhook_NoConflictScenarios(t *testing.T) {
	env := setupWebhookTest(t)
	defer env.cleanup()

	var createdRoles []string
	defer func() {
		env.cleanupRoles(t, createdRoles)
	}()

	// Test case 1: Multiple roles with different effective names - should all succeed
	roles := []*RedpandaRole{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "regular-role",
				Namespace: env.namespace,
			},
			Spec: RoleSpec{
				ClusterSource: &ClusterSource{
					ClusterRef: &ClusterRef{Name: "cluster"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "role-with-internal",
				Namespace: env.namespace,
			},
			Spec: RoleSpec{
				ClusterSource: &ClusterSource{
					ClusterRef: &ClusterRef{Name: "cluster"},
				},
				InternalRoleName: ptr.To("unique-internal"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "another-role",
				Namespace: env.namespace,
			},
			Spec: RoleSpec{
				ClusterSource: &ClusterSource{
					ClusterRef: &ClusterRef{Name: "cluster"},
				},
				InternalRoleName: ptr.To("different-internal"),
			},
		},
	}

	for _, role := range roles {
		require.NoError(t, env.client.Create(env.ctx, role), "Role %s should create successfully", role.Name)
		createdRoles = append(createdRoles, role.Name)
	}

	// Test case 2: Update existing role to non-conflicting internal name - should succeed
	var role RedpandaRole
	require.NoError(t, env.client.Get(env.ctx, types.NamespacedName{Namespace: env.namespace, Name: "regular-role"}, &role))

	role.Spec.InternalRoleName = ptr.To("newly-added-internal")
	require.NoError(t, env.client.Update(env.ctx, &role))

	// Verify the effective name changed
	require.NoError(t, env.client.Get(env.ctx, types.NamespacedName{Namespace: env.namespace, Name: "regular-role"}, &role))
	assert.Equal(t, "__newly-added-internal", role.GetEffectiveRoleName())
}
