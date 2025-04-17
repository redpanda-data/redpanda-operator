// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package statuses

// GENERATED from ./statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterStatus - Defines the observed status conditions of a cluster.
type ClusterStatus struct {
	// ClusterReadyStatus - This condition indicates whether a cluster is ready to
	// serve any traffic. This can happen, for example if a cluster is partially
	// degraded but still can process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	Ready *ClusterReadyStatus
	// ClusterHealthyStatus - This condition indicates whether a cluster is fully
	// healthy.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	Healthy *ClusterHealthyStatus
	// ClusterLicenseValidStatus - This condition indicates whether a cluster has a
	// valid license.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	LicenseValid *ClusterLicenseValidStatus
	// ClusterClusterResourcesSyncedStatus - This condition indicates whether
	// cluster configuration parameters have currently been applied to a cluster for
	// the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterResourcesSynced *ClusterClusterResourcesSyncedStatus
	// ClusterClusterConfigurationAppliedStatus - This condition indicates whether
	// cluster configuration parameters have currently been applied to a cluster for
	// the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterConfigurationApplied *ClusterClusterConfigurationAppliedStatus
	// ClusterQuiescedStatus - This condition is used as to indicate that the
	// cluster is no longer reconciling due to it being in a finalized state for the
	// current generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	Quiesced *ClusterQuiescedStatus
	// ClusterStableStatus - This condition is used as a roll-up status for any sort
	// of automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	Stable *ClusterStableStatus
}

// NewCluster() returns a new ClusterStatus
func NewCluster() *ClusterStatus {
	return &ClusterStatus{
		&ClusterReadyStatus{},
		&ClusterHealthyStatus{},
		&ClusterLicenseValidStatus{},
		&ClusterClusterResourcesSyncedStatus{},
		&ClusterClusterConfigurationAppliedStatus{},
		&ClusterQuiescedStatus{},
		&ClusterStableStatus{},
	}
}

// Conditions returns the aggregated status conditions of the ClusterStatus.
func (s *ClusterStatus) Conditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		s.Ready.Condition(generation),
		s.Healthy.Condition(generation),
		s.LicenseValid.Condition(generation),
		s.ClusterResourcesSynced.Condition(generation),
		s.ClusterConfigurationApplied.Condition(generation),
		s.Quiesced.Condition(generation),
		s.Stable.Condition(generation),
	}
}

// ClusterReadyStatus - This condition indicates whether a cluster is ready to
// serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterReadyStatus struct {
	// ClusterReadyConditionReasonNotReady - This reason is used with the "Ready"
	// condition when a cluster is not ready.
	NotReady error
}

const (
	// ClusterReadyCondition - This condition indicates whether a cluster is ready
	// to serve any traffic. This can happen, for example if a cluster is partially
	// degraded but still can process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterReadyCondition = "Ready"
	// ClusterReadyConditionReasonReady - This reason is used with the "Ready"
	// condition when the condition is True.
	ClusterReadyConditionReasonReady = "Ready"
	// ClusterReadyConditionReasonNotReady - This reason is used with the "Ready"
	// condition when a cluster is not ready.
	ClusterReadyConditionReasonNotReady = "NotReady"
)

// Condition returns the status condition of the ClusterReadyStatus based off of
// the underlying errors that are set.
func (s *ClusterReadyStatus) Condition(generation int64) metav1.Condition {
	if s.NotReady != nil {
		return metav1.Condition{
			Type:               ClusterReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterReadyConditionReasonNotReady,
			Message:            s.NotReady.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterReadyConditionReasonReady,
		Message:            "Cluster ready to service requests",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterReadyStatus value to JSON
func (s *ClusterReadyStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.NotReady != nil {
		data["NotReady"] = s.NotReady.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterReadyStatus from JSON
func (s *ClusterReadyStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["NotReady"]; ok {
		s.NotReady = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterReadyStatus) HasError() bool {
	return s.NotReady != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterReadyStatus) MaybeError() error {
	if s.NotReady != nil {
		return s.NotReady
	}

	return nil
}

// ClusterHealthyStatus - This condition indicates whether a cluster is fully
// healthy.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterHealthyStatus struct {
	// ClusterHealthyConditionReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	TerminalError error
	// ClusterHealthyConditionReasonError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired cluster state. It should only
	// be set on the conditions currently in scope for the current cluster state,
	// any subsequently derived conditions should use "StillReconciling".
	Error error
	// ClusterHealthyConditionReasonStillReconciling - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a blocking condition prior to applying the desired cluster state. It does not
	// necessarily indicate an underlying error occurred during reconciliation.
	StillReconciling error
	// ClusterHealthyConditionReasonNotHealthy - This reason is used with the
	// "Healthy" condition when a cluster is not healthy.
	NotHealthy error
}

const (
	// ClusterHealthyCondition - This condition indicates whether a cluster is fully
	// healthy.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterHealthyCondition = "Healthy"
	// ClusterHealthyConditionReasonHealthy - This reason is used with the "Healthy"
	// condition when the condition is True.
	ClusterHealthyConditionReasonHealthy = "Healthy"
	// ClusterHealthyConditionReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterHealthyConditionReasonTerminalError = "TerminalError"
	// ClusterHealthyConditionReasonError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired cluster state. It should only
	// be set on the conditions currently in scope for the current cluster state,
	// any subsequently derived conditions should use "StillReconciling".
	ClusterHealthyConditionReasonError = "Error"
	// ClusterHealthyConditionReasonStillReconciling - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a blocking condition prior to applying the desired cluster state. It does not
	// necessarily indicate an underlying error occurred during reconciliation.
	ClusterHealthyConditionReasonStillReconciling = "StillReconciling"
	// ClusterHealthyConditionReasonNotHealthy - This reason is used with the
	// "Healthy" condition when a cluster is not healthy.
	ClusterHealthyConditionReasonNotHealthy = "NotHealthy"
)

// Condition returns the status condition of the ClusterHealthyStatus based off
// of the underlying errors that are set.
func (s *ClusterHealthyStatus) Condition(generation int64) metav1.Condition {
	if s.TerminalError != nil {
		return metav1.Condition{
			Type:               ClusterHealthyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterHealthyConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.Error != nil {
		return metav1.Condition{
			Type:               ClusterHealthyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterHealthyConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.StillReconciling != nil {
		return metav1.Condition{
			Type:               ClusterHealthyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterHealthyConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.NotHealthy != nil {
		return metav1.Condition{
			Type:               ClusterHealthyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterHealthyConditionReasonNotHealthy,
			Message:            s.NotHealthy.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterHealthyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterHealthyConditionReasonHealthy,
		Message:            "Cluster is healthy",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterHealthyStatus value to JSON
func (s *ClusterHealthyStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}
	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}
	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}
	if s.NotHealthy != nil {
		data["NotHealthy"] = s.NotHealthy.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterHealthyStatus from JSON
func (s *ClusterHealthyStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}
	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}
	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}
	if err, ok := data["NotHealthy"]; ok {
		s.NotHealthy = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterHealthyStatus) HasError() bool {
	return s.TerminalError != nil || s.Error != nil || s.StillReconciling != nil || s.NotHealthy != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterHealthyStatus) MaybeError() error {
	if s.TerminalError != nil {
		return s.TerminalError
	}
	if s.Error != nil {
		return s.Error
	}
	if s.StillReconciling != nil {
		return s.StillReconciling
	}
	if s.NotHealthy != nil {
		return s.NotHealthy
	}

	return nil
}

// ClusterLicenseValidStatus - This condition indicates whether a cluster has a
// valid license.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterLicenseValidStatus struct {
	// ClusterLicenseValidConditionReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	TerminalError error
	// ClusterLicenseValidConditionReasonError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a
	// retryable error occurring prior to applying the desired cluster state. It
	// should only be set on the conditions currently in scope for the current
	// cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	Error error
	// ClusterLicenseValidConditionReasonStillReconciling - This reason is used when
	// a cluster has only been partially reconciled and we have early returned due
	// to a blocking condition prior to applying the desired cluster state. It does
	// not necessarily indicate an underlying error occurred during reconciliation.
	StillReconciling error
	// ClusterLicenseValidConditionReasonLicenseExpired - This reason is used with
	// the "LicenseValid" condition when a cluster has an expired license.
	LicenseExpired error
	// ClusterLicenseValidConditionReasonLicenseNotPresent - This reason is used
	// with the "LicenseValid" condition when a cluster has no license.
	LicenseNotPresent error
}

const (
	// ClusterLicenseValidCondition - This condition indicates whether a cluster has
	// a valid license.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterLicenseValidCondition = "LicenseValid"
	// ClusterLicenseValidConditionReasonLicenseValid - This reason is used with the
	// "LicenseValid" condition when the condition is True.
	ClusterLicenseValidConditionReasonLicenseValid = "LicenseValid"
	// ClusterLicenseValidConditionReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterLicenseValidConditionReasonTerminalError = "TerminalError"
	// ClusterLicenseValidConditionReasonError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a
	// retryable error occurring prior to applying the desired cluster state. It
	// should only be set on the conditions currently in scope for the current
	// cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	ClusterLicenseValidConditionReasonError = "Error"
	// ClusterLicenseValidConditionReasonStillReconciling - This reason is used when
	// a cluster has only been partially reconciled and we have early returned due
	// to a blocking condition prior to applying the desired cluster state. It does
	// not necessarily indicate an underlying error occurred during reconciliation.
	ClusterLicenseValidConditionReasonStillReconciling = "StillReconciling"
	// ClusterLicenseValidConditionReasonLicenseExpired - This reason is used with
	// the "LicenseValid" condition when a cluster has an expired license.
	ClusterLicenseValidConditionReasonLicenseExpired = "LicenseExpired"
	// ClusterLicenseValidConditionReasonLicenseNotPresent - This reason is used
	// with the "LicenseValid" condition when a cluster has no license.
	ClusterLicenseValidConditionReasonLicenseNotPresent = "LicenseNotPresent"
)

// Condition returns the status condition of the ClusterLicenseValidStatus based
// off of the underlying errors that are set.
func (s *ClusterLicenseValidStatus) Condition(generation int64) metav1.Condition {
	if s.TerminalError != nil {
		return metav1.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.Error != nil {
		return metav1.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.StillReconciling != nil {
		return metav1.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.LicenseExpired != nil {
		return metav1.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonLicenseExpired,
			Message:            s.LicenseExpired.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.LicenseNotPresent != nil {
		return metav1.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonLicenseNotPresent,
			Message:            s.LicenseNotPresent.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterLicenseValidCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterLicenseValidConditionReasonLicenseValid,
		Message:            "Cluster has a valid license",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterLicenseValidStatus value to JSON
func (s *ClusterLicenseValidStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}
	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}
	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}
	if s.LicenseExpired != nil {
		data["LicenseExpired"] = s.LicenseExpired.Error()
	}
	if s.LicenseNotPresent != nil {
		data["LicenseNotPresent"] = s.LicenseNotPresent.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterLicenseValidStatus from JSON
func (s *ClusterLicenseValidStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}
	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}
	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}
	if err, ok := data["LicenseExpired"]; ok {
		s.LicenseExpired = errors.New(err)
	}
	if err, ok := data["LicenseNotPresent"]; ok {
		s.LicenseNotPresent = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterLicenseValidStatus) HasError() bool {
	return s.TerminalError != nil || s.Error != nil || s.StillReconciling != nil || s.LicenseExpired != nil || s.LicenseNotPresent != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterLicenseValidStatus) MaybeError() error {
	if s.TerminalError != nil {
		return s.TerminalError
	}
	if s.Error != nil {
		return s.Error
	}
	if s.StillReconciling != nil {
		return s.StillReconciling
	}
	if s.LicenseExpired != nil {
		return s.LicenseExpired
	}
	if s.LicenseNotPresent != nil {
		return s.LicenseNotPresent
	}

	return nil
}

// ClusterClusterResourcesSyncedStatus - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterClusterResourcesSyncedStatus struct {
	// ClusterClusterResourcesSyncedConditionReasonTerminalError - This reason is
	// used when a cluster has only been partially reconciled and we have early
	// returned due to a known terminal error occurring prior to applying the
	// desired cluster state. Any conditions not already derived should also receive
	// the "TerminalError" reason. The cluster should also no longer be reconciled
	// until it or an underlying resource is changed.
	TerminalError error
	// ClusterClusterResourcesSyncedConditionReasonError - This reason is used when
	// a cluster has only been partially reconciled and we have early returned due
	// to a retryable error occurring prior to applying the desired cluster state.
	// It should only be set on the conditions currently in scope for the current
	// cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	Error error
	// ClusterClusterResourcesSyncedConditionReasonStillReconciling - This reason is
	// used when a cluster has only been partially reconciled and we have early
	// returned due to a blocking condition prior to applying the desired cluster
	// state. It does not necessarily indicate an underlying error occurred during
	// reconciliation.
	StillReconciling error
}

const (
	// ClusterClusterResourcesSyncedCondition - This condition indicates whether
	// cluster configuration parameters have currently been applied to a cluster for
	// the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterClusterResourcesSyncedCondition = "ClusterResourcesSynced"
	// ClusterClusterResourcesSyncedConditionReasonSynced - This reason is used with
	// the "ClusterConfigurationApplied" condition when the condition is True.
	ClusterClusterResourcesSyncedConditionReasonSynced = "Synced"
	// ClusterClusterResourcesSyncedConditionReasonTerminalError - This reason is
	// used when a cluster has only been partially reconciled and we have early
	// returned due to a known terminal error occurring prior to applying the
	// desired cluster state. Any conditions not already derived should also receive
	// the "TerminalError" reason. The cluster should also no longer be reconciled
	// until it or an underlying resource is changed.
	ClusterClusterResourcesSyncedConditionReasonTerminalError = "TerminalError"
	// ClusterClusterResourcesSyncedConditionReasonError - This reason is used when
	// a cluster has only been partially reconciled and we have early returned due
	// to a retryable error occurring prior to applying the desired cluster state.
	// It should only be set on the conditions currently in scope for the current
	// cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	ClusterClusterResourcesSyncedConditionReasonError = "Error"
	// ClusterClusterResourcesSyncedConditionReasonStillReconciling - This reason is
	// used when a cluster has only been partially reconciled and we have early
	// returned due to a blocking condition prior to applying the desired cluster
	// state. It does not necessarily indicate an underlying error occurred during
	// reconciliation.
	ClusterClusterResourcesSyncedConditionReasonStillReconciling = "StillReconciling"
)

// Condition returns the status condition of the
// ClusterClusterResourcesSyncedStatus based off of the underlying errors that
// are set.
func (s *ClusterClusterResourcesSyncedStatus) Condition(generation int64) metav1.Condition {
	if s.TerminalError != nil {
		return metav1.Condition{
			Type:               ClusterClusterResourcesSyncedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterResourcesSyncedConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.Error != nil {
		return metav1.Condition{
			Type:               ClusterClusterResourcesSyncedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterResourcesSyncedConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.StillReconciling != nil {
		return metav1.Condition{
			Type:               ClusterClusterResourcesSyncedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterResourcesSyncedConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterClusterResourcesSyncedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterClusterResourcesSyncedConditionReasonSynced,
		Message:            "Cluster configuration successfully applied",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterClusterResourcesSyncedStatus value to JSON
func (s *ClusterClusterResourcesSyncedStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}
	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}
	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterClusterResourcesSyncedStatus from JSON
func (s *ClusterClusterResourcesSyncedStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}
	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}
	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterClusterResourcesSyncedStatus) HasError() bool {
	return s.TerminalError != nil || s.Error != nil || s.StillReconciling != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterClusterResourcesSyncedStatus) MaybeError() error {
	if s.TerminalError != nil {
		return s.TerminalError
	}
	if s.Error != nil {
		return s.Error
	}
	if s.StillReconciling != nil {
		return s.StillReconciling
	}

	return nil
}

// ClusterClusterConfigurationAppliedStatus - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterClusterConfigurationAppliedStatus struct {
	// ClusterClusterConfigurationAppliedConditionReasonTerminalError - This reason
	// is used when a cluster has only been partially reconciled and we have early
	// returned due to a known terminal error occurring prior to applying the
	// desired cluster state. Any conditions not already derived should also receive
	// the "TerminalError" reason. The cluster should also no longer be reconciled
	// until it or an underlying resource is changed.
	TerminalError error
	// ClusterClusterConfigurationAppliedConditionReasonError - This reason is used
	// when a cluster has only been partially reconciled and we have early returned
	// due to a retryable error occurring prior to applying the desired cluster
	// state. It should only be set on the conditions currently in scope for the
	// current cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	Error error
	// ClusterClusterConfigurationAppliedConditionReasonStillReconciling - This
	// reason is used when a cluster has only been partially reconciled and we have
	// early returned due to a blocking condition prior to applying the desired
	// cluster state. It does not necessarily indicate an underlying error occurred
	// during reconciliation.
	StillReconciling error
}

const (
	// ClusterClusterConfigurationAppliedCondition - This condition indicates
	// whether cluster configuration parameters have currently been applied to a
	// cluster for the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterClusterConfigurationAppliedCondition = "ClusterConfigurationApplied"
	// ClusterClusterConfigurationAppliedConditionReasonApplied - This reason is
	// used with the "ClusterConfigurationApplied" condition when the condition is
	// True.
	ClusterClusterConfigurationAppliedConditionReasonApplied = "Applied"
	// ClusterClusterConfigurationAppliedConditionReasonTerminalError - This reason
	// is used when a cluster has only been partially reconciled and we have early
	// returned due to a known terminal error occurring prior to applying the
	// desired cluster state. Any conditions not already derived should also receive
	// the "TerminalError" reason. The cluster should also no longer be reconciled
	// until it or an underlying resource is changed.
	ClusterClusterConfigurationAppliedConditionReasonTerminalError = "TerminalError"
	// ClusterClusterConfigurationAppliedConditionReasonError - This reason is used
	// when a cluster has only been partially reconciled and we have early returned
	// due to a retryable error occurring prior to applying the desired cluster
	// state. It should only be set on the conditions currently in scope for the
	// current cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	ClusterClusterConfigurationAppliedConditionReasonError = "Error"
	// ClusterClusterConfigurationAppliedConditionReasonStillReconciling - This
	// reason is used when a cluster has only been partially reconciled and we have
	// early returned due to a blocking condition prior to applying the desired
	// cluster state. It does not necessarily indicate an underlying error occurred
	// during reconciliation.
	ClusterClusterConfigurationAppliedConditionReasonStillReconciling = "StillReconciling"
)

// Condition returns the status condition of the
// ClusterClusterConfigurationAppliedStatus based off of the underlying errors
// that are set.
func (s *ClusterClusterConfigurationAppliedStatus) Condition(generation int64) metav1.Condition {
	if s.TerminalError != nil {
		return metav1.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.Error != nil {
		return metav1.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	if s.StillReconciling != nil {
		return metav1.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterClusterConfigurationAppliedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterClusterConfigurationAppliedConditionReasonApplied,
		Message:            "Cluster configuration successfully applied",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterClusterConfigurationAppliedStatus value to JSON
func (s *ClusterClusterConfigurationAppliedStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}
	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}
	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterClusterConfigurationAppliedStatus from JSON
func (s *ClusterClusterConfigurationAppliedStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}
	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}
	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterClusterConfigurationAppliedStatus) HasError() bool {
	return s.TerminalError != nil || s.Error != nil || s.StillReconciling != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterClusterConfigurationAppliedStatus) MaybeError() error {
	if s.TerminalError != nil {
		return s.TerminalError
	}
	if s.Error != nil {
		return s.Error
	}
	if s.StillReconciling != nil {
		return s.StillReconciling
	}

	return nil
}

// ClusterQuiescedStatus - This condition is used as to indicate that the
// cluster is no longer reconciling due to it being in a finalized state for the
// current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterQuiescedStatus struct {
	// ClusterQuiescedConditionReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when a cluster has only been partially reconciled and we
	// have not fully completed reconciliation. This happens when, for example,
	// we're doing a cluster scaling operation.
	StillReconciling error
}

const (
	// ClusterQuiescedCondition - This condition is used as to indicate that the
	// cluster is no longer reconciling due to it being in a finalized state for the
	// current generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterQuiescedCondition = "Quiesced"
	// ClusterQuiescedConditionReasonQuiesced - This reason is used with the
	// "Quiesced" condition when the condition is True.
	ClusterQuiescedConditionReasonQuiesced = "Quiesced"
	// ClusterQuiescedConditionReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when a cluster has only been partially reconciled and we
	// have not fully completed reconciliation. This happens when, for example,
	// we're doing a cluster scaling operation.
	ClusterQuiescedConditionReasonStillReconciling = "StillReconciling"
)

// Condition returns the status condition of the ClusterQuiescedStatus based off
// of the underlying errors that are set.
func (s *ClusterQuiescedStatus) Condition(generation int64) metav1.Condition {
	if s.StillReconciling != nil {
		return metav1.Condition{
			Type:               ClusterQuiescedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterQuiescedConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterQuiescedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterQuiescedConditionReasonQuiesced,
		Message:            "Cluster reconciliation finished",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterQuiescedStatus value to JSON
func (s *ClusterQuiescedStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterQuiescedStatus from JSON
func (s *ClusterQuiescedStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterQuiescedStatus) HasError() bool {
	return s.StillReconciling != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterQuiescedStatus) MaybeError() error {
	if s.StillReconciling != nil {
		return s.StillReconciling
	}

	return nil
}

// ClusterStableStatus - This condition is used as a roll-up status for any sort
// of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterStableStatus struct {
	// ClusterStableConditionReasonUnstable - This reason is used with the "Stable"
	// condition when a cluster has not yet stabilized for automation purposes.
	Unstable error
}

const (
	// ClusterStableCondition - This condition is used as a roll-up status for any
	// sort of automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterStableCondition = "Stable"
	// ClusterStableConditionReasonStable - This reason is used with the "Stable"
	// condition when the condition is True.
	ClusterStableConditionReasonStable = "Stable"
	// ClusterStableConditionReasonUnstable - This reason is used with the "Stable"
	// condition when a cluster has not yet stabilized for automation purposes.
	ClusterStableConditionReasonUnstable = "Unstable"
)

// Condition returns the status condition of the ClusterStableStatus based off
// of the underlying errors that are set.
func (s *ClusterStableStatus) Condition(generation int64) metav1.Condition {
	if s.Unstable != nil {
		return metav1.Condition{
			Type:               ClusterStableCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ClusterStableConditionReasonUnstable,
			Message:            s.Unstable.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               ClusterStableCondition,
		Status:             metav1.ConditionTrue,
		Reason:             ClusterStableConditionReasonStable,
		Message:            "Cluster Stable",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a ClusterStableStatus value to JSON
func (s *ClusterStableStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	if s.Unstable != nil {
		data["Unstable"] = s.Unstable.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterStableStatus from JSON
func (s *ClusterStableStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if err, ok := data["Unstable"]; ok {
		s.Unstable = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the underlying errors for the given condition are set.
func (s *ClusterStableStatus) HasError() bool {
	return s.Unstable != nil
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *ClusterStableStatus) MaybeError() error {
	if s.Unstable != nil {
		return s.Unstable
	}

	return nil
}
