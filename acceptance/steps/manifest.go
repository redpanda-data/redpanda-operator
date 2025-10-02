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
	"regexp"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

// substRE is a regular expression used to match placeholders in YAML manifests in the form of: ${KEY_NAME}
// os.Expand is not used as it's matching is too permissive.
var substRE = regexp.MustCompile(`\$\{[A-Z_]+\}`)

func iApplyKubernetesManifest(ctx context.Context, t framework.TestingT, manifest *godog.DocString) {
	file, err := os.CreateTemp("", "manifest-*.yaml")
	require.NoError(t, err)

	content := substRE.ReplaceAllStringFunc(manifest.Content, func(match string) string {
		key := match[2 : len(match)-1] // ${FOO} -> FOO
		switch key {
		case "NAMESPACE":
			return t.Namespace()
		}

		t.Fatalf("unhandled expansion: %s", key)
		return "UNREACHABLE"
	})

	_, err = file.Write([]byte(content))
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
