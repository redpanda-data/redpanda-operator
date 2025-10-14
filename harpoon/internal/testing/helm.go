// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

// AddHelmRepo adds a helm repository by name and URL.
func (t *TestingT) AddHelmRepo(ctx context.Context, name, url string) {
	require.NoError(t, t.helmClient.RepoAdd(ctx, name, url))
}

// InstallHelmChart installs a helm chart from either a local path or a repo reference.
//
//	t.InstallHelmChart("../charts/redpanda/chart") // Local chart
//	t.InstallHelmChart("jetstack/cert-manager") // From repo
func (t *TestingT) InstallHelmChart(ctx context.Context, chart string, options helm.InstallOptions) {
	require.NotEqual(t, "", options.Namespace, "namespace must not be blank")
	require.NotEqual(t, "", options.Name, "name must not be blank")

	options.CreateNamespace = true

	t.Logf("installing chart %q", chart)
	rel, err := t.helmClient.Install(ctx, chart, options)
	require.NoError(t, err)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("uninstalling chart %q", chart)
		require.NoError(t, t.helmClient.Uninstall(ctx, helm.Release{
			Name:      rel.Name,
			Namespace: options.Namespace,
		}))
	})
}

// UpgradeHelmChart upgrades a helm chart from either a local path or a repo reference.
// Examples:
//   - Local chart: "../charts/redpanda/chart"
//   - Remote chart: "console/console" or "jetstack/cert-manager"
func (t *TestingT) UpgradeHelmChart(ctx context.Context, release, chart string, options helm.UpgradeOptions) {
	require.NotEqual(t, "", options.Namespace, "namespace must not be blank")

	t.Logf("upgrading chart %q", chart)
	_, err := t.helmClient.Upgrade(ctx, release, chart, options)
	require.NoError(t, err)
}
