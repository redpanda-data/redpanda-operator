// Copyright 2025 Redpanda Data, Inc.
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
	"os/exec"

	"github.com/cockroachdb/errors"
	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func iKustomizeApply(ctx context.Context, t framework.TestingT, url string) {
	cmd := exec.CommandContext(ctx, "kubectl", "kustomize", url)

	out, err := cmd.CombinedOutput()
	require.NoError(t, errors.WithStack(err), "failed to run kubectl kustomize %q", url)

	iApplyKubernetesManifest(ctx, t, &godog.DocString{Content: string(out)})

	t.Cleanup(func(ctx context.Context) {
		helmrepositories := &unstructured.UnstructuredList{
			Object: map[string]interface{}{"kind": "HelmRepository", "apiVersion": "source.toolkit.fluxcd.io/v1beta2"},
		}
		// Ignore errors
		_ = t.List(ctx, helmrepositories, &client.ListOptions{
			Namespace: t.Namespace(),
		})
		for _, repo := range helmrepositories.Items {
			r := client.MergeFrom(repo.DeepCopy())
			unstructured.RemoveNestedField(repo.Object, "metadata", "finalizers")
			require.NoError(t, t.Patch(ctx, &repo, r))
		}

		helmreleases := &unstructured.UnstructuredList{
			Object: map[string]interface{}{"kind": "HelmRelease", "apiVersion": "helm.toolkit.fluxcd.io/v2beta2"},
		}
		// Ignore errors
		_ = t.List(ctx, helmreleases, &client.ListOptions{
			Namespace: t.Namespace(),
		})
		for _, rel := range helmreleases.Items {
			r := client.MergeFrom(rel.DeepCopy())
			unstructured.RemoveNestedField(rel.Object, "metadata", "finalizers")
			require.NoError(t, t.Patch(ctx, &rel, r))
		}

		helmcharts := &unstructured.UnstructuredList{
			Object: map[string]interface{}{"kind": "HelmChart", "apiVersion": "source.toolkit.fluxcd.io/v1beta2"},
		}
		// Ignore errors
		_ = t.List(ctx, helmcharts, &client.ListOptions{
			Namespace: t.Namespace(),
		})
		for _, chart := range helmcharts.Items {
			r := client.MergeFrom(chart.DeepCopy())
			unstructured.RemoveNestedField(chart.Object, "metadata", "finalizers")
			require.NoError(t, t.Patch(ctx, &chart, r))
		}
	})
}
