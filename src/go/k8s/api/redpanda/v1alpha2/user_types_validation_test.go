package v1alpha2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
)

func TestValidateUser(t *testing.T) {
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
			ClusterRef: &ClusterRef{
				Name: "cluster",
			},
		},
	}

	password := Password{
		ValueFrom: &PasswordSource{
			SecretKeyRef: &SecretKeyRef{
				Name: "key",
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
		"clusterRef or kafkaApiSpec and adminApiSpec - none": {
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
			},
			errors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - admin api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.AdminAPISpec = &AdminAPISpec{
					URLs: []string{"http://1.2.3.4:0"},
				}
			},
			errors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.KafkaAPISpec = &KafkaAPISpec{
					Brokers: []string{"1.2.3.4:0"},
				}
			},
			errors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		"clusterRef or kafkaApiSpec and adminApiSpec - kafka and admin api spec": {
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.KafkaAPISpec = &KafkaAPISpec{
					Brokers: []string{"1.2.3.4:0"},
				}
				user.Spec.AdminAPISpec = &AdminAPISpec{
					URLs: []string{"http://1.2.3.4:0"},
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
					Type:     ptr.To("scram-sha-512"),
					Password: password,
				}
			},
		},
		"authentication type - sha-256": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type:     ptr.To("scram-sha-256"),
					Password: password,
				}
			},
		},
		"authentication type - invalid": {
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type: ptr.To("invalid"),
				}
			},
			errors: []string{`spec.authentication.type: Unsupported value: "invalid": supported values: "scram-sha-256", "scram-sha-512"`},
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
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "topic",
							Name: "foo",
						},
						Operations: []string{"Alter", "AlterConfigs", "Create", "Delete", "Describe", "DescribeConfigs", "Read", "Write"},
					}},
				}
			},
		},
		"authorization topic - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "topic",
						},
						Operations: []string{"Alter", "AlterConfigs", "Create", "Delete", "Describe", "DescribeConfigs", "Read", "Write"},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization topic - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "topic",
							Name: "foo",
						},
						Operations: []string{"IdempotentWrite"},
					}},
				}
			},
			errors: []string{`supported topic operations are ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']`},
		},
		"authorization group": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "group",
							Name: "foo",
						},
						Operations: []string{"Delete", "Describe", "Read"},
					}},
				}
			},
		},
		"authorization group - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "group",
						},
						Operations: []string{"Delete", "Describe", "Read"},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization group - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "group",
							Name: "foo",
						},
						Operations: []string{"IdempotentWrite"},
					}},
				}
			},
			errors: []string{`supported group operations are ['Delete', 'Describe', 'Read']`},
		},
		"authorization delegationToken": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "delegationToken",
							Name: "foo",
						},
						Operations: []string{"Describe"},
					}},
				}
			},
		},
		"authorization delegationToken - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "delegationToken",
						},
						Operations: []string{"Describe"},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization delegationToken - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "delegationToken",
							Name: "foo",
						},
						Operations: []string{"IdempotentWrite"},
					}},
				}
			},
			errors: []string{`supported delegationToken operations are ['Describe']`},
		},
		"authorization transactionalId": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "transactionalId",
							Name: "foo",
						},
						Operations: []string{"Describe", "Write"},
					}},
				}
			},
		},
		"authorization transactionalId - no resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "transactionalId",
						},
						Operations: []string{"Describe", "Write"},
					}},
				}
			},
			errors: []string{`acl rules on non-cluster resources must specify a name`},
		},
		"authorization transactionalId - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "transactionalId",
							Name: "foo",
						},
						Operations: []string{"IdempotentWrite"},
					}},
				}
			},
			errors: []string{`supported transactionalId operations are ['Describe', 'Write']`},
		},
		"authorization cluster": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "cluster",
						},
						Operations: []string{"Alter", "AlterConfigs", "ClusterAction", "Create", "Describe", "DescribeConfigs", "IdempotentWrite"},
					}},
				}
			},
		},
		"authorization cluster - resource name": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "cluster",
							Name: "cluster",
						},
						Operations: []string{"Alter", "AlterConfigs", "ClusterAction", "Create", "Describe", "DescribeConfigs", "IdempotentWrite"},
					}},
				}
			},
			errors: []string{`name must not be specified for type ['cluster']`},
		},
		"authorization cluster - invalid operation": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "allow",
						Resource: ACLResourceSpec{
							Type: "cluster",
						},
						Operations: []string{"Delete"},
					}},
				}
			},
			errors: []string{`supported cluster operations are ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']`},
		},
		"authorization - deny": {
			mutate: func(user *User) {
				user.Spec.Authorization = &UserAuthorizationSpec{
					ACLs: []ACLRule{{
						Type: "deny",
						Resource: ACLResourceSpec{
							Type: "cluster",
						},
						Operations: []string{"Alter"},
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
