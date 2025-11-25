// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestConfigFromV2(t *testing.T) {
	cases, err := txtar.ParseFile("testdata/migrate-cases.txtar")
	require.NoError(t, err)

	goldens := testutil.NewTxTar(t, "testdata/migrate-cases.golden.txtar")

	for _, tc := range cases.Files {
		t.Run(tc.Name, func(t *testing.T) {
			var input map[string]any
			require.NoError(t, yaml.Unmarshal(tc.Data, &input))

			converted, warnings, errJSONPath := ConfigFromV2(input)
			require.NoError(t, errJSONPath)

			actual, err := yaml.Marshal(map[string]any{
				"output":   converted,
				"warnings": warnings,
			})
			require.NoError(t, err)

			goldens.AssertGolden(t, testutil.YAML, tc.Name, append(actual, '\n'))
		})
	}
}
