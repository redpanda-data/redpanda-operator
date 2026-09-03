// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package operator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// TestPrometheusRules checks and unit-tests the chart's PrometheusRule with
// promtool — the only tool in this suite that actually evaluates PromQL. The
// rules are rendered in-process rather than committed, so the promtool
// scenarios in testdata/promtool always run against what the chart produces
// now: editing an expression in prometheusrule.go is enough to make them fail.
//
// Two installs are rendered because each one scopes its rules to its own job
// and namespace: the second file is what lets the cross-install scenarios see
// anything to collide with. The file names and release identities are pinned
// by the rule_files stanzas and label expectations of the scenario files.
func TestPrometheusRules(t *testing.T) {
	promtool, err := exec.LookPath("promtool")
	require.NoError(t, err, "promtool not found on PATH; run inside `nix develop`")

	dir := t.TempDir()

	ruleFiles := []string{"rules.generated.yaml", "rules-second-install.generated.yaml"}
	for name, release := range map[string]helmette.Release{
		ruleFiles[0]: {Name: "operator", Namespace: "redpanda-system", Service: "Helm"},
		ruleFiles[1]: {Name: "operator-two", Namespace: "redpanda-other", Service: "Helm"},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), renderRuleSpecYAML(t, release), 0o644))
	}

	// promtool resolves rule_files relative to the scenario file, so the
	// scenarios are copied next to the rendered rules.
	scenarios, err := filepath.Glob("testdata/promtool/*_test.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, scenarios, "no promtool scenario files found under testdata/promtool")

	for _, scenario := range scenarios {
		data, err := os.ReadFile(scenario)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.Base(scenario)), data, 0o644))
	}

	check := exec.Command(promtool, append([]string{"check", "rules"}, ruleFiles...)...)
	check.Dir = dir
	out, err := check.CombinedOutput()
	require.NoError(t, err, "promtool check rules failed:\n%s", out)

	for _, scenario := range scenarios {
		test := exec.Command(promtool, "test", "rules", filepath.Base(scenario))
		test.Dir = dir
		out, err := test.CombinedOutput()
		require.NoError(t, err, "promtool test rules %s failed:\n%s", filepath.Base(scenario), out)
	}
}

// renderRuleSpecYAML renders the PrometheusRule for one install and returns
// its spec as YAML — the shape `promtool check rules` expects.
func renderRuleSpecYAML(t *testing.T, release helmette.Release) []byte {
	t.Helper()

	values, err := Chart.LoadValues(map[string]any{
		"monitoring": map[string]any{"rulesEnabled": true},
	})
	require.NoError(t, err)

	dot, err := Chart.Dot(nil, release, values)
	require.NoError(t, err)

	rule := PrometheusRule(dot)
	require.NotNil(t, rule, "monitoring.rulesEnabled=true should render a PrometheusRule")

	spec, err := yaml.Marshal(rule.Spec)
	require.NoError(t, err)
	return spec
}
