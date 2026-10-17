// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// TestCPUResources renders through helm so that `--set` reaches the chart as
// helm parses it: `--set resources.cpu.cores=2` arrives as an int64, which a
// values file (float64) never exercises.
func TestCPUResources(t *testing.T) {
	ctx := testutil.Context(t)

	tmp := testutil.TempDir(t)
	require.NoError(t, redpandachart.Chart.Write(tmp))
	chartDir := filepath.Join(tmp, "redpanda")

	client, err := helm.New(helm.Options{ConfigHome: tmp})
	require.NoError(t, err)

	cases := []struct {
		name          string
		set           []string
		wantOutput    []string
		wantNotOutput []string
	}{
		{
			name:       "integer cores from --set",
			set:        []string{"resources.cpu.cores=2"},
			wantOutput: []string{"--smp=2"},
		},
		{
			name:       "string millicores from --set",
			set:        []string{"resources.cpu.cores=500m"},
			wantOutput: []string{"--smp=1", "overprovisioned: true"},
		},
		{
			name: "requests and limits without cpu",
			set: []string{
				"resources.requests.memory=3Gi",
				"resources.limits.memory=3Gi",
			},
			wantNotOutput: []string{"--smp=1"},
		},
		{
			name: "requests and limits with cpu",
			set: []string{
				"resources.requests.cpu=2",
				"resources.requests.memory=3Gi",
				"resources.limits.cpu=2",
				"resources.limits.memory=3Gi",
			},
			wantOutput: []string{"--smp=2"},
		},
		{
			name: "only requests set",
			set: []string{
				"resources.requests.memory=3Gi",
			},
			wantOutput: []string{"--smp=1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := client.Template(ctx, chartDir, helm.TemplateOptions{
				Name: "redpanda",
				Set: append([]string{
					"tests.enabled=false",
					"console.secret.authentication.jwtSigningKey=SECRETKEY",
					"auth.sasl.bootstrapUser.password=changeme",
				}, tc.set...),
			})
			require.NoError(t, err)

			for _, want := range tc.wantOutput {
				require.Contains(t, string(out), want+"\n")
			}
			for _, unwanted := range tc.wantNotOutput {
				require.NotContains(t, string(out), unwanted+"\n")
			}
		})
	}
}
