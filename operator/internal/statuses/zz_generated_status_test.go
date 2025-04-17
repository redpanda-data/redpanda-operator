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
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterReadyStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterReadyStatus{}
	assert.Equal(t, "Cluster ready to service requests", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonReady, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterReadyStatus{NotReady: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterReadyConditionReasonNotReady, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterHealthyStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterHealthyStatus{}
	assert.Equal(t, "Cluster is healthy", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonHealthy, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterHealthyStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterHealthyStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterHealthyStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterHealthyStatus{NotHealthy: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterHealthyConditionReasonNotHealthy, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterClusterResourcesSyncedStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterClusterResourcesSyncedStatus{}
	assert.Equal(t, "Cluster configuration successfully applied", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterResourcesSyncedConditionReasonSynced, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterClusterResourcesSyncedStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterResourcesSyncedConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterClusterResourcesSyncedStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterResourcesSyncedConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterClusterResourcesSyncedStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterResourcesSyncedConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterClusterConfigurationAppliedStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterClusterConfigurationAppliedStatus{}
	assert.Equal(t, "Cluster configuration successfully applied", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonApplied, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterClusterConfigurationAppliedStatus{TerminalError: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonTerminalError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterClusterConfigurationAppliedStatus{Error: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonError, status.Condition(0).Reason)
	assert.True(t, status.HasError())

	status = &ClusterClusterConfigurationAppliedStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterClusterConfigurationAppliedConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterQuiescedStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterQuiescedStatus{}
	assert.Equal(t, "Cluster reconciliation finished", status.Condition(0).Message)
	assert.Equal(t, ClusterQuiescedConditionReasonQuiesced, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterQuiescedStatus{StillReconciling: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterQuiescedConditionReasonStillReconciling, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterStableStatus(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &ClusterStableStatus{}
	assert.Equal(t, "Cluster Stable", status.Condition(0).Message)
	assert.Equal(t, ClusterStableConditionReasonStable, status.Condition(0).Reason)
	assert.False(t, status.HasError())

	status = &ClusterStableStatus{Unstable: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, ClusterStableConditionReasonUnstable, status.Condition(0).Reason)
	assert.True(t, status.HasError())
}

func TestClusterStatus(t *testing.T) {
	t.Parallel()
	status := NewCluster()
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

	conditionType = ClusterClusterResourcesSyncedCondition
	reason = ClusterClusterResourcesSyncedConditionReasonSynced
	assert.Equal(t, conditionType, conditions[2].Type)
	assert.Equal(t, reason, conditions[2].Reason)

	conditionType = ClusterClusterConfigurationAppliedCondition
	reason = ClusterClusterConfigurationAppliedConditionReasonApplied
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
	status := &ClusterReadyStatus{
		NotReady: errors.New("NotReady"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterReadyStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.NotReady.Error(), unmarshaled.NotReady.Error())
}

func TestClusterHealthyStatusMarshaling(t *testing.T) {
	t.Parallel()
	status := &ClusterHealthyStatus{
		TerminalError:    errors.New("TerminalError"),
		Error:            errors.New("Error"),
		StillReconciling: errors.New("StillReconciling"),
		NotHealthy:       errors.New("NotHealthy"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterHealthyStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
	assert.Equal(t, status.NotHealthy.Error(), unmarshaled.NotHealthy.Error())
}

func TestClusterClusterResourcesSyncedStatusMarshaling(t *testing.T) {
	t.Parallel()
	status := &ClusterClusterResourcesSyncedStatus{
		TerminalError:    errors.New("TerminalError"),
		Error:            errors.New("Error"),
		StillReconciling: errors.New("StillReconciling"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterClusterResourcesSyncedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
}

func TestClusterClusterConfigurationAppliedStatusMarshaling(t *testing.T) {
	t.Parallel()
	status := &ClusterClusterConfigurationAppliedStatus{
		TerminalError:    errors.New("TerminalError"),
		Error:            errors.New("Error"),
		StillReconciling: errors.New("StillReconciling"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterClusterConfigurationAppliedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.TerminalError.Error(), unmarshaled.TerminalError.Error())
	assert.Equal(t, status.Error.Error(), unmarshaled.Error.Error())
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
}

func TestClusterQuiescedStatusMarshaling(t *testing.T) {
	t.Parallel()
	status := &ClusterQuiescedStatus{
		StillReconciling: errors.New("StillReconciling"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterQuiescedStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.StillReconciling.Error(), unmarshaled.StillReconciling.Error())
}

func TestClusterStableStatusMarshaling(t *testing.T) {
	t.Parallel()
	status := &ClusterStableStatus{
		Unstable: errors.New("Unstable"),
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &ClusterStableStatus{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	assert.Equal(t, status.Unstable.Error(), unmarshaled.Unstable.Error())
}
