// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package enterprisedrift

import (
	"os"
	"regexp"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	entobservability "github.com/redpanda-data/redpanda-operator/enterprise/operator/observability"
)

// collectorFQNames returns the fully-qualified metric names a collector
// describes, extracted from its Desc strings (prometheus exposes no direct
// fqName accessor).
func collectorFQNames(t *testing.T, c prometheus.Collector) []string {
	t.Helper()
	fqName := regexp.MustCompile(`fqName: "([^"]+)"`)
	ch := make(chan *prometheus.Desc, 8)
	go func() {
		c.Describe(ch)
		close(ch)
	}()
	var names []string
	for desc := range ch {
		m := fqName.FindStringSubmatch(desc.String())
		require.NotNil(t, m, "could not extract fqName from %s", desc.String())
		names = append(names, m[1])
	}
	require.NotEmpty(t, names)
	return names
}

// TestStretchClusterMetricNamesPinnedToPrometheusRule pins the metric names
// exported by enterprise/operator/observability to the alert/recording-rule
// expressions in the operator chart's PrometheusRule
// (operator/chart/prometheusrule.go). The chart matches these names as plain
// strings, and the enterprise module cannot import the chart (nor vice
// versa), so a rename on either side would silently break dashboards and
// alerts. Each gauge's fully-qualified name must therefore keep appearing in
// the rule file's source.
func TestStretchClusterMetricNamesPinnedToPrometheusRule(t *testing.T) {
	// Relative to this package directory (go test runs with the package dir
	// as the working directory).
	src, err := os.ReadFile("../../chart/prometheusrule.go")
	require.NoError(t, err, "reading operator/chart/prometheusrule.go")
	rules := string(src)

	expected := map[string]prometheus.Collector{
		"operator_stretchcluster_member_reachable":   entobservability.StretchClusterMemberReachable,
		"operator_stretchcluster_brokers":            entobservability.StretchClusterBrokers,
		"operator_stretchcluster_brokers_ready":      entobservability.StretchClusterBrokersReady,
		"operator_stretchcluster_replication_health": entobservability.StretchClusterReplicationHealth,
		"operator_stretchcluster_spec_drift":         entobservability.StretchClusterSpecDrift,
	}
	for name, collector := range expected {
		require.Equal(t, []string{name}, collectorFQNames(t, collector),
			"the enterprise gauge no longer exports the metric name the chart's PrometheusRule expressions match on")
		require.Contains(t, rules, name,
			"operator/chart/prometheusrule.go no longer references %q; if the rule was renamed on purpose, update the enterprise metric (and dashboards) in lockstep", name)
	}
}

// TestMaintenanceModeMetricNamesStable pins the maintenance-mode remediation
// counters' fully-qualified names. They moved wholesale from
// operator/internal/observability to the enterprise module; scrapers and
// dashboards depend on the names staying put even though no chart
// PrometheusRule references them today.
func TestMaintenanceModeMetricNamesStable(t *testing.T) {
	expected := map[string]prometheus.Collector{
		"operator_controller_maintenance_mode_cleared_total":                 entobservability.MaintenanceModeCleared,
		"operator_controller_maintenance_mode_ghost_cleared_total":           entobservability.MaintenanceModeGhostCleared,
		"operator_controller_maintenance_mode_clear_skipped_ambiguous_total": entobservability.MaintenanceModeClearSkippedAmbiguous,
	}
	for name, collector := range expected {
		require.Equal(t, []string{name}, collectorFQNames(t, collector))
	}
}
