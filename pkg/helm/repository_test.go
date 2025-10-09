// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helm

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
)

func TestRepository(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository()
	server := httptest.NewServer(repo)
	t.Cleanup(server.Close)

	client, err := New(Options{
		ConfigHome: t.TempDir(),
		CacheHome:  t.TempDir(),
	})
	require.NoError(t, err)

	redpandaChart, err := os.ReadFile("testdata/redpanda-5.9.19.tgz")
	require.NoError(t, err)

	repo.AddChart(chart.Metadata{
		Name:     "redpanda",
		Version:  "5.9.19",
		Keywords: []string{"red", "panda", "redpanda"},
	}, redpandaChart)

	require.NoError(t, client.RepoAdd(ctx, "local", server.URL))

	charts, err := client.Search(ctx, "redpanda")
	require.NoError(t, err)
	require.Equal(t, []Chart{{
		Name:    "local/redpanda",
		Version: "5.9.19",
	}}, charts)

	_, err = client.Template(ctx, "local/redpanda", TemplateOptions{
		Name: "local-build",
	})
	require.NoError(t, err)
}
