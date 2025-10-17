// Copyright 2025 Redpanda Data, Inc.
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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestRoleBindingPrincipalFormats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	tests := []struct {
		name       string
		principals []string
	}{
		{
			name:       "user with prefix",
			principals: []string{"User:john"},
		},
		{
			name:       "multiple users with prefix",
			principals: []string{"User:john", "User:jane", "User:alice"},
		},
		{
			name:       "user without prefix",
			principals: []string{"john"},
		},
		{
			name:       "multiple users without prefix",
			principals: []string{"john", "jane", "bob"},
		},
		{
			name:       "mixed with and without prefix",
			principals: []string{"User:john", "jane", "User:alice", "bob"},
		},
		{
			name:       "email addresses",
			principals: []string{"john@example.com", "jane@company.org"},
		},
		{
			name:       "email with User prefix",
			principals: []string{"User:john@example.com", "User:jane@company.org"},
		},
		{
			name:       "mixed usernames and emails",
			principals: []string{"User:john", "jane@example.com", "bob", "User:alice@company.org"},
		},
	}

	for i, tt := range tests {
		tt := tt
		i := i
		t.Run(tt.name, func(t *testing.T) {
			// Generate a valid Kubernetes resource name (lowercase, alphanumeric and dashes only)
			rbName := fmt.Sprintf("test-binding-%d", i)

			rb := &RedpandaRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbName,
					Namespace: metav1.NamespaceDefault,
				},
				Spec: RedpandaRoleBindingSpec{
					RoleRef: RoleRef{
						Name: "test-role",
					},
					Principals: tt.principals,
				},
			}

			err := c.Create(ctx, rb)
			require.NoError(t, err, "Should be able to create RoleBinding with principals: %v", tt.principals)

			// Verify it was created with the correct principals
			var fetched RedpandaRoleBinding
			err = c.Get(ctx, types.NamespacedName{Namespace: rb.Namespace, Name: rb.Name}, &fetched)
			require.NoError(t, err)
			require.Equal(t, tt.principals, fetched.Spec.Principals)
		})
	}
}

func TestRoleBindingCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	// Test RoleBinding with principals
	require.NoError(t, c.Create(ctx, &RedpandaRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-binding-with-principals",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RedpandaRoleBindingSpec{
			RoleRef: RoleRef{
				Name: "test-role",
			},
			Principals: []string{"User:john", "User:jane"},
		},
	}))

	var rb RedpandaRoleBinding
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "test-binding-with-principals"}, &rb))

	require.Equal(t, "test-role", rb.Spec.RoleRef.Name)
	require.Equal(t, []string{"User:john", "User:jane"}, rb.Spec.Principals)

	// Test RoleBinding without principals - should fail validation (MinItems=1)
	err = c.Create(ctx, &RedpandaRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-binding-no-principals",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RedpandaRoleBindingSpec{
			RoleRef: RoleRef{
				Name: "another-role",
			},
			Principals: []string{}, // Empty array should be rejected
		},
	})
	require.Error(t, err, "RoleBinding with empty principals should fail validation")
	require.Contains(t, err.Error(), "spec.principals")
}
