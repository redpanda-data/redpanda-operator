// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lint_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/redpanda-data/helm-charts/pkg/testutil"
)

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
