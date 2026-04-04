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
	"regexp"
	"strings"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func iApplyKubernetesManifest(ctx context.Context, t framework.TestingT, manifest *godog.DocString) {
	file, err := os.CreateTemp("", "manifest-*.yaml")
	require.NoError(t, err)

	content := PatchManifest(t, manifest.Content)

	_, err = file.Write(normalizeContent(t, content))
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

// substRE is a regular expression used to match placeholders in YAML manifests in the form of: ${KEY_NAME}
// os.Expand is not used as it's matching is too permissive.
var substRE = regexp.MustCompile(`\$\{[A-Z_]+\}`)

func PatchManifest(t framework.TestingT, content string) string {
	return substRE.ReplaceAllStringFunc(content, func(match string) string {
		key := match[2 : len(match)-1] // ${FOO} -> FOO
		switch key {
		case "DEFAULT_REDPANDA_REPO":
			return DefaultRedpandaRepo
		case "DEFAULT_REDPANDA_TAG":
			return DefaultRedpandaTag
		case "NAMESPACE":
			return t.Namespace()
		}

		t.Fatalf("unhandled expansion: %s", key)
		return "UNREACHABLE"
	})
}
