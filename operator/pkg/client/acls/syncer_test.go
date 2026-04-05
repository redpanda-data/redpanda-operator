// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package acls

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpsr"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func setupTestContainer(t *testing.T, ctx context.Context) (*kgo.Client, rpsr.ACLClient) {
	t.Helper()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v25.2.1",
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)
	require.NoError(t, err)

	// If an enterprise license is available, apply it to enable SR ACLs.
	if license := os.Getenv("REDPANDA_SAMPLE_LICENSE"); license != "" {
		adminURL, err := container.AdminAPIAddress(ctx)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, adminURL+"/v1/features/license", strings.NewReader(license))
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.SASL(scram.Auth{
		User: "user",
		Pass: "password",
	}.AsSha256Mechanism()))
	require.NoError(t, err)

	schemaRegistry, err := container.SchemaRegistryAddress(ctx)
	require.NoError(t, err)

	srCl, err := sr.NewClient(sr.URLs(schemaRegistry), sr.BasicAuth("user", "password"))
	require.NoError(t, err)

	srClient, err := rpsr.NewClient(srCl)
	require.NoError(t, err)

	return kafkaClient, srClient
}

func TestIntegrationSyncer(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	kafkaClient, _ := setupTestContainer(t, ctx)

	syncer := NewSyncer(kafkaClient, nil)
	defer syncer.Close()

	sortACLs := func(acls []redpandav1alpha2.ACLRule) {
		sort.SliceStable(acls, func(i, j int) bool {
			return acls[i].Resource.Type < acls[j].Resource.Type
		})
	}

	principalOne := "User:testuser"
	principalTwo := "User:testuser2"

	expectACLsMatch := func(t *testing.T, principal string, acls []redpandav1alpha2.ACLRule) {
		actual, err := syncer.ListACLs(ctx, principal)
		require.NoError(t, err)

		require.Len(t, actual, len(acls))

		sortACLs(acls)
		sortACLs(actual)

		for i := 0; i < len(actual); i++ {
			require.True(t, actual[i].Equals(acls[i]), "%+v != %+v", actual[i], acls[i])
		}
	}

	expectACLUpdate := func(t *testing.T, principal string, acls []redpandav1alpha2.ACLRule, expectCreated, expectDeleted int) {
		t.Helper()

		created, deleted, err := syncer.syncACL(ctx, principal, acls)
		require.NoError(t, err)

		require.Equal(t, expectCreated, created)
		require.Equal(t, expectDeleted, deleted)

		expectACLsMatch(t, principal, acls)
	}

	initialACLS := []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeTopic,
			Name: "1",
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}, {
		Type: redpandav1alpha2.ACLTypeAllow,
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeCluster,
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}}

	// create initial acls
	expectACLUpdate(t, principalOne, initialACLS, 2, 0)

	// remove 1 acl
	expectACLUpdate(t, principalOne, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeCluster,
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}}, 0, 1)

	// update acl
	expectACLUpdate(t, principalOne, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type:        redpandav1alpha2.ResourceTypeTopic,
			Name:        "mytopic",
			PatternType: ptr.To(redpandav1alpha2.PatternTypePrefixed),
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}}, 1, 1)

	// update acl again
	expectACLUpdate(t, principalOne, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type:        redpandav1alpha2.ResourceTypeTopic,
			Name:        "mytopic2",
			PatternType: ptr.To(redpandav1alpha2.PatternTypePrefixed),
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}}, 1, 1)

	// delete all acls
	expectACLUpdate(t, principalOne, []redpandav1alpha2.ACLRule{}, 0, 1)

	// check de-duplication of ACLs
	created, deleted, err := syncer.syncACL(ctx, principalOne, append(initialACLS, initialACLS[0]))
	require.NoError(t, err)
	require.Equal(t, created, 2)
	require.Equal(t, deleted, 0)
	expectACLsMatch(t, principalOne, initialACLS)

	// make sure we have separation based on principals
	aclsTwo := []redpandav1alpha2.ACLRule{initialACLS[0]}
	created, deleted, err = syncer.syncACL(ctx, principalTwo, aclsTwo)
	require.NoError(t, err)
	require.Equal(t, created, 1)
	require.Equal(t, deleted, 0)
	expectACLsMatch(t, principalTwo, aclsTwo)
	expectACLsMatch(t, principalOne, initialACLS)

	// clear all
	err = syncer.deleteAllACL(ctx, principalOne)
	require.NoError(t, err)
	expectACLsMatch(t, principalOne, []redpandav1alpha2.ACLRule{})
	err = syncer.deleteAllACL(ctx, principalTwo)
	require.NoError(t, err)
	expectACLsMatch(t, principalTwo, []redpandav1alpha2.ACLRule{})
}

func TestIntegrationSyncerSR(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	kafkaClient, srClient := setupTestContainer(t, ctx)

	// Probe whether the SR ACL endpoint is available (requires enterprise license).
	if _, err := srClient.ListACLs(ctx, nil); err != nil {
		t.Skipf("SR ACL endpoint not available (requires enterprise license): %v", err)
	}

	syncer := NewSyncer(kafkaClient, srClient)
	defer syncer.Close()

	// --- SR ACL tests ---

	srPrincipal := "User:sruser"

	listSRACLs := func(t *testing.T) []rpsr.ACL {
		t.Helper()
		acls, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: srPrincipal})
		require.NoError(t, err)
		return acls
	}

	// sync creates SR ACLs
	_, _, err := syncer.syncSRACL(ctx, srPrincipal, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
			Name: "my-subject",
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
			redpandav1alpha2.ACLOperationWrite,
		},
	}})
	require.NoError(t, err)
	require.Len(t, listSRACLs(t), 2)

	// sync is idempotent
	_, _, err = syncer.syncSRACL(ctx, srPrincipal, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
			Name: "my-subject",
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
			redpandav1alpha2.ACLOperationWrite,
		},
	}})
	require.NoError(t, err)
	require.Len(t, listSRACLs(t), 2)

	// sync updates SR ACLs
	_, _, err = syncer.syncSRACL(ctx, srPrincipal, []redpandav1alpha2.ACLRule{{
		Type: redpandav1alpha2.ACLTypeAllow,
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
			Name: "new-subject",
		},
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationRead,
		},
	}})
	require.NoError(t, err)
	srACLs := listSRACLs(t)
	require.Len(t, srACLs, 1)
	require.Equal(t, "new-subject", srACLs[0].Resource)

	// deleteAll removes all SR ACLs for principal
	err = syncer.deleteAllSRACL(ctx, srPrincipal)
	require.NoError(t, err)
	require.Empty(t, listSRACLs(t))

	// --- Mixed Kafka + SR ACL tests ---

	mixedPrincipal := "User:mixeduser"

	// Sync with mixed rules partitions correctly
	mixedObj := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{Name: "mixeduser"},
		Spec: redpandav1alpha2.UserSpec{
			Authorization: &redpandav1alpha2.UserAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeTopic,
							Name: "mixed-topic",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
						},
					},
					{
						Type: redpandav1alpha2.ACLTypeAllow,
						Resource: redpandav1alpha2.ACLResourceSpec{
							Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
							Name: "mixed-subject",
						},
						Operations: []redpandav1alpha2.ACLOperation{
							redpandav1alpha2.ACLOperationRead,
						},
					},
				},
			},
		},
	}

	err = syncer.Sync(ctx, mixedObj)
	require.NoError(t, err)

	// Verify Kafka ACLs were created
	kafkaACLs, err := syncer.ListACLs(ctx, mixedPrincipal)
	require.NoError(t, err)
	require.Len(t, kafkaACLs, 1)

	// Verify SR ACLs were created
	mixedSRACLs, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: mixedPrincipal})
	require.NoError(t, err)
	require.Len(t, mixedSRACLs, 1)
	require.Equal(t, "mixed-subject", mixedSRACLs[0].Resource)

	// Sync with Kafka-only rules doesn't create SR ACLs for a different principal
	kafkaOnlyPrincipal := "User:kafkaonly"

	kafkaOnlyObj := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{Name: "kafkaonly"},
		Spec: redpandav1alpha2.UserSpec{
			Authorization: &redpandav1alpha2.UserAuthorizationSpec{
				ACLs: []redpandav1alpha2.ACLRule{{
					Type: redpandav1alpha2.ACLTypeAllow,
					Resource: redpandav1alpha2.ACLResourceSpec{
						Type: redpandav1alpha2.ResourceTypeTopic,
						Name: "kafka-topic",
					},
					Operations: []redpandav1alpha2.ACLOperation{
						redpandav1alpha2.ACLOperationRead,
					},
				}},
			},
		},
	}

	err = syncer.Sync(ctx, kafkaOnlyObj)
	require.NoError(t, err)

	kafkaOnlySRACLs, err := srClient.ListACLs(ctx, &rpsr.ACL{Principal: kafkaOnlyPrincipal})
	require.NoError(t, err)
	require.Empty(t, kafkaOnlySRACLs)

	// DeleteAll cleans up both Kafka and SR ACLs
	err = syncer.DeleteAll(ctx, mixedObj)
	require.NoError(t, err)

	kafkaACLs, err = syncer.ListACLs(ctx, mixedPrincipal)
	require.NoError(t, err)
	require.Empty(t, kafkaACLs)

	mixedSRACLs, err = srClient.ListACLs(ctx, &rpsr.ACL{Principal: mixedPrincipal})
	require.NoError(t, err)
	require.Empty(t, mixedSRACLs)

	// clean up kafka-only user
	err = syncer.deleteAllACL(ctx, kafkaOnlyPrincipal)
	require.NoError(t, err)
}

func TestSyncerNilKafkaClient(t *testing.T) {
	t.Run("nil kafka client panics", func(t *testing.T) {
		require.Panics(t, func() {
			NewSyncer(nil, nil)
		})
	})
}
