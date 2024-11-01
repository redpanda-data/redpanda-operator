// Copyright 2024 Redpanda Data, Inc.
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
	"os"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func iApplyKubernetesManifest(ctx context.Context, t framework.TestingT, manifest *godog.DocString) {
	file, err := os.CreateTemp("", "manifest-*.yaml")
	require.NoError(t, err)

	_, err = file.Write([]byte(manifest.Content))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, os.RemoveAll(file.Name()))
	})

	t.ApplyManifest(ctx, file.Name())
}
