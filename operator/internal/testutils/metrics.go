// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutils

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// GaugeSeries reports the value of the gauge series named name whose labels
// include every pair in labels, scraping the controller-runtime registry the
// operator exports. found is false when no such series exists, which
// `vec.WithLabelValues(...)` cannot tell apart from zero because it creates
// the series it looks up.
func GaugeSeries(t *testing.T, name string, labels map[string]string) (value float64, found bool) {
	t.Helper()

	gatherer, ok := ctrlmetrics.Registry.(prometheus.Gatherer)
	require.True(t, ok, "controller-runtime metrics.Registry must implement prometheus.Gatherer")
	families, err := gatherer.Gather()
	require.NoError(t, err)

	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if g := m.GetGauge(); g != nil && labelsContain(m.GetLabel(), labels) {
				return g.GetValue(), true
			}
		}
	}
	return 0, false
}

func labelsContain(pairs []*dto.LabelPair, want map[string]string) bool {
	for k, v := range want {
		var found bool
		for _, lp := range pairs {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
