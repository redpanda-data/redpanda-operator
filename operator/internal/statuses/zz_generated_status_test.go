// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package statuses

// GENERATED from statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertConditionStatus(t *testing.T, name string, status metav1.ConditionStatus, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			assert.Equal(t, status, condition.Status)
			return
		}
	}

	t.Errorf("did not find condition with the name %q", name)
}

type setClusterFunc func(status *ClusterStatus)

func TestCluster(t *testing.T) {

	// final conditions tests
	for name, condition := range map[string]string{
		"Quiesced": ClusterQuiesced,
	} {
		condition := condition
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewCluster(0)

			// attempt to set all conditions one by one until they are all set
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.SetReady(ClusterReadyReasonReady, "reason")
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.SetHealthy(ClusterHealthyReasonHealthy, "reason")
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason")
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason")
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
			assertConditionStatus(t, condition, metav1.ConditionTrue, status.Conditions())
		})
	}

	// transient error tests
	for name, tt := range map[string]struct {
		setTransientErrFn   setClusterFunc
		setConditionReasons []setClusterFunc
	}{
		"Transient Error: Error, Condition: Ready": {
			setTransientErrFn: func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonError, "reason") },
			setConditionReasons: []setClusterFunc{
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: Healthy": {
			setTransientErrFn: func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonError, "reason") },
			setConditionReasons: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: LicenseValid": {
			setTransientErrFn: func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonError, "reason") },
			setConditionReasons: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: ResourcesSynced": {
			setTransientErrFn: func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonError, "reason") },
			setConditionReasons: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: ConfigurationApplied": {
			setTransientErrFn: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonError, "reason")
			},
			setConditionReasons: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewCluster(0)

			assertConditionStatus(t, ClusterQuiesced, metav1.ConditionFalse, status.Conditions())

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			assertConditionStatus(t, ClusterQuiesced, metav1.ConditionFalse, status.Conditions())
		})
	}

	// terminal error tests
	for name, setFn := range map[string]setClusterFunc{
		"Terminal Error: TerminalError, Condition: Ready":        func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonTerminalError, "reason") },
		"Terminal Error: TerminalError, Condition: Healthy":      func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonTerminalError, "reason") },
		"Terminal Error: TerminalError, Condition: LicenseValid": func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonTerminalError, "reason") },
		"Terminal Error: TerminalError, Condition: ResourcesSynced": func(status *ClusterStatus) {
			status.SetResourcesSynced(ClusterResourcesSyncedReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: ConfigurationApplied": func(status *ClusterStatus) {
			status.SetConfigurationApplied(ClusterConfigurationAppliedReasonTerminalError, "reason")
		},
	} {
		setFn := setFn
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewCluster(0)

			assertConditionStatus(t, ClusterQuiesced, metav1.ConditionFalse, status.Conditions())

			setFn(status)

			assertConditionStatus(t, ClusterQuiesced, metav1.ConditionTrue, status.Conditions())
		})
	}
}
