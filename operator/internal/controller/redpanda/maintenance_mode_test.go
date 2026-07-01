// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestPodNotReadyFor pins the "down duration" derived from the pod's Ready
// condition transition time — the restart-surviving signal used to gate the
// maintenance-mode clear behind a sustained-down threshold.
func TestPodNotReadyFor(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	pod := func(status corev1.ConditionStatus, since time.Time) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.NewTime(since),
		}}}}
	}

	t.Run("ready pod is not counted", func(t *testing.T) {
		_, notReady := podNotReadyFor(pod(corev1.ConditionTrue, now.Add(-time.Hour)), now)
		assert.False(t, notReady)
	})
	t.Run("not-ready pod returns duration since transition", func(t *testing.T) {
		d, notReady := podNotReadyFor(pod(corev1.ConditionFalse, now.Add(-8*time.Minute)), now)
		assert.True(t, notReady)
		assert.Equal(t, 8*time.Minute, d)
	})
	t.Run("pod with no Ready condition counts as not-ready for zero duration", func(t *testing.T) {
		_, notReady := podNotReadyFor(&corev1.Pod{}, now)
		assert.True(t, notReady)
	})
}

// TestDecideClearMaintenance pins the guard: only clear maintenance when the
// broker is in maintenance (draining) AND reported not-alive AND its pod has
// been not-Ready at least the threshold.
func TestDecideClearMaintenance(t *testing.T) {
	const threshold = 5 * time.Minute
	cases := []struct {
		name          string
		inMaintenance bool
		isAlive       bool
		notReadyFor   time.Duration
		want          bool
	}{
		{"in maintenance + down + past threshold -> clear", true, false, 6 * time.Minute, true},
		{"not in maintenance -> no clear", false, false, 30 * time.Minute, false},
		{"in maintenance but alive -> no clear", true, true, 30 * time.Minute, false},
		{"in maintenance + down but under threshold -> no clear", true, false, 2 * time.Minute, false},
		{"exactly at threshold -> clear", true, false, 5 * time.Minute, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decideClearMaintenance(tc.inMaintenance, tc.isAlive, tc.notReadyFor, threshold)
			assert.Equal(t, tc.want, got, "reason: %s", reason)
		})
	}
}

// TestBrokersByPodName maps a broker's advertised RPC address to the pod name
// (first DNS label), handling both "host" and "host:port" forms, so a
// persistently-down broker's pod can be located from the cluster broker list.
func TestBrokersByPodName(t *testing.T) {
	brokers := []rpadmin.Broker{
		{NodeID: 0, InternalRPCAddress: "redpanda-rp-east-0.redpanda", IsAlive: ptr.To(false), Maintenance: &rpadmin.MaintenanceStatus{Draining: true}},
		{NodeID: 2, InternalRPCAddress: "redpanda-rp-west-0.redpanda:33145", IsAlive: ptr.To(true)},
		{NodeID: 5, InternalRPCAddress: "10.140.1.15", IsAlive: ptr.To(true)},
	}
	m := brokersByPodName(brokers)
	assert.Equal(t, 0, m["redpanda-rp-east-0"].NodeID)
	assert.True(t, m["redpanda-rp-east-0"].Maintenance.Draining)
	assert.Equal(t, 2, m["redpanda-rp-west-0"].NodeID)
	assert.Equal(t, 5, m["10.140.1.15"].NodeID)
	_, ok := m["nonexistent"]
	assert.False(t, ok)
}
