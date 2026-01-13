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

func vclusterTag(ctx context.Context, t TestingT, args ...string) context.Context {
	t.VCluster(ctx)
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
