// Copyright 2026 Redpanda Data, Inc.
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
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestUserAdoptExisting verifies that a User CR applied for an already-existing
// Redpanda user is adopted (managedUser becomes true) instead of being left
// unmanaged. This is the fix for
// https://github.com/redpanda-data/redpanda-operator/issues/1354.
func TestUserAdoptExisting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &UserReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
	require.NoError(t, err)

	userName := "adopt-user-" + strconv.Itoa(int(time.Now().UnixNano()))

	// Step 1: Pre-create the user directly in Redpanda via the Kafka admin
	// API, simulating a user that existed before the operator was deployed.
	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(environment.KafkaURL), timeoutOption, kgo.SASL(scram.Auth{
		User: "superuser",
		Pass: "password",
	}.AsSha256Mechanism()))
	require.NoError(t, err)
	defer kafkaClient.Close()

	adminClient := kadm.NewClient(kafkaClient)
	_, err = adminClient.AlterUserSCRAMs(ctx, nil, []kadm.UpsertSCRAM{{
		User:       userName,
		Password:   "original-password",
		Mechanism:  kadm.ScramSha512,
		Iterations: 4096,
	}})
	require.NoError(t, err)

	// Step 2: Apply a User CR for this pre-existing user.
	user := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userName,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: environment.ClusterSourceValid,
			Authentication: &redpandav1alpha2.UserAuthenticationSpec{
				Password: redpandav1alpha2.Password{
					Value: "adopted-password",
					ValueFrom: &redpandav1alpha2.PasswordSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: userName + "-password",
							},
						},
					},
				},
			},
		},
	}

	key := client.ObjectKeyFromObject(user)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	require.NoError(t, k8sClient.Create(ctx, user))

	// Reconcile twice: first adds finalizer, second does sync.
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, key, user))
	require.True(t, user.Status.ManagedUser, "expected managedUser=true after adopting existing user")
	require.Equal(t, metav1.ConditionTrue, user.Status.Conditions[0].Status)

	// Verify we can authenticate with the new password.
	verifyClient, err := kgo.NewClient(kgo.SeedBrokers(environment.KafkaURL), timeoutOption, kgo.SASL(scram.Auth{
		User: userName,
		Pass: "adopted-password",
	}.AsSha512Mechanism()))
	require.NoError(t, err)
	defer verifyClient.Close()
	verifyAdmin := kadm.NewClient(verifyClient)
	_, err = verifyAdmin.BrokerMetadata(ctx)
	require.NoError(t, err)

	// Cleanup.
	require.NoError(t, k8sClient.Delete(ctx, user))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
}

// TestUserCredentialSync verifies that when syncCredentials is enabled, updating
// the password Secret causes the operator to push the new password to Redpanda
// on the next reconciliation cycle.
func TestUserCredentialSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &UserReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
	require.NoError(t, err)

	userName := "sync-user-" + strconv.Itoa(int(time.Now().UnixNano()))
	secretName := userName + "-password"

	// Step 1: Create the password Secret (simulating ESO-managed secret).
	passwordSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"password": []byte("initial-password"),
		},
	}
	require.NoError(t, k8sClient.Create(ctx, passwordSecret))

	// Step 2: Create User CR with syncCredentials enabled.
	user := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userName,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: environment.ClusterSourceValid,
			Authentication: &redpandav1alpha2.UserAuthenticationSpec{
				Password: redpandav1alpha2.Password{
					ValueFrom: &redpandav1alpha2.PasswordSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "password",
						},
					},
					NoGenerate: true,
				},
				SyncCredentials: true,
			},
		},
	}

	key := client.ObjectKeyFromObject(user)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	require.NoError(t, k8sClient.Create(ctx, user))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, key, user))
	require.True(t, user.Status.ManagedUser)

	// Verify initial password works.
	verifyAuth := func(password string) {
		t.Helper()
		c, err := kgo.NewClient(kgo.SeedBrokers(environment.KafkaURL), timeoutOption, kgo.SASL(scram.Auth{
			User: userName,
			Pass: password,
		}.AsSha512Mechanism()))
		require.NoError(t, err)
		defer c.Close()
		_, err = kadm.NewClient(c).BrokerMetadata(ctx)
		require.NoError(t, err)
	}
	verifyAuth("initial-password")

	// Step 3: Simulate ESO rotating the password.
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(passwordSecret), passwordSecret))
	passwordSecret.Data["password"] = []byte("rotated-password")
	require.NoError(t, k8sClient.Update(ctx, passwordSecret))

	// Step 4: Reconcile again — syncCredentials should push the new password.
	require.NoError(t, k8sClient.Get(ctx, key, user))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	// Verify rotated password now works.
	verifyAuth("rotated-password")

	// Cleanup.
	require.NoError(t, k8sClient.Delete(ctx, user))
	_, err = environment.Reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
}

func TestUserReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &UserReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

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

	baseUser := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource:  environment.ClusterSourceValid,
			Authentication: authenticationSpec,
			Authorization:  authorizationSpec,
		},
	}

	for name, tt := range map[string]struct {
		mutate            func(user *redpandav1alpha2.User)
		expectedCondition metav1.Condition
		onlyCheckDeletion bool
	}{
		"success - authorization and authentication": {
			expectedCondition: environment.SyncedCondition,
		},
		"success - authorization and authentication deletion cleanup": {
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authentication": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - authentication deletion cleanup": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authentication = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - authorization deletion cleanup": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.Authentication = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - adopt existing user": {
			mutate: func(user *redpandav1alpha2.User) {
				// Authorization is left nil so we only check user
				// management, not ACLs.
				user.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
		},
		"error - invalid cluster ref": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = environment.ClusterSourceInvalidRef
			},
			expectedCondition: environment.InvalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = environment.ClusterSourceNoSASL
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = environment.ClusterSourceBadPassword
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"partial sync - SR ACLs without SR configured": {
			mutate: func(user *redpandav1alpha2.User) {
				user.Spec.ClusterSource = environment.ClusterSourceNoSchemaRegistry
				user.Spec.Authorization = &redpandav1alpha2.UserAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeTopic,
								Name: "test-topic",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
						{
							Type: redpandav1alpha2.ACLTypeAllow,
							Resource: redpandav1alpha2.ACLResourceSpec{
								Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
								Name: "test-subject",
							},
							Operations: []redpandav1alpha2.ACLOperation{
								redpandav1alpha2.ACLOperationRead,
							},
						},
					},
				}
			},
			expectedCondition: environment.PartiallySyncedCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := baseUser.DeepCopy()
			user.Name = "user" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(user)
			}

			k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(user)
			req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

			require.NoError(t, k8sClient.Create(ctx, user))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, k8sClient.Get(ctx, key, user))
			require.Equal(t, []string{FinalizerKey}, user.Finalizers)
			require.Len(t, user.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, user.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, user.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, user.Status.Conditions[0].Reason)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				syncer, err := environment.Factory.ACLs(ctx, user)
				require.NoError(t, err)
				defer syncer.Close()

				userClient, err := environment.Factory.Users(ctx, user)
				require.NoError(t, err)
				defer userClient.Close()

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
					kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(environment.KafkaURL), timeoutOption, kgo.SASL(scram.Auth{
						User: user.Name,
						Pass: "password",
					}.AsSha512Mechanism()))
					require.NoError(t, err)
					defer kafkaClient.Close()

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
						require.NoError(t, k8sClient.Update(ctx, user))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, k8sClient.Get(ctx, key, user))
						require.False(t, user.Status.ManagedUser)
					}

					// make sure we no longer have a user
					hasUser, err := userClient.Has(ctx, user)
					require.NoError(t, err)
					require.False(t, hasUser)

					if user.ShouldManageACLs() {
						// now clear out any managed ACLs and re-check
						user.Spec.Authorization = nil
						require.NoError(t, k8sClient.Update(ctx, user))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, k8sClient.Get(ctx, key, user))
						require.False(t, user.Status.ManagedACLs)
					}

					// make sure we no longer have acls
					acls, err := syncer.ListACLs(ctx, user.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, k8sClient.Delete(ctx, user))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, user)))

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
			require.NoError(t, k8sClient.Delete(ctx, user))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, user)))
		})
	}
}
