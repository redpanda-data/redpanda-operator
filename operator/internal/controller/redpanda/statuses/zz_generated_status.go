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

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterReadyStatus - This condition indicates whether a cluster is ready to
// serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterReadyStatus struct {
	// This reason is used with the "Ready" condition when a cluster is not ready.
	NotReady error
	// This reason is used with the "Ready" condition when an error that is
	// retryable was encountered when attempting to determine readiness status. If
	// this is set, then reconciliation should be retried.
	Error error
	// This reason is used with the "Ready" condition when an error that is
	// irrecoverable was encountered when attempting to determine readiness status.
	TerminalError error
}

const (
	// ClusterReadyCondition - This condition indicates whether a cluster is ready
	// to serve any traffic. This can happen, for example if a cluster is partially
	// degraded but still can process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterReadyCondition = "Ready"
	// ClusterConditionReasonReady - This reason is used with the "Ready" condition
	// when the condition is True.
	ClusterReadyConditionReasonReady = "Ready"
	// ClusterReadyConditionReasonNotReady - This reason is used with the "Ready"
	// condition when a cluster is not ready.
	ClusterReadyConditionReasonNotReady = "NotReady"
	// ClusterReadyConditionReasonError - This reason is used with the "Ready"
	// condition when an error that is retryable was encountered when attempting to
	// determine readiness status. If this is set, then reconciliation should be
	// retried.
	ClusterReadyConditionReasonError = "Error"
	// ClusterReadyConditionReasonTerminalError - This reason is used with the
	// "Ready" condition when an error that is irrecoverable was encountered when
	// attempting to determine readiness status.
	ClusterReadyConditionReasonTerminalError = "TerminalError"
)

// Condition returns the status condition of the ClusterReadyStatus based off of
// the underlying errors that are set.
func (s ClusterReadyStatus) Condition(generation int64) meta.Condition {
	if s.NotReady != nil {
		return meta.Condition{
			Type:               ClusterReadyCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterReadyConditionReasonNotReady,
			Message:            s.NotReady.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.Error != nil {
		return meta.Condition{
			Type:               ClusterReadyCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterReadyConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.TerminalError != nil {
		return meta.Condition{
			Type:               ClusterReadyCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterReadyConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterReadyCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterReadyConditionReasonReady,
		Message:            "Cluster ready to service requests",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterReadyStatus value to JSON
func (s ClusterReadyStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}

	if s.NotReady != nil {
		data["NotReady"] = s.NotReady.Error()
	}

	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}

	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
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

	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}

	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the ClusterReadyStatus errors are set.
func (s ClusterReadyStatus) HasError() bool {
	return s.NotReady != nil || s.Error != nil || s.TerminalError != nil
}

// ClusterHealthyStatus - This condition indicates whether a cluster is fully
// healthy.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterHealthyStatus struct {
	// This reason is used with the "Healthy" condition when a cluster is not
	// healthy.
	NotHealthy error
	// This reason is used with the "Healthy" condition when an error that is
	// retryable was encountered when attempting to determine health status. If this
	// is set, then reconciliation should be retried.
	Error error
	// This reason is used with the "Healthy" condition when an error that is
	// irrecoverable was encountered when attempting to determine health status.
	TerminalError error
}

const (
	// ClusterHealthyCondition - This condition indicates whether a cluster is fully
	// healthy.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterHealthyCondition = "Healthy"
	// ClusterConditionReasonHealthy - This reason is used with the "Healthy"
	// condition when the condition is True.
	ClusterHealthyConditionReasonHealthy = "Healthy"
	// ClusterHealthyConditionReasonNotHealthy - This reason is used with the
	// "Healthy" condition when a cluster is not healthy.
	ClusterHealthyConditionReasonNotHealthy = "NotHealthy"
	// ClusterHealthyConditionReasonError - This reason is used with the "Healthy"
	// condition when an error that is retryable was encountered when attempting to
	// determine health status. If this is set, then reconciliation should be
	// retried.
	ClusterHealthyConditionReasonError = "Error"
	// ClusterHealthyConditionReasonTerminalError - This reason is used with the
	// "Healthy" condition when an error that is irrecoverable was encountered when
	// attempting to determine health status.
	ClusterHealthyConditionReasonTerminalError = "TerminalError"
)

// Condition returns the status condition of the ClusterHealthyStatus based off
// of the underlying errors that are set.
func (s ClusterHealthyStatus) Condition(generation int64) meta.Condition {
	if s.NotHealthy != nil {
		return meta.Condition{
			Type:               ClusterHealthyCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterHealthyConditionReasonNotHealthy,
			Message:            s.NotHealthy.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.Error != nil {
		return meta.Condition{
			Type:               ClusterHealthyCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterHealthyConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.TerminalError != nil {
		return meta.Condition{
			Type:               ClusterHealthyCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterHealthyConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterHealthyCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterHealthyConditionReasonHealthy,
		Message:            "Cluster is healthy",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterHealthyStatus value to JSON
func (s ClusterHealthyStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}

	if s.NotHealthy != nil {
		data["NotHealthy"] = s.NotHealthy.Error()
	}

	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}

	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterHealthyStatus from JSON
func (s *ClusterHealthyStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	if err, ok := data["NotHealthy"]; ok {
		s.NotHealthy = errors.New(err)
	}

	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}

	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the ClusterHealthyStatus errors are set.
func (s ClusterHealthyStatus) HasError() bool {
	return s.NotHealthy != nil || s.Error != nil || s.TerminalError != nil
}

// ClusterClusterConfigurationAppliedStatus - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterClusterConfigurationAppliedStatus struct {
	// This reason is used with the "ClusterConfigurationApplied" condition when a
	// cluster has only been partially reconciled and we have early returned prior
	// to applying the desired cluster state. This happens when, for example, we're
	// doing a cluster scaling operation.
	StillReconciling error
	// This reason is used with the "ClusterConfigurationApplied" condition when an
	// error that is retryable was encountered when attempting to apply the cluster
	// configuration. If this is set, then reconciliation should be retried.
	Error error
	// This reason is used with the "ClusterConfigurationApplied" condition when an
	// error that is irrecoverable was encountered when attempting to apply the
	// cluster configuration and it cannot be applied.
	TerminalError error
}

const (
	// ClusterClusterConfigurationAppliedCondition - This condition indicates
	// whether cluster configuration parameters have currently been applied to a
	// cluster for the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterClusterConfigurationAppliedCondition = "ClusterConfigurationApplied"
	// ClusterConditionReasonApplied - This reason is used with the
	// "ClusterConfigurationApplied" condition when the condition is True.
	ClusterClusterConfigurationAppliedConditionReasonApplied = "Applied"
	// ClusterClusterConfigurationAppliedConditionReasonStillReconciling - This
	// reason is used with the "ClusterConfigurationApplied" condition when a
	// cluster has only been partially reconciled and we have early returned prior
	// to applying the desired cluster state. This happens when, for example, we're
	// doing a cluster scaling operation.
	ClusterClusterConfigurationAppliedConditionReasonStillReconciling = "StillReconciling"
	// ClusterClusterConfigurationAppliedConditionReasonError - This reason is used
	// with the "ClusterConfigurationApplied" condition when an error that is
	// retryable was encountered when attempting to apply the cluster configuration.
	// If this is set, then reconciliation should be retried.
	ClusterClusterConfigurationAppliedConditionReasonError = "Error"
	// ClusterClusterConfigurationAppliedConditionReasonTerminalError - This reason
	// is used with the "ClusterConfigurationApplied" condition when an error that
	// is irrecoverable was encountered when attempting to apply the cluster
	// configuration and it cannot be applied.
	ClusterClusterConfigurationAppliedConditionReasonTerminalError = "TerminalError"
)

// Condition returns the status condition of the
// ClusterClusterConfigurationAppliedStatus based off of the underlying errors
// that are set.
func (s ClusterClusterConfigurationAppliedStatus) Condition(generation int64) meta.Condition {
	if s.StillReconciling != nil {
		return meta.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.Error != nil {
		return meta.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.TerminalError != nil {
		return meta.Condition{
			Type:               ClusterClusterConfigurationAppliedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterClusterConfigurationAppliedConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterClusterConfigurationAppliedCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterClusterConfigurationAppliedConditionReasonApplied,
		Message:            "Cluster configuration successfully applied",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterClusterConfigurationAppliedStatus value to JSON
func (s ClusterClusterConfigurationAppliedStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}

	if s.StillReconciling != nil {
		data["StillReconciling"] = s.StillReconciling.Error()
	}

	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}

	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterClusterConfigurationAppliedStatus from JSON
func (s *ClusterClusterConfigurationAppliedStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	if err, ok := data["StillReconciling"]; ok {
		s.StillReconciling = errors.New(err)
	}

	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}

	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the ClusterClusterConfigurationAppliedStatus
// errors are set.
func (s ClusterClusterConfigurationAppliedStatus) HasError() bool {
	return s.StillReconciling != nil || s.Error != nil || s.TerminalError != nil
}

// ClusterLicenseValidStatus - This condition indicates whether a valid license
// exists in the cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterLicenseValidStatus struct {
	// This reason is used with the "LicenseValid" condition when a license is found
	// within the cluster but it is expired.
	LicenseExpired error
	// This reason is used with the "LicenseValid" condition when a license is not
	// found within the cluster.
	LicenseNotPresent error
	// This reason is used with the "LicenseValid" condition when an error that is
	// retryable was encountered when attempting to fetch the license status. If
	// this is set, then reconciliation should be retried.
	Error error
	// This reason is used with the "LicenseValid" condition when an error that is
	// irrecoverable was encountered when attempting to fetch the license status.
	TerminalError error
}

const (
	// ClusterLicenseValidCondition - This condition indicates whether a valid
	// license exists in the cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterLicenseValidCondition = "LicenseValid"
	// ClusterConditionReasonLicenseValid - This reason is used with the
	// "LicenseValid" condition when the condition is True.
	ClusterLicenseValidConditionReasonLicenseValid = "LicenseValid"
	// ClusterLicenseValidConditionReasonLicenseExpired - This reason is used with
	// the "LicenseValid" condition when a license is found within the cluster but
	// it is expired.
	ClusterLicenseValidConditionReasonLicenseExpired = "LicenseExpired"
	// ClusterLicenseValidConditionReasonLicenseNotPresent - This reason is used
	// with the "LicenseValid" condition when a license is not found within the
	// cluster.
	ClusterLicenseValidConditionReasonLicenseNotPresent = "LicenseNotPresent"
	// ClusterLicenseValidConditionReasonError - This reason is used with the
	// "LicenseValid" condition when an error that is retryable was encountered when
	// attempting to fetch the license status. If this is set, then reconciliation
	// should be retried.
	ClusterLicenseValidConditionReasonError = "Error"
	// ClusterLicenseValidConditionReasonTerminalError - This reason is used with
	// the "LicenseValid" condition when an error that is irrecoverable was
	// encountered when attempting to fetch the license status.
	ClusterLicenseValidConditionReasonTerminalError = "TerminalError"
)

// Condition returns the status condition of the ClusterLicenseValidStatus based
// off of the underlying errors that are set.
func (s ClusterLicenseValidStatus) Condition(generation int64) meta.Condition {
	if s.LicenseExpired != nil {
		return meta.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonLicenseExpired,
			Message:            s.LicenseExpired.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.LicenseNotPresent != nil {
		return meta.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterLicenseValidConditionReasonLicenseNotPresent,
			Message:            s.LicenseNotPresent.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.Error != nil {
		return meta.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterLicenseValidConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.TerminalError != nil {
		return meta.Condition{
			Type:               ClusterLicenseValidCondition,
			Status:             meta.ConditionUnknown,
			Reason:             ClusterLicenseValidConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterLicenseValidCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterLicenseValidConditionReasonLicenseValid,
		Message:            "Valid",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterLicenseValidStatus value to JSON
func (s ClusterLicenseValidStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}

	if s.LicenseExpired != nil {
		data["LicenseExpired"] = s.LicenseExpired.Error()
	}

	if s.LicenseNotPresent != nil {
		data["LicenseNotPresent"] = s.LicenseNotPresent.Error()
	}

	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}

	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ClusterLicenseValidStatus from JSON
func (s *ClusterLicenseValidStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	if err, ok := data["LicenseExpired"]; ok {
		s.LicenseExpired = errors.New(err)
	}

	if err, ok := data["LicenseNotPresent"]; ok {
		s.LicenseNotPresent = errors.New(err)
	}

	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}

	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the ClusterLicenseValidStatus errors are set.
func (s ClusterLicenseValidStatus) HasError() bool {
	return s.LicenseExpired != nil || s.LicenseNotPresent != nil || s.Error != nil || s.TerminalError != nil
}

// ClusterQuiescedStatus - This condition is used as to indicate that the
// cluster is no longer reconciling due to it being in a finalized state for the
// current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterQuiescedStatus struct {
	// This reason is used with the "Quiesced" condition when a cluster has only
	// been partially reconciled and we have not fully completed reconciliation.
	// This happens when, for example, we're doing a cluster scaling operation.
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
	// ClusterConditionReasonQuiesced - This reason is used with the "Quiesced"
	// condition when the condition is True.
	ClusterQuiescedConditionReasonQuiesced = "Quiesced"
	// ClusterQuiescedConditionReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when a cluster has only been partially reconciled and we
	// have not fully completed reconciliation. This happens when, for example,
	// we're doing a cluster scaling operation.
	ClusterQuiescedConditionReasonStillReconciling = "StillReconciling"
)

// Condition returns the status condition of the ClusterQuiescedStatus based off
// of the underlying errors that are set.
func (s ClusterQuiescedStatus) Condition(generation int64) meta.Condition {
	if s.StillReconciling != nil {
		return meta.Condition{
			Type:               ClusterQuiescedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterQuiescedConditionReasonStillReconciling,
			Message:            s.StillReconciling.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterQuiescedCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterQuiescedConditionReasonQuiesced,
		Message:            "Cluster reconciliation finished",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterQuiescedStatus value to JSON
func (s ClusterQuiescedStatus) MarshalJSON() ([]byte, error) {
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

// HasError returns whether any of the ClusterQuiescedStatus errors are set.
func (s ClusterQuiescedStatus) HasError() bool {
	return s.StillReconciling != nil
}

// ClusterStableStatus - This condition is used as a roll-up status for any sort
// of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterStableStatus struct {
	// This reason is used with the "Stable" condition when a cluster has not yet
	// stabilized for automation purposes.
	Unstable error
}

const (
	// ClusterStableCondition - This condition is used as a roll-up status for any
	// sort of automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterStableCondition = "Stable"
	// ClusterConditionReasonStable - This reason is used with the "Stable"
	// condition when the condition is True.
	ClusterStableConditionReasonStable = "Stable"
	// ClusterStableConditionReasonUnstable - This reason is used with the "Stable"
	// condition when a cluster has not yet stabilized for automation purposes.
	ClusterStableConditionReasonUnstable = "Unstable"
)

// Condition returns the status condition of the ClusterStableStatus based off
// of the underlying errors that are set.
func (s ClusterStableStatus) Condition(generation int64) meta.Condition {
	if s.Unstable != nil {
		return meta.Condition{
			Type:               ClusterStableCondition,
			Status:             meta.ConditionFalse,
			Reason:             ClusterStableConditionReasonUnstable,
			Message:            s.Unstable.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ClusterStableCondition,
		Status:             meta.ConditionTrue,
		Reason:             ClusterStableConditionReasonStable,
		Message:            "Cluster Stable",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ClusterStableStatus value to JSON
func (s ClusterStableStatus) MarshalJSON() ([]byte, error) {
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

// HasError returns whether any of the ClusterStableStatus errors are set.
func (s ClusterStableStatus) HasError() bool {
	return s.Unstable != nil
}

// ClusterStatus - Defines the observed status conditions of a cluster.
type ClusterStatus struct {
	// This condition indicates whether a cluster is ready to serve any traffic.
	// This can happen, for example if a cluster is partially degraded but still can
	// process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	Ready ClusterReadyStatus
	// This condition indicates whether a cluster is fully healthy.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	Healthy ClusterHealthyStatus
	// This condition indicates whether cluster configuration parameters have
	// currently been applied to a cluster for the given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterConfigurationApplied ClusterClusterConfigurationAppliedStatus
	// This condition indicates whether a valid license exists in the cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	LicenseValid ClusterLicenseValidStatus
	// This condition is used as to indicate that the cluster is no longer
	// reconciling due to it being in a finalized state for the current generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	Quiesced ClusterQuiescedStatus
	// This condition is used as a roll-up status for any sort of automation such as
	// terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	Stable ClusterStableStatus
}

// Conditions returns the aggregated status conditions of the ClusterStatus.
func (s ClusterStatus) Conditions(generation int64) []meta.Condition {
	return []meta.Condition{
		s.Ready.Condition(generation),
		s.Healthy.Condition(generation),
		s.ClusterConfigurationApplied.Condition(generation),
		s.LicenseValid.Condition(generation),
		s.Quiesced.Condition(generation),
		s.Stable.Condition(generation),
	}
}

// ResourceSyncedStatus - This condition indicates whether a cluster resource
// has been synced to its cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster
// resource.
type ResourceSyncedStatus struct {
	// This reason is used with the "Synced" condition when a clusterRef points to
	// an unknown cluster.
	ClusterRefInvalid error
	// This reason is used with the "Synced" condition when a static configuration
	// that points to a cluster is invalid.
	ConfigurationInvalid error
	// This reason is used with the "Synced" condition when an error that is
	// retryable was encountered when attempting to sync a resource to its cluster.
	// If this is set, then reconciliation should be retried.
	Error error
	// This reason is used with the "Synced" condition when an error that is
	// irrecoverable was encountered when attempting to sync a resource to its
	// cluster.
	TerminalError error
}

const (
	// ResourceSyncedCondition - This condition indicates whether a cluster resource
	// has been synced to its cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster
	// resource.
	ResourceSyncedCondition = "Synced"
	// ResourceConditionReasonSynced - This reason is used with the "Synced"
	// condition when the condition is True.
	ResourceSyncedConditionReasonSynced = "Synced"
	// ResourceSyncedConditionReasonClusterRefInvalid - This reason is used with the
	// "Synced" condition when a clusterRef points to an unknown cluster.
	ResourceSyncedConditionReasonClusterRefInvalid = "ClusterRefInvalid"
	// ResourceSyncedConditionReasonConfigurationInvalid - This reason is used with
	// the "Synced" condition when a static configuration that points to a cluster
	// is invalid.
	ResourceSyncedConditionReasonConfigurationInvalid = "ConfigurationInvalid"
	// ResourceSyncedConditionReasonError - This reason is used with the "Synced"
	// condition when an error that is retryable was encountered when attempting to
	// sync a resource to its cluster. If this is set, then reconciliation should be
	// retried.
	ResourceSyncedConditionReasonError = "Error"
	// ResourceSyncedConditionReasonTerminalError - This reason is used with the
	// "Synced" condition when an error that is irrecoverable was encountered when
	// attempting to sync a resource to its cluster.
	ResourceSyncedConditionReasonTerminalError = "TerminalError"
)

// Condition returns the status condition of the ResourceSyncedStatus based off
// of the underlying errors that are set.
func (s ResourceSyncedStatus) Condition(generation int64) meta.Condition {
	if s.ClusterRefInvalid != nil {
		return meta.Condition{
			Type:               ResourceSyncedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ResourceSyncedConditionReasonClusterRefInvalid,
			Message:            s.ClusterRefInvalid.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.ConfigurationInvalid != nil {
		return meta.Condition{
			Type:               ResourceSyncedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ResourceSyncedConditionReasonConfigurationInvalid,
			Message:            s.ConfigurationInvalid.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.Error != nil {
		return meta.Condition{
			Type:               ResourceSyncedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ResourceSyncedConditionReasonError,
			Message:            s.Error.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	if s.TerminalError != nil {
		return meta.Condition{
			Type:               ResourceSyncedCondition,
			Status:             meta.ConditionFalse,
			Reason:             ResourceSyncedConditionReasonTerminalError,
			Message:            s.TerminalError.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}

	return meta.Condition{
		Type:               ResourceSyncedCondition,
		Status:             meta.ConditionTrue,
		Reason:             ResourceSyncedConditionReasonSynced,
		Message:            "Cluster resource synced",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a ResourceSyncedStatus value to JSON
func (s ResourceSyncedStatus) MarshalJSON() ([]byte, error) {
	data := map[string]string{}

	if s.ClusterRefInvalid != nil {
		data["ClusterRefInvalid"] = s.ClusterRefInvalid.Error()
	}

	if s.ConfigurationInvalid != nil {
		data["ConfigurationInvalid"] = s.ConfigurationInvalid.Error()
	}

	if s.Error != nil {
		data["Error"] = s.Error.Error()
	}

	if s.TerminalError != nil {
		data["TerminalError"] = s.TerminalError.Error()
	}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a ResourceSyncedStatus from JSON
func (s *ResourceSyncedStatus) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	if err, ok := data["ClusterRefInvalid"]; ok {
		s.ClusterRefInvalid = errors.New(err)
	}

	if err, ok := data["ConfigurationInvalid"]; ok {
		s.ConfigurationInvalid = errors.New(err)
	}

	if err, ok := data["Error"]; ok {
		s.Error = errors.New(err)
	}

	if err, ok := data["TerminalError"]; ok {
		s.TerminalError = errors.New(err)
	}

	return nil
}

// HasError returns whether any of the ResourceSyncedStatus errors are set.
func (s ResourceSyncedStatus) HasError() bool {
	return s.ClusterRefInvalid != nil || s.ConfigurationInvalid != nil || s.Error != nil || s.TerminalError != nil
}

// ResourceStatus - Defines the observed status conditions of a cluster
// resource.
type ResourceStatus struct {
	// This condition indicates whether a cluster resource has been synced to its
	// cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster
	// resource.
	Synced ResourceSyncedStatus
}

// Conditions returns the aggregated status conditions of the ResourceStatus.
func (s ResourceStatus) Conditions(generation int64) []meta.Condition {
	return []meta.Condition{
		s.Synced.Condition(generation),
	}
}
