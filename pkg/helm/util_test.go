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
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli/values"
	"pgregory.net/rapid"
	"sigs.k8s.io/yaml"
)

// TestMergeYAMLValues asserts that our implementation of MergeYAMLValues
// behaves the same way as Helm's implementation.
func TestMergeYAMLValues(t *testing.T) {
	// Rapid's default string generator is pretty creative which breaks YAML.
	// Limit it to ASCII.
	cleanString := rapid.StringMatching(`[a-zA-Z0-9 _/*]`)

	var yamlValue *rapid.Generator[any]
	yamlValue = rapid.Custom(func(t *rapid.T) any {
		typ := rapid.IntRange(0, 15).Draw(t, "type")

		// World's coolest weighted random function. Bias towards primitives so
		// we don't end up with massively nested maps.
		switch typ {
		case 0, 1, 2:
			return rapid.Int().Draw(t, "value")
		case 3, 4, 5:
			return rapid.Float64().Draw(t, "value")
		case 6, 7, 8:
			return rapid.Bool().Draw(t, "value")
		case 9, 10, 11:
			return cleanString.Draw(t, "value")
		case 12, 13:
			return nil
		case 14:
			return rapid.SliceOfN(yamlValue, 0, 5)
		case 15:
			return rapid.MapOfN(cleanString, yamlValue, 0, 5)
		default:
			panic("unreachable")
		}
	})

	// Rapid.T doesn't support the tempDir helper.
	tempDir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		instances := rapid.IntRange(0, 5).Draw(t, "instances")

		valueBytes := make([][]byte, instances)
		for i := 0; i < instances; i++ {
			value := rapid.MapOfN(cleanString, yamlValue, 0, 5).Draw(t, "value")

			bytes, err := yaml.Marshal(value)
			require.NoError(t, err)

			valueBytes[i] = bytes
		}

		helmMerged := helmPublicMerge(t, tempDir, valueBytes...)
		usMerged, err := MergeYAMLValues(valueBytes...)
		require.NoError(t, err)

		require.Equal(t, helmMerged, usMerged)
	})
}

// helmPublicMerge uses helm's public APIs to merge a set of values and is used
// as an oracle in our tests to ensure our implementation behaves the same way.
func helmPublicMerge(t *rapid.T, tempDir string, vs ...[]byte) map[string]any {
	var opts values.Options
	for i, v := range vs {
		path := path.Join(tempDir, fmt.Sprintf("values-%d.yaml", i))
		require.NoError(t, os.WriteFile(path, v, 0o664))

		opts.ValueFiles = append(opts.ValueFiles, path)
	}

	merged, err := opts.MergeValues(nil)
	require.NoError(t, err)

	return merged
}
