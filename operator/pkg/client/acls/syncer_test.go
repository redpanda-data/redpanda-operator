// Copyright 2024 Redpanda Data, Inc.
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
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestSyncer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.SASL(scram.Auth{
		User: "user",
		Pass: "password",
	}.AsSha256Mechanism()))
	require.NoError(t, err)

	syncer := NewSyncer(kafkaClient)
	defer syncer.Close()

	sortACLs := func(acls []v1alpha2.ACLRule) {
		sort.SliceStable(acls, func(i, j int) bool {
			return acls[i].Resource.Type < acls[j].Resource.Type
		})
	}

	principalOne := "User:testuser"
	principalTwo := "User:testuser2"

	expectACLsMatch := func(t *testing.T, principal string, acls []v1alpha2.ACLRule) {
		actual, err := syncer.ListACLs(ctx, principal)
		require.NoError(t, err)

		require.Len(t, actual, len(acls))

		sortACLs(acls)
		sortACLs(actual)

		for i := 0; i < len(actual); i++ {
			require.True(t, actual[i].Equals(acls[i]), "%+v != %+v", actual[i], acls[i])
		}
	}

	expectACLUpdate := func(t *testing.T, principal string, acls []v1alpha2.ACLRule, expectCreated, expectDeleted int) {
		t.Helper()

		created, deleted, err := syncer.sync(ctx, principal, acls)
		require.NoError(t, err)

		require.Equal(t, expectCreated, created)
		require.Equal(t, expectDeleted, deleted)

		expectACLsMatch(t, principal, acls)
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
	expectACLUpdate(t, principalOne, initialACLS, 2, 0)

	// remove 1 acl
	expectACLUpdate(t, principalOne, []v1alpha2.ACLRule{{
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
	expectACLUpdate(t, principalOne, []v1alpha2.ACLRule{{
		Type: v1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: v1alpha2.ACLResourceSpec{
			Type:        v1alpha2.ResourceTypeTopic,
			Name:        "mytopic",
			PatternType: ptr.To(v1alpha2.PatternTypePrefixed),
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationRead,
		},
	}}, 1, 1)

	// update acl again
	expectACLUpdate(t, principalOne, []v1alpha2.ACLRule{{
		Type: v1alpha2.ACLTypeAllow,
		Host: ptr.To("*"),
		Resource: v1alpha2.ACLResourceSpec{
			Type:        v1alpha2.ResourceTypeTopic,
			Name:        "mytopic2",
			PatternType: ptr.To(v1alpha2.PatternTypePrefixed),
		},
		Operations: []v1alpha2.ACLOperation{
			v1alpha2.ACLOperationRead,
		},
	}}, 1, 1)

	// delete all acls
	expectACLUpdate(t, principalOne, []v1alpha2.ACLRule{}, 0, 1)

	// check de-duplication of ACLs
	created, deleted, err := syncer.sync(ctx, principalOne, append(initialACLS, initialACLS[0]))
	require.NoError(t, err)
	require.Equal(t, created, 2)
	require.Equal(t, deleted, 0)
	expectACLsMatch(t, principalOne, initialACLS)

	// make sure we have separation based on principals
	aclsTwo := []v1alpha2.ACLRule{initialACLS[0]}
	created, deleted, err = syncer.sync(ctx, principalTwo, aclsTwo)
	require.NoError(t, err)
	require.Equal(t, created, 1)
	require.Equal(t, deleted, 0)
	expectACLsMatch(t, principalTwo, aclsTwo)
	expectACLsMatch(t, principalOne, initialACLS)

	// clear all
	err = syncer.deleteAll(ctx, principalOne)
	require.NoError(t, err)
	expectACLsMatch(t, principalOne, []v1alpha2.ACLRule{})
	err = syncer.deleteAll(ctx, principalTwo)
	require.NoError(t, err)
	expectACLsMatch(t, principalTwo, []v1alpha2.ACLRule{})
}
