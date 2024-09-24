// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package envsubst_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/envsubst"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils/testutils"
	"github.com/stretchr/testify/require"
)

func TestCommand(t *testing.T) {
	t.Setenv("KEY_1", "value 1")
	t.Setenv("KEY_2", "value 2")

	cases := []struct {
		In  string
		Out string
	}{
		{In: "", Out: ""},
		{In: "$KEY_1", Out: "value 1"},
		{In: "Foo: bar\nBar: ${KEY_2}", Out: "Foo: bar\nBar: value 2"},
	}

	for _, tc := range cases {
		path := testutils.WriteFile(t, "*", []byte(tc.In))

		// To stdout
		{
			var out bytes.Buffer
			cmd := envsubst.Command()
			cmd.SetArgs([]string{path})
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			require.Equal(t, tc.Out, out.String())
		}

		// To a file
		{
			outfile := t.TempDir() + "/output.txt"

			cmd := envsubst.Command()
			cmd.SetArgs([]string{path, "--output", outfile})
			require.NoError(t, cmd.Execute())

			data, err := os.ReadFile(outfile)
			require.NoError(t, err)
			require.Equal(t, tc.Out, string(data))
		}
	}
}
