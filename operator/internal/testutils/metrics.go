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

	"github.com/cockroachdb/errors"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// RequireGauge asserts that the registry the operator exports carries a gauge
// series named name whose labels include every pair in labels, with value want.
func RequireGauge(t *testing.T, name string, labels map[string]string, want float64) {
	t.Helper()
	got, found, err := GaugeValue(name, labels)
	require.NoError(t, err)
	require.True(t, found, "%s%v not exported", name, labels)
	require.Equal(t, want, got, "%s%v", name, labels)
}

// RequireNoGauge asserts that no gauge series named name carries every label
// pair in labels. `vec.WithLabelValues(...)` cannot check this: it creates the
// series it looks up.
func RequireNoGauge(t *testing.T, name string, labels map[string]string) {
	t.Helper()
	_, found, err := GaugeValue(name, labels)
	require.NoError(t, err)
	require.False(t, found, "%s%v still exported", name, labels)
}

// GaugeValue reads the gauge series named name whose labels include every
// pair in labels from the registry the operator exports. It fails no test
// itself, so a polling assertion can call it from its own goroutine.
func GaugeValue(name string, labels map[string]string) (float64, bool, error) {
	gatherer, ok := ctrlmetrics.Registry.(prometheus.Gatherer)
	if !ok {
		return 0, false, errors.New("controller-runtime metrics.Registry does not implement prometheus.Gatherer")
	}
	families, err := gatherer.Gather()
	if err != nil {
		return 0, false, err
	}

	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if g := m.GetGauge(); g != nil && labelsContain(m.GetLabel(), labels) {
				return g.GetValue(), true, nil
			}
		}
	}
	return 0, false, nil
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
