package redpanda

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestUserReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	server := &envtest.APIServer{}
	etcd := &envtest.Etcd{}

	testEnv := testutils.RedpandaTestEnv{
		Environment: envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: server,
				Etcd:      etcd,
			},
		},
	}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Cleanup(func() {
		_ = testEnv.Stop()
	})

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("superuser"),
		redpanda.WithNewServiceAccount("superuser", "password"),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	err = redpandav1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	factory := internalclient.NewFactory(cfg, c)

	// ensure we have a secret which we can pull a password from
	err = c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "superuser",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"password": []byte("password"),
		},
	})
	require.NoError(t, err)

	err = c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalidsuperuser",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"password": []byte("invalid"),
		},
	})
	require.NoError(t, err)

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)

	reconciler := UserReconciler{
		Client:        c,
		ClientFactory: factory,
		extraOptions:  []kgo.Opt{timeoutOption},
	}

	authenticationSpec := &redpandav1alpha2.UserAuthenticationSpec{
		Password: redpandav1alpha2.Password{
			Value: "password",
			ValueFrom: &redpandav1alpha2.PasswordSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "password",
					},
				},
			},
		},
	}

	authorizationSpec := &redpandav1alpha2.UserAuthorizationSpec{
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

	validClusterSource := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{broker},
				SASL: &redpandav1alpha2.KafkaSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "superuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{admin},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "superuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
		},
	}

	invalidAuthClusterSourceBadPassword := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{broker},
				SASL: &redpandav1alpha2.KafkaSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "invalidsuperuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{admin},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "invalidsuperuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
		},
	}

	invalidAuthClusterSourceNoSASL := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{broker},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{admin},
			},
		},
	}

	invalidClusterRefSource := &redpandav1alpha2.ClusterSource{
		ClusterRef: &redpandav1alpha2.ClusterRef{
			Name: "nonexistent",
		},
	}

	baseUser := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource:  validClusterSource,
			Authentication: authenticationSpec,
			Authorization:  authorizationSpec,
		},
	}

	syncedClusterRefCondition := redpandav1alpha2.UserSyncedCondition("test")

	invalidClusterRefCondition := redpandav1alpha2.UserNotSyncedCondition(
		redpandav1alpha2.UserConditionReasonClusterRefInvalid, errors.New("test"),
	)

	clientErrorCondition := redpandav1alpha2.UserNotSyncedCondition(
		redpandav1alpha2.UserConditionReasonTerminalClientError, errors.New("test"),
	)

	for name, tt := range map[string]struct {
		mutate            func(user *redpandav1alpha2.User)
		expectedCondition metav1.Condition
		onlyCheckDeletion bool
	}{
		"success - authorization and authentication": {
			expectedCondition: syncedClusterRefCondition,
		},
		"success - authorization and authentication deletion cleanup": {
			expectedCondition: syncedClusterRefCondition,
			onlyCheckDeletion: true,
		},
		"success - authentication": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authorization = nil
			},
			expectedCondition: syncedClusterRefCondition,
		},
		"success - authentication deletion cleanup": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authorization = nil
			},
			expectedCondition: syncedClusterRefCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authentication = nil
			},
			expectedCondition: syncedClusterRefCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization deletion cleanup": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authentication = nil
			},
			expectedCondition: syncedClusterRefCondition,
			onlyCheckDeletion: true,
		},
		"error - invalid cluster ref": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = invalidClusterRefSource
			},
			expectedCondition: invalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = invalidAuthClusterSourceNoSASL
			},
			expectedCondition: clientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = invalidAuthClusterSourceBadPassword
			},
			expectedCondition: clientErrorCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := baseUser.DeepCopy()
			user.Name = "user" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(user)
			}

			key := client.ObjectKeyFromObject(user)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, c.Create(ctx, user))
			_, err = reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, c.Get(ctx, key, user))
			require.Equal(t, []string{FinalizerKey}, user.Finalizers)
			require.Len(t, user.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, user.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, user.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, user.Status.Conditions[0].Reason)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				syncer, err := factory.ACLs(ctx, user)
				require.NoError(t, err)
				userClient, err := factory.Users(ctx, user)
				require.NoError(t, err)

				// if we're supposed to have synced, then check to make sure we properly
				// set the management flags
				require.Equal(t, user.ShouldManageUser(), user.Status.ManagedUser)
				require.Equal(t, user.ShouldManageACLs(), user.Status.ManagedACLs)

				if user.ShouldManageUser() {
					// make sure we actually have a user
					hasUser, err := userClient.Has(ctx, user)
					require.NoError(t, err)
					require.True(t, hasUser)
				}

				if user.ShouldManageACLs() {
					// make sure we actually have a user
					acls, err := syncer.ListACLs(ctx, user.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 1)
				}

				if user.ShouldManageUser() {
					kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(broker), timeoutOption, kgo.SASL(scram.Auth{
						User: user.Name,
						Pass: "password",
					}.AsSha512Mechanism()))
					require.NoError(t, err)
					kafkaAdminClient := kadm.NewClient(kafkaClient)

					// first do an operation that anyone can do just to make
					// sure we can authenticate
					_, err = kafkaAdminClient.BrokerMetadata(ctx)
					require.NoError(t, err)

					_, err = kafkaAdminClient.DescribeGroups(ctx, "group")
					// check to make sure we have an error based on
					// whether we're able to do this privileged operation
					if user.ShouldManageACLs() {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				}

				if !tt.onlyCheckDeletion {
					if user.ShouldManageUser() {
						// now clear out any managed User and re-check
						user.Spec.Authentication = nil
						require.NoError(t, c.Update(ctx, user))
						_, err = reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, c.Get(ctx, key, user))
						require.False(t, user.Status.ManagedUser)
					}

					// make sure we no longer have a user
					hasUser, err := userClient.Has(ctx, user)
					require.NoError(t, err)
					require.False(t, hasUser)

					if user.ShouldManageACLs() {
						// now clear out any managed ACLs and re-check
						user.Spec.Authorization = nil
						require.NoError(t, c.Update(ctx, user))
						_, err = reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, c.Get(ctx, key, user))
						require.False(t, user.Status.ManagedACLs)
					}

					// make sure we no longer have acls
					acls, err := syncer.ListACLs(ctx, user.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, c.Delete(ctx, user))
				_, err = reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(c.Get(ctx, key, user)))

				// make sure we no longer have a user
				hasUser, err := userClient.Has(ctx, user)
				require.NoError(t, err)
				require.False(t, hasUser)

				// make sure we no longer have acls
				acls, err := syncer.ListACLs(ctx, user.GetPrincipal())
				require.NoError(t, err)
				require.Len(t, acls, 0)

				return
			}

			// clean up and make sure we properly delete everything
			require.NoError(t, c.Delete(ctx, user))
			_, err = reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(c.Get(ctx, key, user)))
		})
	}
}
