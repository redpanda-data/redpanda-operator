// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func schemaIsSuccessfullySynced(ctx context.Context, t framework.TestingT, schema string) {
	var schemaObject redpandav1alpha2.Schema
	require.NoError(t, t.Get(ctx, t.ResourceKey(schema), &schemaObject))

	waitForSyncedCondition(ctx, t, &schemaObject, func() []metav1.Condition {
		return schemaObject.Status.Conditions
	}, func() int64 {
		return schemaObject.Status.ObservedGeneration
	})
}

func thereIsASchema(ctx context.Context, schema, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectSchema(ctx, schema)
}

func thereIsNoSchema(ctx context.Context, schema, version, cluster string) {
	versionedClientsForCluster(ctx, version, cluster).ExpectNoSchema(ctx, schema)
}

func iShouldBeAbleToCheckCompatibilityAgainst(ctx context.Context, schema, version, cluster string) {
	clients := versionedClientsForCluster(ctx, version, cluster)
	clients.ExpectSchema(ctx, schema)
}
