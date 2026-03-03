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
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestGroupReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &GroupReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	authorizationSpec := &redpandav1alpha2.GroupAuthorizationSpec{
		ACLs: []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeGroup,
				Name: "consumer-group",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationDescribe,
			},
		}},
	}

	baseGroup := &redpandav1alpha2.RedpandaGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.GroupSpec{
			ClusterSource: environment.ClusterSourceValid,
			Authorization: authorizationSpec,
		},
	}

	for name, tt := range map[string]struct {
		mutate            func(group *redpandav1alpha2.RedpandaGroup)
		expectedCondition metav1.Condition
		onlyCheckDeletion bool
	}{
		"success - with authorization": {
			expectedCondition: environment.SyncedCondition,
		},
		"success - with authorization deletion cleanup": {
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"success - without authorization": {
			mutate: func(group *redpandav1alpha2.RedpandaGroup) {
				group.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
		},
		"success - without authorization deletion cleanup": {
			mutate: func(group *redpandav1alpha2.RedpandaGroup) {
				group.Spec.Authorization = nil
			},
			expectedCondition: environment.SyncedCondition,
			onlyCheckDeletion: true,
		},
		"error - invalid cluster ref": {
			mutate: func(group *redpandav1alpha2.RedpandaGroup) {
				group.Spec.ClusterSource = environment.ClusterSourceInvalidRef
			},
			expectedCondition: environment.InvalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(group *redpandav1alpha2.RedpandaGroup) {
				group.Spec.ClusterSource = environment.ClusterSourceNoSASL
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(group *redpandav1alpha2.RedpandaGroup) {
				group.Spec.ClusterSource = environment.ClusterSourceBadPassword
			},
			expectedCondition: environment.ClientErrorCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			group := baseGroup.DeepCopy()
			group.Name = "group" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(group)
			}

			k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(group)
			req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

			require.NoError(t, k8sClient.Create(ctx, group))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, k8sClient.Get(ctx, key, group))
			require.Equal(t, []string{FinalizerKey}, group.Finalizers)
			require.Len(t, group.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, group.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Status, group.Status.Conditions[0].Status)
			require.Equal(t, tt.expectedCondition.Reason, group.Status.Conditions[0].Reason)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				syncer, err := environment.Factory.ACLs(ctx, group)
				require.NoError(t, err)
				defer syncer.Close()

				if group.Spec.Authorization != nil && len(group.Spec.Authorization.ACLs) > 0 {
					// make sure we actually have ACLs
					acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 1)
				} else {
					// no authorization means no ACLs
					acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				if !tt.onlyCheckDeletion {
					if group.Spec.Authorization != nil {
						// now clear out authorization and re-check
						group.Spec.Authorization = nil
						require.NoError(t, k8sClient.Update(ctx, group))
						_, err = environment.Reconciler.Reconcile(ctx, req)
						require.NoError(t, err)
						require.NoError(t, k8sClient.Get(ctx, key, group))
					}

					// make sure we no longer have ACLs
					acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
					require.NoError(t, err)
					require.Len(t, acls, 0)
				}

				// clean up and make sure we properly delete everything
				require.NoError(t, k8sClient.Delete(ctx, group))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, group)))

				// make sure we no longer have ACLs
				acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
				require.NoError(t, err)
				require.Len(t, acls, 0)

				return
			}

			// clean up and make sure we properly delete everything
			require.NoError(t, k8sClient.Delete(ctx, group))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, group)))
		})
	}
}

func TestGroupACLConfigurations(t *testing.T) { // nolint:funlen // Comprehensive test coverage
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &GroupReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	testCases := []struct {
		name          string
		authorization *redpandav1alpha2.GroupAuthorizationSpec
		expectedACLs  int
	}{
		{
			name:          "no-authorization",
			authorization: nil,
			expectedACLs:  0,
		},
		{
			name: "empty-acls",
			authorization: &redpandav1alpha2.GroupAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{},
			},
			expectedACLs: 0,
		},
		{
			name: "single-topic-acl",
			authorization: &redpandav1alpha2.GroupAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeTopic,
						Name: "test-topic",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
					},
				}},
			},
			expectedACLs: 1,
		},
		{
			name: "multiple-acls",
			authorization: &redpandav1alpha2.GroupAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "team-topic",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
							redpandav1alpha2.ACLOperationWrite,
						},
					},
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeGroup,
							Name: "team-group",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
						},
					},
				},
			},
			expectedACLs: 3, // 2 topic + 1 group
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			group := &redpandav1alpha2.RedpandaGroup{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-group-" + strconv.Itoa(int(time.Now().UnixNano())),
				},
				Spec: redpandav1alpha2.GroupSpec{
					ClusterSource: environment.ClusterSourceValid,
					Authorization: tt.authorization,
				},
			}

			k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(group)
			req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

			// Create and reconcile
			require.NoError(t, k8sClient.Create(ctx, group))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			// Verify status
			require.NoError(t, k8sClient.Get(ctx, key, group))
			require.Equal(t, []string{FinalizerKey}, group.Finalizers)
			require.Len(t, group.Status.Conditions, 1)
			require.Equal(t, environment.SyncedCondition.Status, group.Status.Conditions[0].Status)

			// Verify ACLs
			syncer, err := environment.Factory.ACLs(ctx, group)
			require.NoError(t, err)
			defer syncer.Close()

			acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, tt.expectedACLs)

			// Clean up
			require.NoError(t, k8sClient.Delete(ctx, group))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, group)))
		})
	}
}

func TestGroupACLLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &GroupReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	})

	group := &redpandav1alpha2.RedpandaGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "lifecycle-group-" + strconv.Itoa(int(time.Now().UnixNano())),
		},
		Spec: redpandav1alpha2.GroupSpec{
			ClusterSource: environment.ClusterSourceValid,
			// Start without authorization
		},
	}

	key := client.ObjectKeyFromObject(group)
	req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

	// Phase 1: Create without authorization (no ACLs)
	t.Run("create_without_authorization", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Create(ctx, group))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))
		require.Len(t, group.Status.Conditions, 1)
		require.Equal(t, metav1.ConditionTrue, group.Status.Conditions[0].Status)

		// Verify no ACLs
		syncer, err := environment.Factory.ACLs(ctx, group)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0)
	})

	// Phase 2: Add authorization (ACLs should be created)
	t.Run("add_authorization", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))
		group.Spec.Authorization = &redpandav1alpha2.GroupAuthorizationSpec{
			ACLs: []redpandav1alpha2.ACLRule{{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeTopic,
					Name: "lifecycle-topic",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			}},
		}

		require.NoError(t, k8sClient.Update(ctx, group))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))
		require.Len(t, group.Status.Conditions, 1)
		require.Equal(t, metav1.ConditionTrue, group.Status.Conditions[0].Status)

		// Verify ACLs exist
		syncer, err := environment.Factory.ACLs(ctx, group)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 1)
	})

	// Phase 3: Update ACLs (add more rules)
	t.Run("update_acls", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))
		group.Spec.Authorization = &redpandav1alpha2.GroupAuthorizationSpec{
			ACLs: []redpandav1alpha2.ACLRule{
				{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeTopic,
						Name: "lifecycle-topic",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
						redpandav1alpha2.ACLOperationWrite,
					},
				},
				{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeGroup,
						Name: "lifecycle-consumer",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
					},
				},
			},
		}

		require.NoError(t, k8sClient.Update(ctx, group))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))

		// Verify updated ACLs
		syncer, err := environment.Factory.ACLs(ctx, group)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 3) // 2 topic + 1 group
	})

	// Phase 4: Remove authorization (ACLs should be removed)
	t.Run("remove_authorization", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))
		group.Spec.Authorization = nil

		require.NoError(t, k8sClient.Update(ctx, group))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, group))

		// Verify ACLs are removed
		syncer, err := environment.Factory.ACLs(ctx, group)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 0)
	})

	// Phase 5: Clean up
	t.Run("cleanup", func(t *testing.T) {
		k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Delete(ctx, group))
		_, err = environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, group)))
	})
}

func TestGroupOIDCIntegration(t *testing.T) { //nolint:funlen // End-to-end integration test.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	oidcConfig := OIDCConfig{
		Email:        "testuser@example.com",
		Password:     "password",
		Groups:       []string{"engineering"},
		ClientID:     "redpanda",
		ClientSecret: "redpanda-secret",
		Scopes:       "openid groups email",
	}

	timeoutOption := kgo.RetryTimeout(1 * time.Millisecond)
	environment := InitializeResourceReconcilerTest(t, ctx, &GroupReconciler{
		extraOptions: []kgo.Opt{timeoutOption},
	}, WithOIDC(oidcConfig))

	// initial test setup
	{
		// Create test topics as superuser.
		superuserClient, err := kgo.NewClient(
			kgo.SeedBrokers(environment.KafkaURL),
			kgo.SASL(scram.Auth{User: "superuser", Pass: "password"}.AsSha256Mechanism()),
		)
		require.NoError(t, err)
		defer superuserClient.Close()

		superuserAdmin := kadm.NewClient(superuserClient)

		// "team-test" will be the authorized topic; "secret-topic" will be unauthorized.
		_, err = superuserAdmin.CreateTopic(ctx, 1, 1, nil, "team-test")
		require.NoError(t, err)
		_, err = superuserAdmin.CreateTopic(ctx, 1, 1, nil, "secret-topic")
		require.NoError(t, err)

		// Seed "team-test" with a message so we can verify consume access later.
		produceResults := superuserClient.ProduceSync(ctx, &kgo.Record{
			Topic: "team-test",
			Value: []byte("hello from superuser"),
		})
		require.NoError(t, produceResults.FirstErr())
	}

	k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
	require.NoError(t, err)
	// Obtain a JWT from Dex for a user in the "engineering" group.
	idToken := environment.FetchOIDCToken(t)
	newOIDCClient := func(t *testing.T) (*kgo.Client, *kadm.Client) {
		t.Helper()
		cl, err := kgo.NewClient(
			kgo.SeedBrokers(environment.KafkaURL),
			kgo.SASL(oauth.Auth{Token: idToken}.AsMechanism()),
			kgo.RecordRetries(0),
			kgo.RetryTimeout(5*time.Second),
		)
		require.NoError(t, err)
		t.Cleanup(cl.Close)
		return cl, kadm.NewClient(cl)
	}

	setupGroup := func(t *testing.T) func(t *testing.T) {
		group := &redpandav1alpha2.RedpandaGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "engineering",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: redpandav1alpha2.GroupSpec{
				ClusterSource: environment.ClusterSourceValid,
				Authorization: &redpandav1alpha2.GroupAuthorizationSpec{
					ACLs: []redpandav1alpha2.ACLRule{{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "team-test",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
							redpandav1alpha2.ACLOperationDescribe,
						},
					}},
				},
			},
		}

		key := client.ObjectKeyFromObject(group)
		req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

		require.NoError(t, k8sClient.Create(ctx, group))
		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		// Verify the group was synced successfully.
		require.NoError(t, k8sClient.Get(ctx, key, group))
		require.Len(t, group.Status.Conditions, 1)
		require.Equal(t, metav1.ConditionTrue, group.Status.Conditions[0].Status, "RedpandaGroup not synced: %s", group.Status.Conditions[0].Message)

		// Verify ACLs were created for the Group principal.
		syncer, err := environment.Factory.ACLs(ctx, group)
		require.NoError(t, err)
		defer syncer.Close()

		acls, err := syncer.ListACLs(ctx, group.GetPrincipal())
		require.NoError(t, err)
		require.Len(t, acls, 2, "expected 2 ACLs (Read + Describe) for Group:engineering")

		return func(t *testing.T) {
			// delete the group and verify ACLs are removed.
			require.NoError(t, k8sClient.Delete(ctx, group))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, key, group)))

			// Create a fresh syncer — the one from setupGroup may have a stale connection.
			cleanupSyncer, err := environment.Factory.ACLs(ctx, group)
			require.NoError(t, err)
			defer cleanupSyncer.Close()

			acls, err := cleanupSyncer.ListACLs(ctx, group.GetPrincipal())
			require.NoError(t, err)
			require.Len(t, acls, 0, "ACLs should be removed after group deletion")
		}
	}

	hasTopics := func(want []string, missing []string) func() bool {
		return func() bool {
			_, admin := newOIDCClient(t)
			topics, err := admin.ListTopics(ctx)
			if err != nil {
				return false
			}
			for _, topic := range want {
				if _, ok := topics[topic]; !ok {
					return false
				}
			}
			for _, topic := range missing {
				if _, ok := topics[topic]; ok {
					return false
				}
			}
			return true
		}
	}

	t.Run("with acls", func(t *testing.T) {
		cleanup := setupGroup(t)
		defer cleanup(t)

		// Check Authorized topics.
		require.Eventually(t, hasTopics([]string{"team-test"}, []string{"secret-topic"}), 10*time.Second, 1*time.Second, "OIDC client should eventually see 'team-test' once Redpanda finishes OIDC key sync")

		// DENIED

		// Produce to authorized topic (no Write ACL).
		oidcClient, oidcAdmin := newOIDCClient(t)
		produceResults := oidcClient.ProduceSync(ctx, &kgo.Record{
			Topic: "team-test",
			Value: []byte("unauthorized write attempt"),
		})
		require.Error(t, produceResults.FirstErr(), "producing to 'team-test' should fail (Write not in Group:engineering ACL)")

		// Create topic (no Cluster-level Create ACL).
		_, err = oidcAdmin.CreateTopic(ctx, 1, 1, nil, "oidc-created-topic")
		require.Error(t, err, "creating a topic should fail (no Create ACL for Group:engineering)")
	})

	t.Run("without acls", func(t *testing.T) {
		// Without any RedpandaGroup ACLs, the OIDC client should never gain
		// access to any topics. Poll for 10 seconds to confirm access is
		// consistently denied (guards against false negatives from async propagation).
		require.Never(t, hasTopics([]string{"team-test", "secret-topic"}, []string{}), 10*time.Second, 1*time.Second, "OIDC client should never see test topics without ACLs")
	})
}
