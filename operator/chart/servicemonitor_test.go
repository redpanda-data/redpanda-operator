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
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// renderServiceMonitor renders the operator ServiceMonitor with the given
// partial values merged over the chart defaults.
func renderServiceMonitor(t *testing.T, partial map[string]any) *monitoringv1.ServiceMonitor {
	t.Helper()
	values, err := Chart.LoadValues(partial)
	require.NoError(t, err)
	dot, err := Chart.Dot(nil, helmette.Release{
		Name:      "rp-op",
		Namespace: "redpanda-operator",
		Service:   "Helm",
	}, values)
	require.NoError(t, err)
	return ServiceMonitor(dot)
}

// multiclusterValues is the minimum multicluster stanza that renders, so the
// inherit path can be exercised without dragging in a full stretch config.
func multiclusterValues(name string) map[string]any {
	return map[string]any{
		"enabled":                  true,
		"name":                     name,
		"apiServerExternalAddress": "https://ABCD.gr7.us-east-1.eks.amazonaws.com",
		"peers": []any{
			map[string]any{"name": name, "address": "a.us-east-1.elb.amazonaws.com"},
		},
	}
}

func relabelings(t *testing.T, sm *monitoringv1.ServiceMonitor) []monitoringv1.RelabelConfig {
	t.Helper()
	require.NotNil(t, sm)
	require.Len(t, sm.Spec.Endpoints, 1)
	return sm.Spec.Endpoints[0].RelabelConfigs
}

// TestServiceMonitorClusterLabel covers monitoring.clusterLabel. The label is
// motivated by stretch clusters, but nothing about it is gated on multicluster:
// any deployment that aggregates several clusters' operator metrics into one
// Prometheus-compatible backend has the same collision to solve, since job,
// namespace, service and pod names are identical across clusters. Multicluster
// only supplies a default for the value.
func TestServiceMonitorClusterLabel(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{"enabled": true},
		})
		assert.Empty(t, relabelings(t, sm), "a single-cluster install has nothing to disambiguate")
	})

	// The case this exists for outside of stretch: N independent single-cluster
	// installs, each remote_writing to one central Prometheus. No multicluster
	// anywhere, so the value is supplied per install and the label name is left
	// at its default.
	t.Run("single cluster: value only, default label name", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled": true,
				"clusterLabel": map[string]any{
					"enabled": true,
					"value":   "prod-us-east-1",
				},
			},
		})

		require.Equal(t, []monitoringv1.RelabelConfig{{
			TargetLabel: "redpanda_k8s_cluster",
			Replacement: ptrTo("prod-us-east-1"),
		}}, relabelings(t, sm))
	})

	t.Run("single cluster: explicit label name", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled": true,
				"clusterLabel": map[string]any{
					"enabled": true,
					"name":    "k8s_cluster",
					"value":   "prod-eu-west-1",
				},
			},
		})

		require.Equal(t, []monitoringv1.RelabelConfig{{
			TargetLabel: "k8s_cluster",
			Replacement: ptrTo("prod-eu-west-1"),
		}}, relabelings(t, sm))
	})

	t.Run("multicluster: value inherited from multicluster.name", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled":      true,
				"clusterLabel": map[string]any{"enabled": true},
			},
			"multicluster": multiclusterValues("rp-us-east-1"),
		})

		require.Equal(t, []monitoringv1.RelabelConfig{{
			TargetLabel: "redpanda_k8s_cluster",
			Replacement: ptrTo("rp-us-east-1"),
		}}, relabelings(t, sm))
	})

	t.Run("multicluster: explicit value wins over multicluster.name", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled": true,
				"clusterLabel": map[string]any{
					"enabled": true,
					"value":   "east",
				},
			},
			"multicluster": multiclusterValues("rp-us-east-1"),
		})

		require.Equal(t, []monitoringv1.RelabelConfig{{
			TargetLabel: "redpanda_k8s_cluster",
			Replacement: ptrTo("east"),
		}}, relabelings(t, sm))
	})

	// An empty replacement would stamp an empty label, which reads as data
	// rather than as missing configuration, so the render fails instead.
	t.Run("no value and no multicluster.name fails the render", func(t *testing.T) {
		assert.PanicsWithValue(t, "monitoring.clusterLabel.value must be set unless multicluster.enabled is true and multicluster.name is non-empty", func() {
			renderServiceMonitor(t, map[string]any{
				"monitoring": map[string]any{
					"enabled":      true,
					"clusterLabel": map[string]any{"enabled": true},
				},
			})
		})
	})

	// The fallback is gated on multicluster mode being ENABLED, not merely on
	// multicluster.name being non-empty. That name is a plain string settable
	// with multicluster.enabled false, where it configures nothing else — so
	// inheriting it there would turn a stale or aspirational name into a metric
	// label with nothing pointing at the connection. Raised in review on #1758.
	t.Run("multicluster.name is not inherited when multicluster is disabled", func(t *testing.T) {
		assert.PanicsWithValue(t, "monitoring.clusterLabel.value must be set unless multicluster.enabled is true and multicluster.name is non-empty", func() {
			renderServiceMonitor(t, map[string]any{
				"monitoring": map[string]any{
					"enabled":      true,
					"clusterLabel": map[string]any{"enabled": true},
				},
				// Set, but multicluster mode is off.
				"multicluster": map[string]any{
					"enabled": false,
					"name":    "rp-us-east-1",
				},
			})
		})
	})

	// #1752 made the multicluster metrics server serve TLS so this endpoint
	// shape works; the label must not perturb any of it.
	t.Run("endpoint shape is untouched", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled": true,
				"clusterLabel": map[string]any{
					"enabled": true,
					"value":   "prod-us-east-1",
				},
			},
		})

		endpoint := sm.Spec.Endpoints[0]
		assert.Equal(t, "https", endpoint.Port)
		assert.Equal(t, "/metrics", endpoint.Path)
		require.NotNil(t, endpoint.Scheme)
		assert.Equal(t, monitoringv1.Scheme("https"), *endpoint.Scheme)
		require.NotNil(t, endpoint.TLSConfig)
		assert.Equal(t, ptrTo(true), endpoint.TLSConfig.InsecureSkipVerify)
		// The chart sets the deprecated BearerTokenFile deliberately (see
		// servicemonitor.go); asserting it is how that choice stays pinned.
		assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", endpoint.BearerTokenFile) //nolint:staticcheck // SA1019: pinning the field the chart intentionally sets
	})

	t.Run("monitoring disabled renders nothing", func(t *testing.T) {
		sm := renderServiceMonitor(t, map[string]any{
			"monitoring": map[string]any{
				"enabled":      false,
				"clusterLabel": map[string]any{"enabled": true, "value": "prod"},
			},
		})
		assert.Nil(t, sm)
	})
}

func ptrTo[T any](v T) *T { return &v }
