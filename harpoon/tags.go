// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"context"

	"github.com/stretchr/testify/require"
)

func isolatedTag(ctx context.Context, t TestingT, args ...string) context.Context {
	t.IsolateNamespace(ctx)
	return ctx
}

// vclusterTag handles @vcluster and @vcluster:<cluster-domain>. The optional
// argument runs the feature in a vcluster whose DNS serves that domain instead
// of cluster.local, for features that exercise the operator's --cluster-domain
// handling.
func vclusterTag(ctx context.Context, t TestingT, args ...string) context.Context {
	require.LessOrEqual(t, len(args), 1, "vcluster tags take at most one argument, the cluster domain")
	clusterDomain := ""
	if len(args) == 1 {
		clusterDomain = args[0]
	}
	t.VCluster(ctx, clusterDomain)
	return ctx
}

func variantTag(ctx context.Context, t TestingT, args ...string) context.Context {
	require.Equal(t, len(args), 1, "variant tags take a single argument")
	return ctx
}

func injectVariantTag(ctx context.Context, t TestingT, args ...string) context.Context {
	require.Equal(t, len(args), 1, "variant tags take a single argument")
	t.MarkVariant(args[0])
	return ctx
}
