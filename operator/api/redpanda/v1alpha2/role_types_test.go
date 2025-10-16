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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestRole_GetPrincipal(t *testing.T) {
	role := &RedpandaRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-role",
		},
	}

	assert.Equal(t, "RedpandaRole:test-role", role.GetPrincipal())
}

func TestRole_GetACLs(t *testing.T) {
	tests := []struct {
		name     string
		role     *RedpandaRole
		expected []ACLRule
	}{
		{
			name: "role with ACLs",
			role: &RedpandaRole{
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
			role: &RedpandaRole{
				Spec: RoleSpec{},
			},
			expected: nil,
		},
		{
			name: "role with nil authorization",
			role: &RedpandaRole{
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
		role     *RedpandaRole
		expected bool
	}{
		{
			name: "role with authorization",
			role: &RedpandaRole{
				Spec: RoleSpec{
					Authorization: &RoleAuthorizationSpec{},
				},
			},
			expected: true,
		},
		{
			name: "role without authorization",
			role: &RedpandaRole{
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
		role     *RedpandaRole
		expected bool
	}{
		{
			name: "role with managed ACLs",
			role: &RedpandaRole{
				Status: RoleStatus{
					ManagedACLs: true,
				},
			},
			expected: true,
		},
		{
			name: "role without managed ACLs",
			role: &RedpandaRole{
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

	baseRole := RedpandaRole{
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

	for name, tt := range map[string]validationTestCase[*RedpandaRole]{
		"basic create": {},
		// connection params
		"clusterRef or kafkaApiSpec and adminApiSpec - no cluster source": {
			mutate: func(role *RedpandaRole) {
				role.Spec.ClusterSource = nil
			},
			errors: []string{`spec.cluster: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - none": {
			mutate: func(role *RedpandaRole) {
				role.Spec.ClusterSource.ClusterRef = nil
			},
			errors: []string{`either clusterref or staticconfiguration must be set`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - admin api spec": {
			mutate: func(role *RedpandaRole) {
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
			mutate: func(role *RedpandaRole) {
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
			mutate: func(role *RedpandaRole) {
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
			mutate: func(role *RedpandaRole) {
				role.Spec.Principals = []string{"User:john", "User:jane"}
			},
		},
		"principals - user without type prefix": {
			mutate: func(role *RedpandaRole) {
				role.Spec.Principals = []string{"john", "jane"}
			},
		},
		// authorization (optional)
		"authorization topic": {
			mutate: func(role *RedpandaRole) {
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
			mutate: func(role *RedpandaRole) {
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
			mutate: func(role *RedpandaRole) {
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
	require.NoError(t, c.Create(ctx, &RedpandaRole{
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

	var principalsOnlyRole RedpandaRole
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "principals-only"}, &principalsOnlyRole))

	require.Len(t, principalsOnlyRole.Status.Conditions, 1)
	require.Equal(t, ResourceConditionTypeSynced, principalsOnlyRole.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionUnknown, principalsOnlyRole.Status.Conditions[0].Status)
	require.Equal(t, ResourceConditionReasonPending, principalsOnlyRole.Status.Conditions[0].Reason)

	require.Equal(t, []string{"User:john", "User:jane"}, principalsOnlyRole.Spec.Principals)
	require.Nil(t, principalsOnlyRole.Spec.Authorization)
	require.False(t, principalsOnlyRole.ShouldManageACLs())

	// Test role with both principals and authorization (Integrated mode)
	require.NoError(t, c.Create(ctx, &RedpandaRole{
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

	var authRole RedpandaRole
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

	require.NoError(t, c.Create(ctx, &RedpandaRole{
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

	var role RedpandaRole
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &role))

	role.Spec.ClusterSource.ClusterRef.Name = "other"
	err = c.Update(ctx, &role)

	require.EqualError(t, err, `RedpandaRole.cluster.redpanda.com "name" is invalid: spec.cluster: Invalid value: "object": ClusterSource is immutable`)

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

	require.EqualError(t, err, `RedpandaRole.cluster.redpanda.com "name" is invalid: spec.cluster: Invalid value: "object": ClusterSource is immutable`)
}
