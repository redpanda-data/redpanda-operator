package statuses

// GENERATED from ./statuses.yaml, DO NOT EDIT DIRECTLY

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
		"ConfigurationApplied/NotApplied": {
			condition: ClusterConfigurationApplied,
			reason:    string(ClusterConfigurationAppliedReasonNotApplied),
			expected:  metav1.ConditionFalse,
			setFn: func(status *ClusterStatus) {
				status.SetConfigurationApplied(ClusterConfigurationAppliedReasonNotApplied, "reason")
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

			status := NewCluster()

			assertNoCondition(t, tt.condition, status.getConditions(0))
			tt.setFn(status)
			assertConditionStatusReason(t, tt.condition, tt.expected, tt.reason, status.getConditions(0))
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

			status := NewCluster()

			// attempt to set all conditions one by one until they are all set
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetReady(ClusterReadyReasonReady, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetHealthy(ClusterHealthyReasonHealthy, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetLicenseValid(ClusterLicenseValidReasonValid, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionTrue, conditionReason.trueReason, status.getConditions(0))
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

			status := NewCluster()

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.getConditions(0))

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.getConditions(0))
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

			status := NewCluster()

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionFalse, string(ClusterQuiescedReasonStillReconciling), status.getConditions(0))

			setFn(status)

			assertConditionStatusReason(t, ClusterQuiesced, metav1.ConditionTrue, string(ClusterQuiescedReasonQuiesced), status.getConditions(0))
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

			status := NewCluster()

			assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))

			if tt.falseCondition != nil {
				tt.falseCondition(status)
			}
			for _, setFn := range tt.trueConditions {
				setFn(status)
			}

			if tt.falseCondition != nil {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))
			} else {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionTrue, tt.trueReason, status.getConditions(0))
			}
		})
	}
}

type setStretchClusterFunc func(status *StretchClusterStatus)

func TestStretchCluster(t *testing.T) {
	// regular condition tests
	for name, tt := range map[string]struct {
		condition string
		reason    string
		expected  metav1.ConditionStatus
		setFn     setStretchClusterFunc
	}{
		"Ready/Ready": {
			condition: StretchClusterReady,
			reason:    string(StretchClusterReadyReasonReady),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
		},
		"Ready/NotReady": {
			condition: StretchClusterReady,
			reason:    string(StretchClusterReadyReasonNotReady),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonNotReady, "reason") },
		},
		"Ready/Error": {
			condition: StretchClusterReady,
			reason:    string(StretchClusterReadyReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonError, "reason") },
		},
		"Ready/TerminalError": {
			condition: StretchClusterReady,
			reason:    string(StretchClusterReadyReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonTerminalError, "reason") },
		},
		"Healthy/Healthy": {
			condition: StretchClusterHealthy,
			reason:    string(StretchClusterHealthyReasonHealthy),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
		},
		"Healthy/NotHealthy": {
			condition: StretchClusterHealthy,
			reason:    string(StretchClusterHealthyReasonNotHealthy),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonNotHealthy, "reason") },
		},
		"Healthy/Error": {
			condition: StretchClusterHealthy,
			reason:    string(StretchClusterHealthyReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonError, "reason") },
		},
		"Healthy/TerminalError": {
			condition: StretchClusterHealthy,
			reason:    string(StretchClusterHealthyReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetHealthy(StretchClusterHealthyReasonTerminalError, "reason")
			},
		},
		"LicenseValid/Valid": {
			condition: StretchClusterLicenseValid,
			reason:    string(StretchClusterLicenseValidReasonValid),
			expected:  metav1.ConditionTrue,
			setFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
			},
		},
		"LicenseValid/Expired": {
			condition: StretchClusterLicenseValid,
			reason:    string(StretchClusterLicenseValidReasonExpired),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonExpired, "reason")
			},
		},
		"LicenseValid/NotPresent": {
			condition: StretchClusterLicenseValid,
			reason:    string(StretchClusterLicenseValidReasonNotPresent),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonNotPresent, "reason")
			},
		},
		"LicenseValid/Error": {
			condition: StretchClusterLicenseValid,
			reason:    string(StretchClusterLicenseValidReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonError, "reason")
			},
		},
		"LicenseValid/TerminalError": {
			condition: StretchClusterLicenseValid,
			reason:    string(StretchClusterLicenseValidReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonTerminalError, "reason")
			},
		},
		"ResourcesSynced/Synced": {
			condition: StretchClusterResourcesSynced,
			reason:    string(StretchClusterResourcesSyncedReasonSynced),
			expected:  metav1.ConditionTrue,
			setFn: func(status *StretchClusterStatus) {
				status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
			},
		},
		"ResourcesSynced/Error": {
			condition: StretchClusterResourcesSynced,
			reason:    string(StretchClusterResourcesSyncedReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetResourcesSynced(StretchClusterResourcesSyncedReasonError, "reason")
			},
		},
		"ResourcesSynced/TerminalError": {
			condition: StretchClusterResourcesSynced,
			reason:    string(StretchClusterResourcesSyncedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetResourcesSynced(StretchClusterResourcesSyncedReasonTerminalError, "reason")
			},
		},
		"ConfigurationApplied/Applied": {
			condition: StretchClusterConfigurationApplied,
			reason:    string(StretchClusterConfigurationAppliedReasonApplied),
			expected:  metav1.ConditionTrue,
			setFn: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
			},
		},
		"ConfigurationApplied/NotApplied": {
			condition: StretchClusterConfigurationApplied,
			reason:    string(StretchClusterConfigurationAppliedReasonNotApplied),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonNotApplied, "reason")
			},
		},
		"ConfigurationApplied/Error": {
			condition: StretchClusterConfigurationApplied,
			reason:    string(StretchClusterConfigurationAppliedReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonError, "reason")
			},
		},
		"ConfigurationApplied/TerminalError": {
			condition: StretchClusterConfigurationApplied,
			reason:    string(StretchClusterConfigurationAppliedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonTerminalError, "reason")
			},
		},
		"SpecSynced/Synced": {
			condition: StretchClusterSpecSynced,
			reason:    string(StretchClusterSpecSyncedReasonSynced),
			expected:  metav1.ConditionTrue,
			setFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
			},
		},
		"SpecSynced/DriftDetected": {
			condition: StretchClusterSpecSynced,
			reason:    string(StretchClusterSpecSyncedReasonDriftDetected),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonDriftDetected, "reason")
			},
		},
		"SpecSynced/ClusterUnreachable": {
			condition: StretchClusterSpecSynced,
			reason:    string(StretchClusterSpecSyncedReasonClusterUnreachable),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonClusterUnreachable, "reason")
			},
		},
		"SpecSynced/Error": {
			condition: StretchClusterSpecSynced,
			reason:    string(StretchClusterSpecSyncedReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonError, "reason")
			},
		},
		"SpecSynced/TerminalError": {
			condition: StretchClusterSpecSynced,
			reason:    string(StretchClusterSpecSyncedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonTerminalError, "reason")
			},
		},
		"BootstrapUserSynced/Synced": {
			condition: StretchClusterBootstrapUserSynced,
			reason:    string(StretchClusterBootstrapUserSyncedReasonSynced),
			expected:  metav1.ConditionTrue,
			setFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
			},
		},
		"BootstrapUserSynced/ExistingReused": {
			condition: StretchClusterBootstrapUserSynced,
			reason:    string(StretchClusterBootstrapUserSyncedReasonExistingReused),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonExistingReused, "reason")
			},
		},
		"BootstrapUserSynced/PasswordMismatch": {
			condition: StretchClusterBootstrapUserSynced,
			reason:    string(StretchClusterBootstrapUserSyncedReasonPasswordMismatch),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonPasswordMismatch, "reason")
			},
		},
		"BootstrapUserSynced/Error": {
			condition: StretchClusterBootstrapUserSynced,
			reason:    string(StretchClusterBootstrapUserSyncedReasonError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonError, "reason")
			},
		},
		"BootstrapUserSynced/TerminalError": {
			condition: StretchClusterBootstrapUserSynced,
			reason:    string(StretchClusterBootstrapUserSyncedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonTerminalError, "reason")
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewStretchCluster()

			assertNoCondition(t, tt.condition, status.getConditions(0))
			tt.setFn(status)
			assertConditionStatusReason(t, tt.condition, tt.expected, tt.reason, status.getConditions(0))
		})
	}

	// final conditions tests
	for name, conditionReason := range map[string]struct {
		condition   string
		trueReason  string
		falseReason string
	}{
		"Quiesced": {
			condition:   StretchClusterQuiesced,
			trueReason:  string(StretchClusterQuiescedReasonQuiesced),
			falseReason: string(StretchClusterQuiescedReasonStillReconciling),
		},
	} {
		conditionReason := conditionReason
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewStretchCluster()

			// attempt to set all conditions one by one until they are all set
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetReady(StretchClusterReadyReasonReady, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionTrue, conditionReason.trueReason, status.getConditions(0))
		})
	}

	// transient error tests
	for name, tt := range map[string]struct {
		setTransientErrFn   setStretchClusterFunc
		setConditionReasons []setStretchClusterFunc
	}{
		"Transient Error: Error, Condition: Ready": {
			setTransientErrFn: func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonError, "reason") },
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: Healthy": {
			setTransientErrFn: func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonError, "reason") },
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: LicenseValid": {
			setTransientErrFn: func(status *StretchClusterStatus) {
				status.SetLicenseValid(StretchClusterLicenseValidReasonError, "reason")
			},
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: ResourcesSynced": {
			setTransientErrFn: func(status *StretchClusterStatus) {
				status.SetResourcesSynced(StretchClusterResourcesSyncedReasonError, "reason")
			},
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: ConfigurationApplied": {
			setTransientErrFn: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonError, "reason")
			},
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: SpecSynced": {
			setTransientErrFn: func(status *StretchClusterStatus) {
				status.SetSpecSynced(StretchClusterSpecSyncedReasonError, "reason")
			},
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Transient Error: Error, Condition: BootstrapUserSynced": {
			setTransientErrFn: func(status *StretchClusterStatus) {
				status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonError, "reason")
			},
			setConditionReasons: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewStretchCluster()

			assertConditionStatusReason(t, StretchClusterQuiesced, metav1.ConditionFalse, string(StretchClusterQuiescedReasonStillReconciling), status.getConditions(0))

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			assertConditionStatusReason(t, StretchClusterQuiesced, metav1.ConditionFalse, string(StretchClusterQuiescedReasonStillReconciling), status.getConditions(0))
		})
	}

	// terminal error tests
	for name, setFn := range map[string]setStretchClusterFunc{
		"Terminal Error: TerminalError, Condition: Ready": func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonTerminalError, "reason") },
		"Terminal Error: TerminalError, Condition: Healthy": func(status *StretchClusterStatus) {
			status.SetHealthy(StretchClusterHealthyReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: LicenseValid": func(status *StretchClusterStatus) {
			status.SetLicenseValid(StretchClusterLicenseValidReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: ResourcesSynced": func(status *StretchClusterStatus) {
			status.SetResourcesSynced(StretchClusterResourcesSyncedReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: ConfigurationApplied": func(status *StretchClusterStatus) {
			status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: SpecSynced": func(status *StretchClusterStatus) {
			status.SetSpecSynced(StretchClusterSpecSyncedReasonTerminalError, "reason")
		},
		"Terminal Error: TerminalError, Condition: BootstrapUserSynced": func(status *StretchClusterStatus) {
			status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonTerminalError, "reason")
		},
	} {
		setFn := setFn
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewStretchCluster()

			assertConditionStatusReason(t, StretchClusterQuiesced, metav1.ConditionFalse, string(StretchClusterQuiescedReasonStillReconciling), status.getConditions(0))

			setFn(status)

			assertConditionStatusReason(t, StretchClusterQuiesced, metav1.ConditionTrue, string(StretchClusterQuiescedReasonQuiesced), status.getConditions(0))
		})
	}

	// rollup conditions tests
	for name, tt := range map[string]struct {
		condition      string
		trueReason     string
		falseReason    string
		falseCondition setStretchClusterFunc
		trueConditions []setStretchClusterFunc
	}{
		"Rollup Conditions: Stable, All True": {
			condition:   StretchClusterStable,
			trueReason:  string(StretchClusterStableReasonStable),
			falseReason: string(StretchClusterStableReasonUnstable),
			trueConditions: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: Ready": {
			condition:      StretchClusterStable,
			trueReason:     string(StretchClusterStableReasonStable),
			falseReason:    string(StretchClusterStableReasonUnstable),
			falseCondition: func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonTerminalError, "reason") },
			trueConditions: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: ResourcesSynced": {
			condition:   StretchClusterStable,
			trueReason:  string(StretchClusterStableReasonStable),
			falseReason: string(StretchClusterStableReasonUnstable),
			falseCondition: func(status *StretchClusterStatus) {
				status.SetResourcesSynced(StretchClusterResourcesSyncedReasonTerminalError, "reason")
			},
			trueConditions: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonApplied, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
		"Rollup Conditions: Stable, False Condition: ConfigurationApplied": {
			condition:   StretchClusterStable,
			trueReason:  string(StretchClusterStableReasonStable),
			falseReason: string(StretchClusterStableReasonUnstable),
			falseCondition: func(status *StretchClusterStatus) {
				status.SetConfigurationApplied(StretchClusterConfigurationAppliedReasonTerminalError, "reason")
			},
			trueConditions: []setStretchClusterFunc{
				func(status *StretchClusterStatus) { status.SetReady(StretchClusterReadyReasonReady, "reason") },
				func(status *StretchClusterStatus) { status.SetHealthy(StretchClusterHealthyReasonHealthy, "reason") },
				func(status *StretchClusterStatus) {
					status.SetLicenseValid(StretchClusterLicenseValidReasonValid, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetResourcesSynced(StretchClusterResourcesSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetSpecSynced(StretchClusterSpecSyncedReasonSynced, "reason")
				},
				func(status *StretchClusterStatus) {
					status.SetBootstrapUserSynced(StretchClusterBootstrapUserSyncedReasonSynced, "reason")
				},
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewStretchCluster()

			assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))

			if tt.falseCondition != nil {
				tt.falseCondition(status)
			}
			for _, setFn := range tt.trueConditions {
				setFn(status)
			}

			if tt.falseCondition != nil {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))
			} else {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionTrue, tt.trueReason, status.getConditions(0))
			}
		})
	}
}

type setNodePoolFunc func(status *NodePoolStatus)

func TestNodePool(t *testing.T) {
	// regular condition tests
	for name, tt := range map[string]struct {
		condition string
		reason    string
		expected  metav1.ConditionStatus
		setFn     setNodePoolFunc
	}{
		"Bound/Bound": {
			condition: NodePoolBound,
			reason:    string(NodePoolBoundReasonBound),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonBound, "reason") },
		},
		"Bound/NotBound": {
			condition: NodePoolBound,
			reason:    string(NodePoolBoundReasonNotBound),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonNotBound, "reason") },
		},
		"Bound/Error": {
			condition: NodePoolBound,
			reason:    string(NodePoolBoundReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonError, "reason") },
		},
		"Bound/TerminalError": {
			condition: NodePoolBound,
			reason:    string(NodePoolBoundReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonTerminalError, "reason") },
		},
		"Deployed/Deployed": {
			condition: NodePoolDeployed,
			reason:    string(NodePoolDeployedReasonDeployed),
			expected:  metav1.ConditionTrue,
			setFn:     func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonDeployed, "reason") },
		},
		"Deployed/Scaling": {
			condition: NodePoolDeployed,
			reason:    string(NodePoolDeployedReasonScaling),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonScaling, "reason") },
		},
		"Deployed/NotDeployed": {
			condition: NodePoolDeployed,
			reason:    string(NodePoolDeployedReasonNotDeployed),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonNotDeployed, "reason") },
		},
		"Deployed/Error": {
			condition: NodePoolDeployed,
			reason:    string(NodePoolDeployedReasonError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonError, "reason") },
		},
		"Deployed/TerminalError": {
			condition: NodePoolDeployed,
			reason:    string(NodePoolDeployedReasonTerminalError),
			expected:  metav1.ConditionFalse,
			setFn:     func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonTerminalError, "reason") },
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewNodePool()

			assertNoCondition(t, tt.condition, status.getConditions(0))
			tt.setFn(status)
			assertConditionStatusReason(t, tt.condition, tt.expected, tt.reason, status.getConditions(0))
		})
	}

	// final conditions tests
	for name, conditionReason := range map[string]struct {
		condition   string
		trueReason  string
		falseReason string
	}{
		"Quiesced": {
			condition:   NodePoolQuiesced,
			trueReason:  string(NodePoolQuiescedReasonQuiesced),
			falseReason: string(NodePoolQuiescedReasonStillReconciling),
		},
	} {
		conditionReason := conditionReason
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewNodePool()

			// attempt to set all conditions one by one until they are all set
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetBound(NodePoolBoundReasonBound, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.getConditions(0))

			status.SetDeployed(NodePoolDeployedReasonDeployed, "reason")
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionTrue, conditionReason.trueReason, status.getConditions(0))
		})
	}

	// transient error tests
	for name, tt := range map[string]struct {
		setTransientErrFn   setNodePoolFunc
		setConditionReasons []setNodePoolFunc
	}{
		"Transient Error: Error, Condition: Bound": {
			setTransientErrFn: func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonError, "reason") },
			setConditionReasons: []setNodePoolFunc{
				func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonDeployed, "reason") },
			},
		},
		"Transient Error: Error, Condition: Deployed": {
			setTransientErrFn: func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonError, "reason") },
			setConditionReasons: []setNodePoolFunc{
				func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonBound, "reason") },
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewNodePool()

			assertConditionStatusReason(t, NodePoolQuiesced, metav1.ConditionFalse, string(NodePoolQuiescedReasonStillReconciling), status.getConditions(0))

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			assertConditionStatusReason(t, NodePoolQuiesced, metav1.ConditionFalse, string(NodePoolQuiescedReasonStillReconciling), status.getConditions(0))
		})
	}

	// terminal error tests
	for name, setFn := range map[string]setNodePoolFunc{
		"Terminal Error: TerminalError, Condition: Bound":    func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonTerminalError, "reason") },
		"Terminal Error: TerminalError, Condition: Deployed": func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonTerminalError, "reason") },
	} {
		setFn := setFn
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewNodePool()

			assertConditionStatusReason(t, NodePoolQuiesced, metav1.ConditionFalse, string(NodePoolQuiescedReasonStillReconciling), status.getConditions(0))

			setFn(status)

			assertConditionStatusReason(t, NodePoolQuiesced, metav1.ConditionTrue, string(NodePoolQuiescedReasonQuiesced), status.getConditions(0))
		})
	}

	// rollup conditions tests
	for name, tt := range map[string]struct {
		condition      string
		trueReason     string
		falseReason    string
		falseCondition setNodePoolFunc
		trueConditions []setNodePoolFunc
	}{
		"Rollup Conditions: Stable, All True": {
			condition:   NodePoolStable,
			trueReason:  string(NodePoolStableReasonStable),
			falseReason: string(NodePoolStableReasonUnstable),
			trueConditions: []setNodePoolFunc{
				func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonBound, "reason") },
				func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonDeployed, "reason") },
			},
		},
		"Rollup Conditions: Stable, False Condition: Bound": {
			condition:      NodePoolStable,
			trueReason:     string(NodePoolStableReasonStable),
			falseReason:    string(NodePoolStableReasonUnstable),
			falseCondition: func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonTerminalError, "reason") },
			trueConditions: []setNodePoolFunc{
				func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonDeployed, "reason") },
			},
		},
		"Rollup Conditions: Stable, False Condition: Deployed": {
			condition:      NodePoolStable,
			trueReason:     string(NodePoolStableReasonStable),
			falseReason:    string(NodePoolStableReasonUnstable),
			falseCondition: func(status *NodePoolStatus) { status.SetDeployed(NodePoolDeployedReasonTerminalError, "reason") },
			trueConditions: []setNodePoolFunc{
				func(status *NodePoolStatus) { status.SetBound(NodePoolBoundReasonBound, "reason") },
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := NewNodePool()

			assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))

			if tt.falseCondition != nil {
				tt.falseCondition(status)
			}
			for _, setFn := range tt.trueConditions {
				setFn(status)
			}

			if tt.falseCondition != nil {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.getConditions(0))
			} else {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionTrue, tt.trueReason, status.getConditions(0))
			}
		})
	}
}
