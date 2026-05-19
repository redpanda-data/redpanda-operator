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
	"os"
	"path/filepath"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

// resolveOperatorChart returns a chart path on disk for the given source.
// Local sources pass through unchanged. Remote sources are pulled into a temp
// directory created by the framework helper (cleaned up automatically) and the
// untarred chart directory is returned.
func resolveOperatorChart(ctx context.Context, t framework.TestingT, src operatorChartSource) string {
	if src.Path != "" && src.RemoteChart != nil {
		t.Fatalf("operatorChartSource has both Path and RemoteChart set; exactly one is required")
	}
	if src.Path != "" {
		return src.Path
	}
	if src.RemoteChart == nil {
		t.Fatalf("operatorChartSource has neither Path nor RemoteChart set")
	}
	return pullHelmChart(ctx, t, *src.RemoteChart)
}

// pullHelmChart pulls and untars a remote helm chart into a temp dir, returning
// the path to the untarred chart directory ready for loader.Load.
func pullHelmChart(_ context.Context, t framework.TestingT, rc remoteChart) string {
	destDir, err := os.MkdirTemp("", "operator-chart-*")
	require.NoError(t, err, "creating temp dir for chart pull")
	t.Cleanup(func(_ context.Context) {
		_ = os.RemoveAll(destDir)
	})

	regClient, err := registry.NewClient()
	require.NoError(t, err, "creating helm registry client")

	cfg := &action.Configuration{RegistryClient: regClient}
	pull := action.NewPullWithOpts(action.WithConfig(cfg))
	pull.Settings = cli.New()
	pull.RepoURL = rc.RepoURL
	pull.Version = rc.Version
	pull.DestDir = destDir
	pull.Untar = true
	pull.UntarDir = destDir

	out, err := pull.Run(rc.ChartName)
	require.NoErrorf(t, err, "helm pull %s --repo %s --version %s: %s", rc.ChartName, rc.RepoURL, rc.Version, out)

	chartDir := filepath.Join(destDir, rc.ChartName)
	if _, statErr := os.Stat(chartDir); statErr != nil {
		t.Fatalf("untarred chart dir %q not found after pull: %v", chartDir, statErr)
	}
	t.Logf("pulled helm chart %s@%s from %s to %s", rc.ChartName, rc.Version, rc.RepoURL, chartDir)
	return chartDir
}
