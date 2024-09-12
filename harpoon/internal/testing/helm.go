// Copyright 2024 Redpanda Data, Inc.
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

	"github.com/redpanda-data/helm-charts/pkg/helm"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
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
