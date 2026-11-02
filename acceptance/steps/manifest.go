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
	"strconv"
	"strings"
	"sync"

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

		if offset, ok := nodePortOffsets[key]; ok {
			return strconv.Itoa(nodePortBlockFor(t) + offset)
		}

		// Pass through Redpanda Connect runtime env var interpolations
		// (e.g., ${RPK_BROKERS}) that are resolved inside the container,
		// not by the test framework.
		if strings.HasPrefix(key, "RPK_") {
			return match
		}

		t.Fatalf("unhandled expansion: %s", key)
		return "UNREACHABLE"
	})
}

// The redpanda chart always sets an explicit nodePort on its external
// Service, and nodePorts are global to the shared k3d cluster, so parallel
// features left on the chart defaults fail with "provided port is already
// allocated". A manifest that keeps external enabled sets each listener's
// advertisedPorts to these placeholders instead.
var nodePortOffsets = map[string]int{
	"NODEPORT_ADMIN": 0,
	"NODEPORT_KAFKA": 1,
	"NODEPORT_HTTP":  2,
	"NODEPORT_SR":    3,
}

// Blocks come from the static band of the default range (30000-30085), which
// auto-allocation only falls back to once the rest of the range is exhausted.
// The band stops short of 30081/30082: the chart's default schema registry and
// http nodePorts, still used by features whose manifests are docs snippets.
const (
	nodePortBandStart = 30000
	nodePortBandEnd   = 30079
)

var nodePortBlocks = struct {
	sync.Mutex
	byNamespace map[string]int
}{byNamespace: map[string]int{}}

// nodePortBlockFor returns the first port of the feature's block, keyed by its
// isolated namespace so every manifest the feature applies (including
// re-applies of the same resource) advertises the same ports.
func nodePortBlockFor(t framework.TestingT) int {
	nodePortBlocks.Lock()
	defer nodePortBlocks.Unlock()

	if base, ok := nodePortBlocks.byNamespace[t.Namespace()]; ok {
		return base
	}

	base := nodePortBandStart + len(nodePortBlocks.byNamespace)*len(nodePortOffsets)
	if base+len(nodePortOffsets)-1 > nodePortBandEnd {
		t.Fatalf("static nodePort band %d-%d exhausted; %d features already hold a block", nodePortBandStart, nodePortBandEnd, len(nodePortBlocks.byNamespace))
	}
	nodePortBlocks.byNamespace[t.Namespace()] = base
	return base
}
