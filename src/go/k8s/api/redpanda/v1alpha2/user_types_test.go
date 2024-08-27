package v1alpha2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
)

func TestUserValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	baseUser := User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: UserSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	password := Password{
		ValueFrom: &PasswordSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "key",
				},
			},
		},
	}

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	for name, tt := range map[string]struct {
		mutate func(user *User)
		errors []string
	}{
		"basic create": {},
		// connection params
		"clusterRef or kafkaApiSpec and adminApiSpec - no cluster source": {
			mutate: func(user *User) {
				user.Spec.ClusterSource = nil
			},
			errors: []string{`spec.cluster: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - none": {
			mutate: func(user *User) {
				user.Spec.ClusterSource.ClusterRef = nil
			},
			errors: []string{`either clusterref or staticconfiguration must be set`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - admin api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterSource.ClusterRef = nil
				user.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Admin: &AdminAPISpec{
						URLs: []string{"http://1.2.3.4:0"},
					},
				}
			},
			errors: []string{`spec.cluster.staticconfiguration.kafka: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterSource.ClusterRef = nil
				user.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Kafka: &KafkaAPISpec{
						Brokers: []string{"1.2.3.4:0"},
					},
				}
			},
			errors: []string{`spec.cluster.staticconfiguration.admin: required value`},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka and admin api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterSource.ClusterRef = nil
				user.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Kafka: &KafkaAPISpec{
						Brokers: []string{"1.2.3.4:0"},
					},
					Admin: &AdminAPISpec{
						URLs: []string{"http://1.2.3.4:0"},
					},
				}
			},
		},
		// authentication
		"authentication type - default": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Password: password,
				}
			},
		},
		"authentication type - sha-512": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type:     ptr.To(SASLMechanismScramSHA512),
					Password: password,
				}
			},
		},
		"authentication type - sha-256": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type:     ptr.To(SASLMechanismScramSHA256),
					Password: password,
				}
			},
		},
		"authentication type - invalid": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type: ptr.To(SASLMechanismGSSAPI),
				}
			},
			errors: []string{`spec.authentication.type: Unsupported value: "gssapi": supported values: "scram-sha-256", "scram-sha-512"`},
		},
		"authentication - no password value from": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{}
			},
			errors: []string{`spec.authentication.password.valueFrom: Required value`},
		},
		"authentication - no secret key ref": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Password: Password{
						ValueFrom: &PasswordSource{},
					},
				}
			},
			errors: []string{`spec.authentication.password.valueFrom.secretKeyRef: Required value`},
		},
		// authorization
		"authorization topic": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
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
		"authorization topic - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTopic,
						},
						Operations: []ACLOperation{
							ACLOperationAlter, ACLOperationAlterConfigs, ACLOperationCreate,
							ACLOperationDelete, ACLOperationDescribe, ACLOperationDescribeConfigs,
							ACLOperationRead, ACLOperationWrite,
						},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization topic - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
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
		"authorization group": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeGroup,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationDelete, ACLOperationDescribe, ACLOperationRead,
						},
					}},
				}
			},
		},
		"authorization group - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeGroup,
						},
						Operations: []ACLOperation{
							ACLOperationDelete, ACLOperationDescribe, ACLOperationRead,
						},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization group - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeGroup,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationIdempotentWrite,
						},
					}},
				}
			},
			errors: []string{`supported group operations are ['Delete', 'Describe', 'Read']`},
		},
		"authorization transactionalId": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTransactionalID,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationDescribe, ACLOperationWrite,
						},
					}},
				}
			},
		},
		"authorization transactionalId - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTransactionalID,
						},
						Operations: []ACLOperation{
							ACLOperationDescribe, ACLOperationWrite,
						},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization transactionalId - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeTransactionalID,
							Name: "foo",
						},
						Operations: []ACLOperation{
							ACLOperationIdempotentWrite,
						},
					}},
				}
			},
			errors: []string{`supported transactionalId operations are ['Describe', 'Write']`},
		},
		"authorization cluster": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeCluster,
						},
						Operations: []ACLOperation{
							ACLOperationAlter, ACLOperationAlterConfigs, ACLOperationClusterAction, ACLOperationCreate,
							ACLOperationDescribe, ACLOperationDescribeConfigs, ACLOperationIdempotentWrite,
						},
					}},
				}
			},
		},
		"authorization cluster - resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeCluster,
							Name: "cluster",
						},
						Operations: []ACLOperation{
							ACLOperationAlter, ACLOperationAlterConfigs, ACLOperationClusterAction, ACLOperationCreate,
							ACLOperationDescribe, ACLOperationDescribeConfigs, ACLOperationIdempotentWrite,
						},
					}},
				}
			},
			errors: []string{`name must not be specified for type ['cluster']`},
		},
		"authorization cluster - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type: ResourceTypeCluster,
						},
						Operations: []ACLOperation{
							ACLOperationDelete,
						},
					}},
				}
			},
			errors: []string{`supported cluster operations are ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']`},
		},
		"authorization cluster - invalid patternType prefixed": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeAllow,
						Resource: ACLResourceSpec{
							Type:        ResourceTypeCluster,
							PatternType: ptr.To(PatternTypePrefixed),
						},
						Operations: []ACLOperation{
							ACLOperationAlter,
						},
					}},
				}
			},
			errors: []string{`prefixed pattern type only supported for ['group', 'topic', 'transactionalId']`},
		},
		"authorization - deny": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: ACLTypeDeny,
						Resource: ACLResourceSpec{
							Type: ResourceTypeCluster,
						},
						Operations: []ACLOperation{
							ACLOperationAlter,
						},
					}},
				}
			},
		},
		"authorization - no operations": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "deny",
						Resource: ACLResourceSpec{
							Type: "cluster",
						},
					}},
				}
			},
			errors: []string{`spec.authorization.acls[0].operations: Required value`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := baseUser.DeepCopy()
			user.Name = fmt.Sprintf("name-%v", time.Now().UnixNano())

			if tt.mutate != nil {
				tt.mutate(user)
			}
			err := c.Create(ctx, user)

			if len(tt.errors) != 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			for _, expected := range tt.errors {
				assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(expected))
			}
		})
	}
}

func TestUserDefaults(t *testing.T) {
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

	require.NoError(t, c.Create(ctx, &User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: UserSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			Authentication: &UserAuthenticationSpec{
				Password: Password{
					ValueFrom: &PasswordSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "key",
							},
						},
					},
				},
			},
			Authorization: &UserAuthorizationSpec{
				ACLs: []ACLRule{{
					Type: ACLTypeAllow,
					Resource: ACLResourceSpec{
						Type: ResourceTypeTopic,
						Name: "something",
					},
					Operations: []ACLOperation{ACLOperationRead},
				}},
			},
		},
	}))

	var user User
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &user))

	require.Len(t, user.Status.Conditions, 1)
	require.Equal(t, UserConditionTypeSynced, user.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionUnknown, user.Status.Conditions[0].Status)
	require.Equal(t, UserConditionReasonPending, user.Status.Conditions[0].Reason)

	require.NotNil(t, user.Spec.Authentication.Type)
	require.True(t, user.Spec.Authentication.Type.Equals(SASLMechanismScramSHA512))

	require.NotNil(t, user.Spec.Authorization.Type)
	require.Equal(t, AuthorizationTypeSimple, *user.Spec.Authorization.Type)

	require.NotNil(t, user.Spec.Authorization.ACLs[0].Host)
	require.Equal(t, "*", *user.Spec.Authorization.ACLs[0].Host)

	require.NotNil(t, user.Spec.Authorization.ACLs[0].Resource.PatternType)
	require.Equal(t, PatternTypeLiteral, *user.Spec.Authorization.ACLs[0].Resource.PatternType)
}

func TestUserImmutableFields(t *testing.T) {
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

	require.NoError(t, c.Create(ctx, &User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: UserSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}))

	var user User
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &user))

	user.Spec.ClusterSource.ClusterRef.Name = "other"
	err = c.Update(ctx, &user)

	require.EqualError(t, err, `User.cluster.redpanda.com "name" is invalid: spec.cluster.clusterRef: Invalid value: "object": clusterRef is immutable`)
}
