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
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

func (t *TestingT) InstallHelmChart(ctx context.Context, url, repo, chart string, options helm.InstallOptions) {
	helmClient, err := helm.New(helm.Options{
		KubeConfig: rest.CopyConfig(t.restConfig),
	})
	require.NoError(t, err)
	require.NoError(t, helmClient.RepoAdd(ctx, repo, url))
	require.NotEqual(t, "", options.Namespace, "namespace must not be blank")
	require.NotEqual(t, "", options.Name, "name must not be blank")

	options.CreateNamespace = true

	t.Logf("installing chart %q", repo+"/"+chart)
	_, err = helmClient.Install(ctx, repo+"/"+chart, options)
	require.NoError(t, err)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("uninstalling chart %q", repo+"/"+chart)
		require.NoError(t, helmClient.Uninstall(ctx, helm.Release{
			Name:      options.Name,
			Namespace: options.Namespace,
		}))
	})
}

func (t *TestingT) InstallLocalHelmChart(ctx context.Context, path string, options helm.InstallOptions, deps ...helm.Dependency) {
	helmClient, err := helm.New(helm.Options{
		KubeConfig: rest.CopyConfig(t.restConfig),
	})
	require.NoError(t, err)
	require.NotEqual(t, "", options.Namespace, "namespace must not be blank")
	require.NotEqual(t, "", options.Name, "name must not be blank")

	options.CreateNamespace = true

	for _, dep := range deps {
		require.NoError(t, helmClient.RepoAdd(ctx, dep.Name, dep.Repository))
	}

	require.NoError(t, helmClient.DependencyBuild(ctx, path))

	t.Logf("installing chart %q", path)
	_, err = helmClient.Install(ctx, path, options)
	require.NoError(t, err)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("uninstalling chart %q", path)
		require.NoError(t, helmClient.Uninstall(ctx, helm.Release{
			Name:      options.Name,
			Namespace: options.Namespace,
		}))
	})
}
