// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

// GENERATED from statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterReadyStatus(t *testing.T) {
	t.Parallel()

	var status ClusterReadyStatus

	expected := errors.New("expected")

	status = ClusterReadyStatus{}
	assert.Equal(t, "Cluster ready to service requests", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonReady, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterReadyStatus{NotReady: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonNotReady, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterReadyStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterReadyStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterHealthyStatus(t *testing.T) {
	t.Parallel()

	var status ClusterHealthyStatus

	expected := errors.New("expected")

	status = ClusterHealthyStatus{}
	assert.Equal(t, "Cluster is healthy", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonHealthy, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterHealthyStatus{NotHealthy: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonNotHealthy, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterHealthyStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterHealthyStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterClusterConfigurationAppliedStatus(t *testing.T) {
	t.Parallel()

	var status ClusterClusterConfigurationAppliedStatus

	expected := errors.New("expected")

	status = ClusterClusterConfigurationAppliedStatus{}
	assert.Equal(t, "Cluster configuration successfully applied", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonApplied, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterClusterConfigurationAppliedStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterClusterConfigurationAppliedStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterClusterConfigurationAppliedStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterLicenseValidStatus(t *testing.T) {
	t.Parallel()

	var status ClusterLicenseValidStatus

	expected := errors.New("expected")

	status = ClusterLicenseValidStatus{}
	assert.Equal(t, "Valid", status.Condition(0).Message)
	assert.Equal(t, ClusterLicenseValidConditionReasonLicenseValid, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterLicenseValidStatus{LicenseExpired: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterLicenseValidConditionReasonLicenseExpired, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterLicenseValidStatus{LicenseNotPresent: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterLicenseValidConditionReasonLicenseNotPresent, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterLicenseValidStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterLicenseValidConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ClusterLicenseValidStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterLicenseValidConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterQuiescedStatus(t *testing.T) {
	t.Parallel()

	var status ClusterQuiescedStatus

	expected := errors.New("expected")

	status = ClusterQuiescedStatus{}
	assert.Equal(t, "Cluster reconciliation finished", status.Condition(0).Message)
	assert.Equal(t, ClusterQuiescedConditionReasonQuiesced, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterQuiescedStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterQuiescedConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterStableStatus(t *testing.T) {
	t.Parallel()

	var status ClusterStableStatus

	expected := errors.New("expected")

	status = ClusterStableStatus{}
	assert.Equal(t, "Cluster Stable", status.Condition(0).Message)
	assert.Equal(t, ClusterStableConditionReasonStable, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ClusterStableStatus{Unstable: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterStableConditionReasonUnstable, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestClusterStatus(t *testing.T) {
	t.Parallel()

	status := ClusterStatus{}
	conditions := status.Conditions(0)

	var conditionType string
	var reason string

	conditionType = ClusterReadyCondition
	reason = ClusterReadyConditionReasonReady
	assert.Equal(t, conditionType, conditions[0].Type)
	assert.Equal(t, reason, conditions[0].Reason)

	conditionType = ClusterHealthyCondition
	reason = ClusterHealthyConditionReasonHealthy
	assert.Equal(t, conditionType, conditions[1].Type)
	assert.Equal(t, reason, conditions[1].Reason)

	conditionType = ClusterClusterConfigurationAppliedCondition
	reason = ClusterClusterConfigurationAppliedConditionReasonApplied
	assert.Equal(t, conditionType, conditions[2].Type)
	assert.Equal(t, reason, conditions[2].Reason)

	conditionType = ClusterLicenseValidCondition
	reason = ClusterLicenseValidConditionReasonLicenseValid
	assert.Equal(t, conditionType, conditions[3].Type)
	assert.Equal(t, reason, conditions[3].Reason)

	conditionType = ClusterQuiescedCondition
	reason = ClusterQuiescedConditionReasonQuiesced
	assert.Equal(t, conditionType, conditions[4].Type)
	assert.Equal(t, reason, conditions[4].Reason)

	conditionType = ClusterStableCondition
	reason = ClusterStableConditionReasonStable
	assert.Equal(t, conditionType, conditions[5].Type)
	assert.Equal(t, reason, conditions[5].Reason)

}

func TestClusterReadyStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterReadyStatus{
		NotReady:      errors.New("NotReady"),
		Error:         errors.New("Error"),
		TerminalError: errors.New("TerminalError"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterReadyStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.NotReady.Error(), unmarshaled.NotReady.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
}

func TestClusterHealthyStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterHealthyStatus{
		NotHealthy:    errors.New("NotHealthy"),
		Error:         errors.New("Error"),
		TerminalError: errors.New("TerminalError"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterHealthyStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.NotHealthy.Error(), unmarshaled.NotHealthy.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
}

func TestClusterClusterConfigurationAppliedStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterClusterConfigurationAppliedStatus{
		StillReconciling: errors.New("StillReconciling"),
		Error:            errors.New("Error"),
		TerminalError:    errors.New("TerminalError"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterClusterConfigurationAppliedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
}

func TestClusterLicenseValidStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterLicenseValidStatus{
		LicenseExpired:    errors.New("LicenseExpired"),
		LicenseNotPresent: errors.New("LicenseNotPresent"),
		Error:             errors.New("Error"),
		TerminalError:     errors.New("TerminalError"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterLicenseValidStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.LicenseExpired.Error(), unmarshaled.LicenseExpired.Error())
	assert.Equal(t, status.LicenseNotPresent.Error(), unmarshaled.LicenseNotPresent.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
}

func TestClusterQuiescedStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterQuiescedStatus{
		StillReconciling: errors.New("StillReconciling"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterQuiescedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
}

func TestClusterStableStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ClusterStableStatus{
		Unstable: errors.New("Unstable"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ClusterStableStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.Unstable.Error(), unmarshaled.Unstable.Error())
}

func TestResourceSyncedStatus(t *testing.T) {
	t.Parallel()

	var status ResourceSyncedStatus

	expected := errors.New("expected")

	status = ResourceSyncedStatus{}
	assert.Equal(t, "Cluster resource synced", status.Condition(0).Message)
	assert.Equal(t, ResourceSyncedConditionReasonSynced, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = ResourceSyncedStatus{ClusterRefInvalid: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ResourceSyncedConditionReasonClusterRefInvalid, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ResourceSyncedStatus{ConfigurationInvalid: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ResourceSyncedConditionReasonConfigurationInvalid, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ResourceSyncedStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ResourceSyncedConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = ResourceSyncedStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ResourceSyncedConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

}

func TestResourceStatus(t *testing.T) {
	t.Parallel()

	status := ResourceStatus{}
	conditions := status.Conditions(0)

	var conditionType string
	var reason string

	conditionType = ResourceSyncedCondition
	reason = ResourceSyncedConditionReasonSynced
	assert.Equal(t, conditionType, conditions[0].Type)
	assert.Equal(t, reason, conditions[0].Reason)

}

func TestResourceSyncedStatusMarshaling(t *testing.T) {
	t.Parallel()

	status := ResourceSyncedStatus{
		ClusterRefInvalid:    errors.New("ClusterRefInvalid"),
		ConfigurationInvalid: errors.New("ConfigurationInvalid"),
		Error:                errors.New("Error"),
		TerminalError:        errors.New("TerminalError"),
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := ResourceSyncedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.ClusterRefInvalid.Error(), unmarshaled.ClusterRefInvalid.Error())
	assert.Equal(t, status.ConfigurationInvalid.Error(), unmarshaled.ConfigurationInvalid.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
}
