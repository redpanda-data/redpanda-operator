// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lint

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

const tagURL = "https://github.com/redpanda-data/redpanda-operator/releases/tag/"

type ChartYAML struct {
	Version     string            `json:"version"`
	AppVersion  string            `json:"appVersion"`
	Annotations map[string]string `json:"annotations"`
}

func TestToolVersions(t *testing.T) {
	golden := testutil.NewTxTar(t, "testdata/tool-versions.txtar")

	for _, cmd := range []string{
		"go version | cut -d ' ' -f 1-3",                           // cut removes os/arch
		"goreleaser --version | awk 'NR>2 {print last} {last=$0}'", // head -n -1 doesn't work on macos
		"helm version",
		"k3d version",
		"kind version | cut -d ' ' -f 1-3", // cut removes os/arch
		"kubectl version --client=true",
		"kustomize version",
		"kuttl version | cut -d ' ' -f 1-7", // cut removes os/arch
		"task --version",
		"yq --version",
		"helm-docs -v",
		"ct version",
	} {
		out := sh(cmd)
		bin := strings.SplitN(cmd, " ", 2)[0]
		expect := fmt.Sprintf("# %s\n%s\n", cmd, out)
		golden.AssertGolden(t, testutil.Text, bin, []byte(expect))
	}
}

func sh(cmd string) string {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("ERROR: %s\n%s", err.Error(), out)
	}
	return string(out)
}

func TestChartYAMLVersions(t *testing.T) {
	// This test isn't currently valid due to the repo migration.
	t.Skipf("CHANGELOG.md does not exist in this repo")

	chartYAMLs, err := fs.Glob(os.DirFS("../.."), "charts/*/Chart.yaml")
	require.NoError(t, err)

	changelog, err := os.ReadFile("../../CHANGELOG.md")
	require.NoError(t, err)

	for _, chartYAML := range chartYAMLs {
		chartBytes, err := os.ReadFile("../../" + chartYAML)
		require.NoError(t, err)

		var chart map[string]any
		require.NoError(t, yaml.Unmarshal(chartBytes, &chart))

		chartName := chart["name"].(string)
		chartVersion := chart["version"].(string)

		releaseHeader := fmt.Sprintf("### [%s](%s%s-%s)", chartVersion, tagURL, chartName, chartVersion)

		// require.Contains is noisy with a large file. Fallback to
		// require.True for friendlier messages.
		assert.Truef(
			t,
			bytes.Contains(changelog, []byte(releaseHeader)),
			"CHANGELOG.md is missing the release header for %s %s\nDid you forget to add it?\n%s",
			chartName,
			chartVersion,
			releaseHeader,
		)
	}
}

func TestOperatorArtifactHubImages(t *testing.T) {
	const operatorRepo = "docker.redpanda.com/redpandadata/redpanda-operator"
	const configuratorRepo = "docker.redpanda.com/redpandadata/configurator"

	chartBytes, err := os.ReadFile("../../charts/operator/Chart.yaml")
	require.NoError(t, err)

	var chart ChartYAML
	require.NoError(t, yaml.Unmarshal(chartBytes, &chart))

	assert.Contains(
		t,
		chart.Annotations["artifacthub.io/images"],
		fmt.Sprintf("%s:%s", operatorRepo, chart.AppVersion),
		"artifacthub.io/images should be in sync with .appVersion",
	)

	assert.Contains(
		t,
		chart.Annotations["artifacthub.io/images"],
		fmt.Sprintf("%s:%s", configuratorRepo, chart.AppVersion),
		"artifacthub.io/images should be in sync with .appVersion",
	)
}

func TestOperatorKustomizationTag(t *testing.T) {
	chartBytes, err := os.ReadFile("../../charts/operator/Chart.yaml")
	require.NoError(t, err)

	var chart map[string]any
	require.NoError(t, yaml.Unmarshal(chartBytes, &chart))

	kustomizationBytes, err := os.ReadFile("../../charts/operator/testdata/kustomization.yaml")
	require.NoError(t, err)

	var kustomization map[string]any
	require.NoError(t, yaml.Unmarshal(kustomizationBytes, &kustomization))

	for _, addr := range kustomization["resources"].([]any) {
		require.Contains(
			t,
			addr,
			chart["appVersion"].(string),
			"testdata kustomization address tag should be equal to the operator chart's appVersion",
		)
	}
}
