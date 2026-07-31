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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestDefaultPhase pins the one-way gate out of Provisioning: once a broker
// has registered (Status.BrokerID set), a pod readiness dip must not regress
// its phase — the readiness probe is cluster-scoped, so one restarting
// broker flips every pod unready and would otherwise flap every registered
// broker's phase Running→Provisioning.
func TestDefaultPhase(t *testing.T) {
	registered := &redpandav1alpha2.Broker{}
	registered.Status.BrokerID = ptr.To(int32(4))
	unregistered := &redpandav1alpha2.Broker{}

	readyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}}}
	unreadyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionFalse},
	}}}
	unschedulablePod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
	}}}

	for name, tc := range map[string]struct {
		broker *redpandav1alpha2.Broker
		pod    *corev1.Pod
		want   redpandav1alpha2.BrokerPhase
	}{
		"unregistered, pod not ready":  {unregistered, unreadyPod, redpandav1alpha2.BrokerPhaseProvisioning},
		"unregistered, pod ready":      {unregistered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, pod ready":        {registered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, readiness dip":    {registered, unreadyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, no pod yet":       {registered, &corev1.Pod{}, redpandav1alpha2.BrokerPhaseRunning},
		"stuck overrides registration": {registered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
		"stuck overrides provisioning": {unregistered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, defaultPhase(tc.broker, tc.pod))
		})
	}
}
