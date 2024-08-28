package client

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestUserClientACLs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = v1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "password",
			Namespace: metav1.NamespaceDefault,
		},
		StringData: map[string]string{
			"password": "password",
		},
	}))

	user := &v1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "user",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha2.UserSpec{
			ClusterSource: &v1alpha2.ClusterSource{
				StaticConfiguration: &v1alpha2.StaticConfigurationSource{
					Kafka: &v1alpha2.KafkaAPISpec{
						Brokers: []string{broker},
						SASL: &v1alpha2.KafkaSASL{
							Username: "user",
							Password: v1alpha2.SecretKeyRef{
								Name: "password",
								Key:  "password",
							},
							Mechanism: v1alpha2.SASLMechanismScramSHA256,
						},
					},
					Admin: &v1alpha2.AdminAPISpec{
						URLs: []string{admin},
						SASL: &v1alpha2.AdminSASL{
							Username: "user",
							Password: v1alpha2.SecretKeyRef{
								Name: "password",
								Key:  "password",
							},
							Mechanism: v1alpha2.SASLMechanismScramSHA256,
						},
					},
				},
			},
		},
	}

	sortACLs := func(acls []v1alpha2.ACLRule) {
		sort.SliceStable(acls, func(i, j int) bool {
			return acls[i].Resource.Type < acls[j].Resource.Type
		})
	}

	expectACLsMatch := func(t *testing.T, acls []v1alpha2.ACLRule) {
		userClient, err := NewFactory(cfg, c).UserClient(ctx, user)
		require.NoError(t, err)

		describeResponse, err := userClient.ListACLs(ctx)
		require.NoError(t, err)

		actual := aclRuleSetFromDescribeResponse(describeResponse).AsRules()
		require.Len(t, actual, len(acls))

		sortACLs(acls)
		sortACLs(actual)

		for i := 0; i < len(actual); i++ {
			require.True(t, actual[i].Equals(acls[i]), fmt.Sprintf("%+v != %+v", actual[i], acls[i]))
		}
	}

	expectACLUpdate := func(t *testing.T, acls []v1alpha2.ACLRule, expectCreated, expectDeleted int) {
		t.Helper()

		user.Spec.Authorization = &v1alpha2.UserAuthorizationSpec{
			ACLs: acls,
		}
		userClient, err := NewFactory(cfg, c).UserClient(ctx, user)
		require.NoError(t, err)

		created, deleted, err := userClient.SyncACLs(ctx)
		require.NoError(t, err)

		require.Equal(t, expectCreated, created)
		require.Equal(t, expectDeleted, deleted)

		expectACLsMatch(t, acls)
	}

	initialACLS := []v1alpha2.ACLRule{{
		Type: v1alpha2.ACLTypeAllow,
		Resource: v1alpha2.ACLResourceSpec{
			Type: v1alpha2.ResourceTypeTopic,
			Name: "1",
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationRead,
		},
	}, {
		Type: v1alpha2.ACLTypeAllow,
		Resource: v1alpha2.ACLResourceSpec{
			Type: v1alpha2.ResourceTypeCluster,
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationRead,
		},
	}}

	// create initial acls
	expectACLUpdate(t, initialACLS, 2, 0)

	// remove 1 acl
	expectACLUpdate(t, []v1alpha2.ACLRule{{
		Type: v1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: v1alpha2.ACLResourceSpec{
			Type: v1alpha2.ResourceTypeCluster,
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationRead,
		},
	}}, 0, 1)

	// update acl
	expectACLUpdate(t, []v1alpha2.ACLRule{{
		Type: v1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: v1alpha2.ACLResourceSpec{
			Type: v1alpha2.ResourceTypeCluster,
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationClusterAction,
		},
	}}, 1, 1)

	// delete all acls
	expectACLUpdate(t, []v1alpha2.ACLRule{}, 0, 1)

	// check de-duplication of ACLs
	user.Spec.Authorization = &v1alpha2.UserAuthorizationSpec{
		ACLs: append(initialACLS, initialACLS[0]),
	}
	userClient, err := NewFactory(cfg, c).UserClient(ctx, user)
	require.NoError(t, err)
	created, deleted, err := userClient.SyncACLs(ctx)
	require.NoError(t, err)
	require.Equal(t, created, 2)
	require.Equal(t, deleted, 0)
	expectACLsMatch(t, initialACLS)

	// clear all
	err = userClient.DeleteAllACLs(ctx)
	require.NoError(t, err)
	expectACLsMatch(t, []v1alpha2.ACLRule{})
}
