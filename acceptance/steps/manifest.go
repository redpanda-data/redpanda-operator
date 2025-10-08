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
	"strings"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func iApplyKubernetesManifest(ctx context.Context, t framework.TestingT, manifest *godog.DocString) {
	file, err := os.CreateTemp("", "manifest-*.yaml")
	require.NoError(t, err)

	_, err = file.Write(normalizeContent(t, manifest.Content))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, os.RemoveAll(file.Name()))
	})

	t.ApplyManifest(ctx, file.Name())
}

func iInstallLocalCRDs(ctx context.Context, t framework.TestingT, directory string) {
	t.ApplyManifest(ctx, directory)
}

func normalizeContent(t framework.TestingT, content string) []byte {
	manifest := map[string]any{}
	require.NoError(t, yaml.Unmarshal([]byte(content), &manifest))

	if getVersion(t, "") == "vectorized" {
		addStringValueAtPath(manifest, "redpanda.vectorized.io", "spec.cluster.clusterRef.group")
		addStringValueAtPath(manifest, "redpanda.vectorized.io", "spec.shadowCluster.clusterRef.group")
		addStringValueAtPath(manifest, "redpanda.vectorized.io", "spec.sourceCluster.clusterRef.group")
		addStringValueAtPath(manifest, "Cluster", "spec.cluster.clusterRef.kind")
		addStringValueAtPath(manifest, "Cluster", "spec.shadowCluster.clusterRef.kind")
		addStringValueAtPath(manifest, "Cluster", "spec.sourceCluster.clusterRef.kind")
	}

	contentBytes, err := yaml.Marshal(manifest)
	require.NoError(t, err)

	return contentBytes
}

func addStringValueAtPath(manifest map[string]any, value string, path string) {
	keys := strings.Split(path, ".")

	current := manifest
	for i, key := range keys {
		if i == len(keys)-1 {
			break
		}
		found, ok := current[key]
		if !ok {
			// all but the final key must exist in the path
			return
		}
		cast, ok := found.(map[string]any)
		if !ok {
			return
		}
		current = cast
	}
	if len(keys) > 0 {
		last := keys[len(keys)-1]
		current[last] = value
	}
}
