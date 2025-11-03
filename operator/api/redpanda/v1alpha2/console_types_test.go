// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestConvertConsoleSubchartToConsoleValues(t *testing.T) {
	cases, err := txtar.ParseFile("testdata/console-migration-cases.txtar")
	require.NoError(t, err)

	goldens := testutil.NewTxTar(t, "testdata/console-migration-cases.golden.txtar")

	for i, tc := range cases.Files {
		t.Run(tc.Name, func(t *testing.T) {
			var in RedpandaConsole
			require.NoError(t, yaml.Unmarshal(tc.Data, &in))

			out, err := ConvertConsoleSubchartToConsoleValues(&in)
			require.NoError(t, err)

			actual, err := yaml.Marshal(out)
			require.NoError(t, err)

			// Add a bit of extra padding to make it easier to navigate the golden file.
			actual = append(actual, '\n')

			goldens.AssertGolden(t, testutil.YAML, fmt.Sprintf("%02d-%s", i, tc.Name), actual)
		})
	}
}
