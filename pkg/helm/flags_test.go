// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

type Flags struct {
	NoWait        bool `flag:"wait"`
	NoWaitForJobs bool `flag:"no-wait-for-jobs"`
	NotAFlag      string
	StringFlag    string   `flag:"string-flag"`
	StringArray   []string `flag:"string-array"`
}

func TestToFlags(t *testing.T) {
	testCases := []struct {
		in  Flags
		out []string
	}{
		{
			in: Flags{},
			out: []string{
				"--wait=true",
				"--no-wait-for-jobs=false",
			},
		},
		{
			in: Flags{},
			out: []string{
				"--wait=true",
				"--no-wait-for-jobs=false",
			},
		},
		{
			in: Flags{
				StringFlag: "something",
			},
			out: []string{
				"--wait=true",
				"--no-wait-for-jobs=false",
				"--string-flag=something",
			},
		},
		{
			in: Flags{
				StringFlag:  "something",
				StringArray: []string{"1", "2", "3"},
			},
			out: []string{
				"--wait=true",
				"--no-wait-for-jobs=false",
				"--string-flag=something",
				"--string-array=1",
				"--string-array=2",
				"--string-array=3",
			},
		},
	}

	for _, tc := range testCases {
		require.Equal(t, tc.out, helm.ToFlags(tc.in))
	}
}
