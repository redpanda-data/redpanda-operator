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

func assertConditionStatusReason(t *testing.T, name string, status metav1.ConditionStatus, reason string, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			assert.Equal(t, status, condition.Status, "%s should be %v but was %v", name, status, condition.Status)
			assert.Equal(t, reason, condition.Reason, "%s should have reason %v but was %v", name, reason, condition.Reason)
			return
		}
	}

	t.Errorf("did not find condition with the name %q", name)
}

func assertNoCondition(t *testing.T, name string, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			t.Errorf("found condition %q with reason %q and status %v when there should be none", name, condition.Reason, condition.Status)
			return
		}
	}
}

type setClusterFunc func(status *ClusterStatus)

func TestCluster(t *testing.T) {
	// regular condition tests
	for name, tt := range map[string]struct {
		condition string
		reason    string
		expected  metav1.ConditionStatus
		setFn     setClusterFunc
	}{
		"Ready/Ready": {
			condition: ClusterReady,
			reason:    string(ClusterReadyReasonReady),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
		},
		"Ready/NotReady": {
			condition: ClusterReady,
			reason:    string(ClusterReadyReasonNotReady),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonNotReady, "reason") },
		},
		"Ready/Error": {
			condition: ClusterReady,
			reason:    string(ClusterReadyReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonError, "reason") },
		},
		"Ready/TerminalError": {
			condition: ClusterReady,
			reason:    string(ClusterReadyReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonTerminalError, "reason") },
		},
		"Healthy/Healthy": {
			condition: ClusterHealthy,
			reason:    string(ClusterHealthyReasonHealthy),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
		},
		"Healthy/NotHealthy": {
			condition: ClusterHealthy,
			reason:    string(ClusterHealthyReasonNotHealthy),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonNotHealthy, "reason") },
		},
		"Healthy/Error": {
			condition: ClusterHealthy,
			reason:    string(ClusterHealthyReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonError, "reason") },
		},
		"Healthy/TerminalError": {
			condition: ClusterHealthy,
			reason:    string(ClusterHealthyReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonTerminalError, "reason") },
		},
		"LicenseValid/Valid": {
			condition: ClusterLicenseValid,
			reason:    string(ClusterLicenseValidReasonValid),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
		},
		"LicenseValid/Expired": {
			condition: ClusterLicenseValid,
			reason:    string(ClusterLicenseValidReasonExpired),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonExpired, "reason") },
		},
		"LicenseValid/NotPresent": {
			condition: ClusterLicenseValid,
			reason:    string(ClusterLicenseValidReasonNotPresent),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonNotPresent, "reason") },
		},
		"LicenseValid/Error": {
			condition: ClusterLicenseValid,
			reason:    string(ClusterLicenseValidReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonError, "reason") },
		},
		"LicenseValid/TerminalError": {
			condition: ClusterLicenseValid,
			reason:    string(ClusterLicenseValidReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonTerminalError, "reason") },
		},
		"ResourcesSynced/Synced": {
			condition: ClusterResourcesSynced,
			reason:    string(ClusterResourcesSyncedReasonSynced),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
		},
		"ResourcesSynced/Error": {
			condition: ClusterResourcesSynced,
			reason:    string(ClusterResourcesSyncedReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonError, "reason") },
		},
		"ResourcesSynced/TerminalError": {
			condition: ClusterResourcesSynced,
			reason:    string(ClusterResourcesSyncedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *ClusterStatus) {
				status.SetResourcesSynced(ClusterResourcesSyncedReasonTerminalError, "reason")
			},
		},
		"ConfigurationApplied/Applied": {
			condition: ClusterConfigurationApplied,
			reason:    string(ClusterConfigurationAppliedReasonApplied),
			expected:  metav1.ConditionTrue,
			setFn: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
			},
		},
		"ConfigurationApplied/Error": {
			condition: ClusterConfigurationApplied,
			reason:    string(ClusterConfigurationAppliedReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonError, "reason")
			},
		},
		"ConfigurationApplied/TerminalError": {
			condition: ClusterConfigurationApplied,
			reason:    string(ClusterConfigurationAppliedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonTerminalError, "reason")
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewCluster(0)

			assertNoCondition(t, tt.condition, status.Conditions())
			tt.setFn(status)
			assertConditionStatusReason(t, tt.condition, tt.expected, tt.reason, status.Conditions())
		})
	}

	// final conditions tests
	for name, conditionReason := range map[string]struct {
		condition   string
		trueReason  string
		falseReason string
	}{
		"Quiesced": {
			condition:   ClusterQuiesced,
			trueReason:  string(ClusterQuiescedReasonQuiesced),
			falseReason: string(ClusterQuiescedReasonStillReconciling),
		},
	} {
		conditionReason := conditionReason
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewCluster(0)

			// attempt to set all conditions one by one until they are all set
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.SetReady(ClusterReadyReasonReady, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.SetHealthy(ClusterHealthyReasonHealthy, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionTrue, conditionReason.trueReason, status.Conditions())
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

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.Conditions())

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.Conditions())
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

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.Conditions())

			setFn(status)

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionTrue, string(ClusterQuiescedReasonQuiesced), status.Conditions())
		})
	}

	// rollup conditions tests
	for name, tt := range map[string]struct {
		condition      string
		trueReason     string
		falseReason    string
		falseCondition setClusterFunc
		trueConditions []setClusterFunc
	}{
		"Rollup Conditions: Stable, All True": {
			condition:   ClusterStable,
			trueReason:  string(ClusterStableReasonStable),
			falseReason: string(ClusterStableReasonUnstable),
			trueConditions: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: Ready": {
			condition:      ClusterStable,
			trueReason:     string(ClusterStableReasonStable),
			falseReason:    string(ClusterStableReasonUnstable),
			falseCondition: func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonTerminalError, "reason") },
			trueConditions: []setClusterFunc{
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: LicenseValid": {
			condition:      ClusterStable,
			trueReason:     string(ClusterStableReasonStable),
			falseReason:    string(ClusterStableReasonUnstable),
			falseCondition: func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonTerminalError, "reason") },
			trueConditions: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: ResourcesSynced": {
			condition:   ClusterStable,
			trueReason:  string(ClusterStableReasonStable),
			falseReason: string(ClusterStableReasonUnstable),
			falseCondition: func(status *ClusterStatus) {
				status.SetResourcesSynced(ClusterResourcesSyncedReasonTerminalError, "reason")
			},
			trueConditions: []setClusterFunc{
				func(status *ClusterStatus) { status.SetReady(ClusterReadyReasonReady, "reason") },
				func(status *ClusterStatus) { status.SetHealthy(ClusterHealthyReasonHealthy, "reason") },
				func(status *ClusterStatus) { status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason") },
				func(status *ClusterStatus) {
					status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: ConfigurationApplied": {
			condition:   ClusterStable,
			trueReason:  string(ClusterStableReasonStable),
			falseReason: string(ClusterStableReasonUnstable),
			falseCondition: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonTerminalError, "reason")
			},
			trueConditions: []setClusterFunc{
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

			assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.Conditions())

			if tt.falseCondition != nil {
				tt.falseCondition(status)
			}
			for _, setFn := range tt.trueConditions {
				setFn(status)
			}

			if tt.falseCondition != nil {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.Conditions())
			} else {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionTrue, tt.trueReason, status.Conditions())
			}
		})
	}
}
