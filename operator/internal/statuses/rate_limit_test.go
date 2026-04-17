package statuses

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestSetStatusCondition_RateZero_NoChangeWhenIdentical covers the regression from
// https://github.com/redpanda-data/redpanda-operator/issues/1455: conditions with a
// zero rate (the default) must not be forced-dirty on every reconcile. If they are,
// a healthy cluster writes its status back to the API server on every pass and
// triggers a hot reconcile loop.
func TestSetStatusCondition_RateZero_NoChangeWhenIdentical(t *testing.T) {
	existing := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "all good",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
	}
	conditions := []metav1.Condition{existing}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "all good",
			ObservedGeneration: 1,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.False(t, changed, "identical condition with rate=0 must not mark the status dirty")
	assert.Equal(t, existing.LastTransitionTime, conditions[0].LastTransitionTime, "LastTransitionTime must be preserved when nothing changes")
}

func TestSetStatusCondition_RateZero_ChangedOnStatusUpdate(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-time.Hour))
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "NotReady",
		Message:            "waiting",
		ObservedGeneration: 1,
		LastTransitionTime: oldTime,
	}}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "all good",
			ObservedGeneration: 1,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "real status change must be detected regardless of rate")
	assert.Equal(t, metav1.ConditionTrue, conditions[0].Status)
	assert.True(t, conditions[0].LastTransitionTime.After(oldTime.Time), "LastTransitionTime must advance on real change")
}

func TestSetStatusCondition_RateZero_ChangedOnReasonUpdate(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-time.Hour))
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "all good",
		ObservedGeneration: 1,
		LastTransitionTime: oldTime,
	}}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "DifferentReason",
			Message:            "all good",
			ObservedGeneration: 1,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "reason change must be detected regardless of rate")
}

func TestSetStatusCondition_RateZero_ChangedOnMessageUpdate(t *testing.T) {
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "initial",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
	}}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "updated",
			ObservedGeneration: 1,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "message change must be detected regardless of rate")
}

func TestSetStatusCondition_RateZero_ChangedOnGenerationUpdate(t *testing.T) {
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "all good",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
	}}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "all good",
			ObservedGeneration: 2,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "observed generation change must be detected regardless of rate")
}

func TestSetStatusCondition_RateLimited_NoHeartbeatBeforeElapsed(t *testing.T) {
	existing := metav1.Condition{
		Type:               ClusterLicenseValid,
		Status:             metav1.ConditionTrue,
		Reason:             string(ClusterLicenseValidReasonValid),
		Message:            "valid",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Second)),
	}
	conditions := []metav1.Condition{existing}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               ClusterLicenseValid,
			Status:             metav1.ConditionTrue,
			Reason:             string(ClusterLicenseValidReasonValid),
			Message:            "valid",
			ObservedGeneration: 1,
		},
		rate: time.Minute,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.False(t, changed, "identical rate-limited condition within the rate window must not be marked dirty")
	assert.Equal(t, existing.LastTransitionTime, conditions[0].LastTransitionTime)
}

func TestSetStatusCondition_RateLimited_HeartbeatAfterElapsed(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	conditions := []metav1.Condition{{
		Type:               ClusterLicenseValid,
		Status:             metav1.ConditionTrue,
		Reason:             string(ClusterLicenseValidReasonValid),
		Message:            "valid",
		ObservedGeneration: 1,
		LastTransitionTime: oldTime,
	}}

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               ClusterLicenseValid,
			Status:             metav1.ConditionTrue,
			Reason:             string(ClusterLicenseValidReasonValid),
			Message:            "valid",
			ObservedGeneration: 1,
		},
		rate: time.Minute,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "rate-limited heartbeat must fire once the rate window has elapsed")
	assert.True(t, conditions[0].LastTransitionTime.After(oldTime.Time), "LastTransitionTime must advance on heartbeat")
}

func TestSetStatusCondition_AppendsWhenExistingIsNil(t *testing.T) {
	var conditions []metav1.Condition

	incoming := ratelimitedCondition{
		condition: metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "all good",
			ObservedGeneration: 1,
		},
		rate: 0,
	}

	changed := setStatusCondition(&conditions, incoming)
	assert.True(t, changed, "appending a new condition must return changed")
	require.Len(t, conditions, 1)
	assert.False(t, conditions[0].LastTransitionTime.IsZero(), "LastTransitionTime must be populated")
}

// TestUpdateConditions_IdempotentOnHealthyCluster reproduces the end-to-end
// behavior from issue #1455: on a healthy cluster nothing is actually changing,
// so calling UpdateConditions twice with the same inputs should return false the
// second time. Before the fix this returned true every pass because rate=0 was
// always treated as "force an update."
func TestUpdateConditions_IdempotentOnHealthyCluster(t *testing.T) {
	obj := &redpandav1alpha2.Redpanda{}
	obj.Generation = 1

	buildStatus := func() *ClusterStatus {
		s := NewCluster()
		s.SetReady(ClusterReadyReasonReady, "ready")
		s.SetHealthy(ClusterHealthyReasonHealthy, "healthy")
		s.SetLicenseValid(ClusterLicenseValidReasonValid, "valid")
		s.SetResourcesSynced(ClusterResourcesSyncedReasonSynced, "synced")
		s.SetConfigurationApplied(ClusterConfigurationAppliedReasonApplied, "applied")
		return s
	}

	// First pass: all conditions are new, so changes must be reported.
	changed := buildStatus().UpdateConditions(obj)
	require.True(t, changed, "first UpdateConditions call should populate conditions")
	require.NotEmpty(t, obj.Status.Conditions)

	// Second pass: identical inputs against the populated object. Only
	// rate-limited conditions should potentially flip, and only after their rate
	// window elapses — which has not happened here. Nothing else should change.
	changed = buildStatus().UpdateConditions(obj)
	assert.False(t, changed, "idempotent UpdateConditions call on healthy cluster must not mark status dirty")
}
