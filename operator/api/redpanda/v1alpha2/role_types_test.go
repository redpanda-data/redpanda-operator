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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestRole_GetPrincipal(t *testing.T) {
	role := &Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-role",
		},
	}

	assert.Equal(t, "RedpandaRole:test-role", role.GetPrincipal())
}

func TestRole_GetACLs(t *testing.T) {
	tests := []struct {
		name     string
		role     *Role
		expected []ACLRule
	}{
		{
			name: "role with ACLs",
			role: &Role{
				Spec: RoleSpec{
					Authorization: &RoleAuthorizationSpec{
						ACLs: []ACLRule{
							{
								Type: ACLTypeAllow,
								Resource: ACLResourceSpec{
									Type: ResourceTypeTopic,
									Name: "test-topic",
								},
								Operations: []ACLOperation{ACLOperationRead},
							},
						},
					},
				},
			},
			expected: []ACLRule{
				{
					Type: ACLTypeAllow,
					Resource: ACLResourceSpec{
						Type: ResourceTypeTopic,
						Name: "test-topic",
					},
					Operations: []ACLOperation{ACLOperationRead},
				},
			},
		},
		{
			name: "role without authorization",
			role: &Role{
				Spec: RoleSpec{},
			},
			expected: nil,
		},
		{
			name: "role with nil authorization",
			role: &Role{
				Spec: RoleSpec{
					Authorization: nil,
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.role.GetACLs()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRole_ShouldManageACLs(t *testing.T) {
	tests := []struct {
		name     string
		role     *Role
		expected bool
	}{
		{
			name: "role with authorization",
			role: &Role{
				Spec: RoleSpec{
					Authorization: &RoleAuthorizationSpec{},
				},
			},
			expected: true,
		},
		{
			name: "role without authorization",
			role: &Role{
				Spec: RoleSpec{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.role.ShouldManageACLs()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRole_HasManagedACLs(t *testing.T) {
	tests := []struct {
		name     string
		role     *Role
		expected bool
	}{
		{
			name: "role with managed ACLs",
			role: &Role{
				Status: RoleStatus{
					ManagedACLs: true,
				},
			},
			expected: true,
		},
		{
			name: "role without managed ACLs",
			role: &Role{
				Status: RoleStatus{
					ManagedACLs: false,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.role.HasManagedACLs()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRoleValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	baseRole := Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	for name, tt := range map[string]validationTestCase[*Role]{
		"basic create": {},
		// connection params
		"clusterRef or kafkaApiSpec and adminApiSpec - no cluster source": {
			mutate: func(role *Role) {
				role.Spec.ClusterSource = nil
			},
			errors: []string{`spec.cluster: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - none": {
			mutate: func(role *Role) {
				role.Spec.ClusterSource.ClusterRef = nil
			},
			errors: []string{`either clusterref or staticconfiguration must be set`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - admin api spec": {
			mutate: func(role *Role) {
				role.Spec.ClusterSource.ClusterRef = nil
				role.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Admin: &AdminAPISpec{
						URLs: []string{"http://1.2.3.4:0"},
					},
				}
			},
			errors: []string{`spec.cluster.staticconfiguration.kafka: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka api spec": {
			mutate: func(role *Role) {
				role.Spec.ClusterSource.ClusterRef = nil
				role.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Kafka: &KafkaAPISpec{
						Brokers: []string{"1.2.3.4:0"},
					},
				}
			},
			errors: []string{`spec.cluster.staticconfiguration.admin: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka and admin api spec": {
			mutate: func(role *Role) {
				role.Spec.ClusterSource.ClusterRef = nil
				role.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Kafka: &KafkaAPISpec{
						Brokers: []string{"1.2.3.4:0"},
					},
					Admin: &AdminAPISpec{
						URLs: []string{"http://1.2.3.4:0"},
					},
				}
			},
		},
		// principals
		"principals - valid user principals": {
			mutate: func(role *Role) {
				role.Spec.Principals = []string{"User:john", "User:jane"}
			},
		},
		"principals - user without type prefix": {
			mutate: func(role *Role) {
				role.Spec.Principals = []string{"john", "jane"}
			},
		},
		// authorization (optional)
		"authorization topic": {
			mutate: func(role *Role) {
				role.Spec.Authorization = &RoleAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTopic,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationAlter, ACLOperationAlterConfigs, ACLOperationCreate,
							ACLOperationDelete, ACLOperationDescribe, ACLOperationDescribeConfigs,
							ACLOperationRead, ACLOperationWrite,
						},
					}},
				}
			},
		},
		"authorization topic - invalid operation": {
			mutate: func(role *Role) {
				role.Spec.Authorization = &RoleAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTopic,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationIdempotentWrite,
						},
					}},
				}
			},
			errors: []string{`supported topic operations are ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']`},
		},
		"combined - principals and authorization": {
			mutate: func(role *Role) {
				role.Spec.Principals = []string{"User:john", "User:jane"}
				role.Spec.Authorization = &RoleAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTopic,
							Name: "test-*",
						},
						Operations: []ACLOperation{ACLOperationRead, ACLOperationWrite},
					}},
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			runValidationTest(ctx, t, tt, c, &baseRole)
		})
	}
}

func TestRoleDefaults(t *testing.T) {
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

	// Test role with just principals (Redpanda RBAC mode)
	require.NoError(t, c.Create(ctx, &Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "principals-only",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			Principals: []string{"User:john", "User:jane"},
		},
	}))

	var principalsOnlyRole Role
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "principals-only"}, &principalsOnlyRole))

	require.Len(t, principalsOnlyRole.Status.Conditions, 1)
	require.Equal(t, ResourceConditionTypeSynced, principalsOnlyRole.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionUnknown, principalsOnlyRole.Status.Conditions[0].Status)
	require.Equal(t, ResourceConditionReasonPending, principalsOnlyRole.Status.Conditions[0].Reason)

	require.Equal(t, []string{"User:john", "User:jane"}, principalsOnlyRole.Spec.Principals)
	require.Nil(t, principalsOnlyRole.Spec.Authorization)
	require.False(t, principalsOnlyRole.ShouldManageACLs())

	// Test role with both principals and authorization (Integrated mode)
	require.NoError(t, c.Create(ctx, &Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "with-authorization",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			Principals: []string{"User:alice"},
			Authorization: &RoleAuthorizationSpec{
				ACLs: []ACLRule{{
					Type: ACLTypeAllow,
					Resource: ACLResourceSpec{
						Type: ResourceTypeTopic,
						Name: "test-topic",
					},
					Operations: []ACLOperation{ACLOperationRead},
				}},
			},
		},
	}))

	var authRole Role
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "with-authorization"}, &authRole))

	require.Equal(t, []string{"User:alice"}, authRole.Spec.Principals)
	require.NotNil(t, authRole.Spec.Authorization)
	require.True(t, authRole.ShouldManageACLs())
	require.Len(t, authRole.GetACLs(), 1)

	// Verify defaults are applied to authorization
	require.NotNil(t, authRole.Spec.Authorization.ACLs[0].Host)
	require.Equal(t, "*", *authRole.Spec.Authorization.ACLs[0].Host)
}

func TestRoleImmutableFields(t *testing.T) {
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

	require.NoError(t, c.Create(ctx, &Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: RoleSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}))

	var role Role
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &role))

	role.Spec.ClusterSource.ClusterRef.Name = "other"
	err = c.Update(ctx, &role)

	require.EqualError(t, err, `Role.cluster.redpanda.com "name" is invalid: spec.cluster: Invalid value: "object": ClusterSource is immutable`)

	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &role))
	role.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
		Kafka: &KafkaAPISpec{
			Brokers: []string{"test:123"},
		},
		Admin: &AdminAPISpec{
			URLs: []string{"http://test:123"},
		},
	}
	err = c.Update(ctx, &role)

	require.EqualError(t, err, `Role.cluster.redpanda.com "name" is invalid: spec.cluster: Invalid value: "object": ClusterSource is immutable`)
}

func TestPrincipalsSource_FetchPrincipals(t *testing.T) {
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
		name      string
		setup     func(t *testing.T)
		source    *PrincipalsSource
		expected  []string
		expectErr bool
	}{
		{
			name:     "nil source",
			source:   nil,
			expected: nil,
		},
		{
			name: "JSON array format",
			setup: func(t *testing.T) {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "json-principals",
						Namespace: metav1.NamespaceDefault,
					},
					Data: map[string]string{
						"principals": `["User:alice", "User:bob", "User:charlie"]`,
					},
				}
				require.NoError(t, c.Create(ctx, cm))
			},
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "json-principals",
					},
					Key: "principals",
				},
			},
			expected: []string{"User:alice", "User:bob", "User:charlie"},
		},
		{
			name: "newline-separated format",
			setup: func(t *testing.T) {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "newline-principals",
						Namespace: metav1.NamespaceDefault,
					},
					Data: map[string]string{
						"principals": "User:alice\nUser:bob\nUser:charlie",
					},
				}
				require.NoError(t, c.Create(ctx, cm))
			},
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "newline-principals",
					},
					Key: "principals",
				},
			},
			expected: []string{"User:alice", "User:bob", "User:charlie"},
		},
		{
			name: "newline-separated with comments and empty lines",
			setup: func(t *testing.T) {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "principals-with-comments",
						Namespace: metav1.NamespaceDefault,
					},
					Data: map[string]string{
						"principals": "User:alice\n# This is a comment\nUser:bob\n\nUser:charlie\n  ",
					},
				}
				require.NoError(t, c.Create(ctx, cm))
			},
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "principals-with-comments",
					},
					Key: "principals",
				},
			},
			expected: []string{"User:alice", "User:bob", "User:charlie"},
		},
		{
			name: "default key name",
			setup: func(t *testing.T) {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-key",
						Namespace: metav1.NamespaceDefault,
					},
					Data: map[string]string{
						"principals": `["User:default"]`,
					},
				}
				require.NoError(t, c.Create(ctx, cm))
			},
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "default-key",
					},
					// Key not specified, should default to "principals"
				},
			},
			expected: []string{"User:default"},
		},
		{
			name: "configmap not found",
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "nonexistent",
					},
					Key: "principals",
				},
			},
			expectErr: true,
		},
		{
			name: "key not found in configmap",
			setup: func(t *testing.T) {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "wrong-key",
						Namespace: metav1.NamespaceDefault,
					},
					Data: map[string]string{
						"other-key": `["User:test"]`,
					},
				}
				require.NoError(t, c.Create(ctx, cm))
			},
			source: &PrincipalsSource{
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "wrong-key",
					},
					Key: "principals",
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			result, err := tt.source.FetchPrincipals(ctx, c, metav1.NamespaceDefault)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestRoleSpec_GetPrincipals(t *testing.T) {
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

	// Setup ConfigMap for testing
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-principals",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string]string{
			"principals": `["User:from-configmap", "User:also-from-cm"]`,
		},
	}
	require.NoError(t, c.Create(ctx, cm))

	tests := []struct {
		name      string
		spec      *RoleSpec
		expected  []string
		expectErr bool
	}{
		{
			name: "only inline principals",
			spec: &RoleSpec{
				Principals: []string{"User:alice", "User:bob"},
			},
			expected: []string{"User:alice", "User:bob"},
		},
		{
			name: "only configmap principals",
			spec: &RoleSpec{
				PrincipalsFrom: &PrincipalsSource{
					ConfigMapRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-principals",
						},
						Key: "principals",
					},
				},
			},
			expected: []string{"User:from-configmap", "User:also-from-cm"},
		},
		{
			name: "merged inline and configmap principals",
			spec: &RoleSpec{
				Principals: []string{"User:inline1", "User:inline2"},
				PrincipalsFrom: &PrincipalsSource{
					ConfigMapRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-principals",
						},
						Key: "principals",
					},
				},
			},
			expected: []string{"User:inline1", "User:inline2", "User:from-configmap", "User:also-from-cm"},
		},
		{
			name:     "no principals",
			spec:     &RoleSpec{},
			expected: nil,
		},
		{
			name: "configmap error propagates",
			spec: &RoleSpec{
				Principals: []string{"User:inline"},
				PrincipalsFrom: &PrincipalsSource{
					ConfigMapRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "nonexistent",
						},
						Key: "principals",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "duplicates are removed",
			spec: &RoleSpec{
				Principals: []string{"User:alice", "User:bob", "User:alice"},
				PrincipalsFrom: &PrincipalsSource{
					ConfigMapRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-principals",
						},
						Key: "principals",
					},
				},
			},
			// test-principals contains: ["User:from-configmap", "User:also-from-cm"]
			// We also have duplicate "User:alice" in inline
			// Final result should deduplicate to just unique values
			expected: []string{"User:alice", "User:bob", "User:from-configmap", "User:also-from-cm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.spec.GetPrincipals(ctx, c, metav1.NamespaceDefault)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
