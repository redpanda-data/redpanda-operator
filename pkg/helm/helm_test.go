// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helm_test

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestHelm(t *testing.T) {
	ctx := testutil.Context(t)

	configDir := path.Join(t.TempDir(), "helm-1")

	c, err := helm.New(helm.Options{ConfigHome: configDir})
	require.NoError(t, err)

	repos := []helm.Repo{
		{Name: "redpanda", URL: "https://charts.redpanda.com"},
		{Name: "jetstack", URL: "https://charts.jetstack.io"},
	}

	listedRepos, err := c.RepoList(ctx)
	require.NoError(t, err)
	require.Len(t, listedRepos, 0)

	for _, repo := range repos {
		require.NoError(t, c.RepoAdd(ctx, repo.Name, repo.URL))
	}

	listedRepos, err = c.RepoList(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, repos, listedRepos)

	charts, err := c.Search(ctx, "redpanda/redpanda")
	require.NoError(t, err)
	require.Len(t, charts, 1)
	require.Equal(t, "Redpanda is the real-time engine for modern apps.", charts[0].Description)
}
