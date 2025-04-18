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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterReadyCondition - This condition indicates whether a cluster is ready
// to serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterReadyCondition string

// ClusterHealthyCondition - This condition indicates whether a cluster is fully
// healthy.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterHealthyCondition string

// ClusterLicenseValidCondition - This condition indicates whether a cluster has
// a valid license.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterLicenseValidCondition string

// ClusterResourcesSyncedCondition - This condition indicates whether cluster
// configuration parameters have currently been applied to a cluster for the
// given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterResourcesSyncedCondition string

// ClusterConfigurationAppliedCondition - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterConfigurationAppliedCondition string

// ClusterQuiescedCondition - This condition is used as to indicate that the
// cluster is no longer reconciling due to it being in a finalized state for the
// current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterQuiescedCondition string

// ClusterStableCondition - This condition is used as a roll-up status for any
// sort of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterStableCondition string

const (
	// ClusterReady - This condition indicates whether a cluster is ready to serve
	// any traffic. This can happen, for example if a cluster is partially degraded
	// but still can process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterReady = "Ready"
	// ClusterReadyReasonReady - This reason is used with the "Ready" condition when
	// the condition is True.
	ClusterReadyReasonReady ClusterReadyCondition = "Ready"
	// ClusterReadyReasonNotReady - This reason is used with the "Ready" condition
	// when a cluster is not ready.
	ClusterReadyReasonNotReady ClusterReadyCondition = "NotReady"
	// ClusterReadyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. It should only be set
	// on the conditions currently in scope for the current cluster state, any
	// subsequently derived conditions should use "StillReconciling".
	ClusterReadyReasonError ClusterReadyCondition = "Error"
	// ClusterReadyReasonTerminalError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a known terminal
	// error occurring prior to applying the desired cluster state. Any conditions
	// not already derived should also receive the "TerminalError" reason. The
	// cluster should also no longer be reconciled until it or an underlying
	// resource is changed.
	ClusterReadyReasonTerminalError ClusterReadyCondition = "TerminalError"

	// ClusterHealthy - This condition indicates whether a cluster is fully healthy.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterHealthy = "Healthy"
	// ClusterHealthyReasonHealthy - This reason is used with the "Healthy"
	// condition when the condition is True.
	ClusterHealthyReasonHealthy ClusterHealthyCondition = "Healthy"
	// ClusterHealthyReasonNotHealthy - This reason is used with the "Healthy"
	// condition when a cluster is not healthy.
	ClusterHealthyReasonNotHealthy ClusterHealthyCondition = "NotHealthy"
	// ClusterHealthyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. It should only be set
	// on the conditions currently in scope for the current cluster state, any
	// subsequently derived conditions should use "StillReconciling".
	ClusterHealthyReasonError ClusterHealthyCondition = "Error"
	// ClusterHealthyReasonTerminalError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Any
	// conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterHealthyReasonTerminalError ClusterHealthyCondition = "TerminalError"

	// ClusterLicenseValid - This condition indicates whether a cluster has a valid
	// license.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterLicenseValid = "LicenseValid"
	// ClusterLicenseValidReasonValid - This reason is used with the "LicenseValid"
	// condition when the condition is True.
	ClusterLicenseValidReasonValid ClusterLicenseValidCondition = "Valid"
	// ClusterLicenseValidReasonExpired - This reason is used with the
	// "LicenseValid" condition when a cluster has an expired license.
	ClusterLicenseValidReasonExpired ClusterLicenseValidCondition = "Expired"
	// ClusterLicenseValidReasonNotPresent - This reason is used with the
	// "LicenseValid" condition when a cluster has no license.
	ClusterLicenseValidReasonNotPresent ClusterLicenseValidCondition = "NotPresent"
	// ClusterLicenseValidReasonError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. It should only be set
	// on the conditions currently in scope for the current cluster state, any
	// subsequently derived conditions should use "StillReconciling".
	ClusterLicenseValidReasonError ClusterLicenseValidCondition = "Error"
	// ClusterLicenseValidReasonTerminalError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Any
	// conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterLicenseValidReasonTerminalError ClusterLicenseValidCondition = "TerminalError"

	// ClusterResourcesSynced - This condition indicates whether cluster
	// configuration parameters have currently been applied to a cluster for the
	// given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterResourcesSynced = "ResourcesSynced"
	// ClusterResourcesSyncedReasonSynced - This reason is used with the
	// "ClusterConfigurationApplied" condition when the condition is True.
	ClusterResourcesSyncedReasonSynced ClusterResourcesSyncedCondition = "Synced"
	// ClusterResourcesSyncedReasonError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired cluster state. It should only
	// be set on the conditions currently in scope for the current cluster state,
	// any subsequently derived conditions should use "StillReconciling".
	ClusterResourcesSyncedReasonError ClusterResourcesSyncedCondition = "Error"
	// ClusterResourcesSyncedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterResourcesSyncedReasonTerminalError ClusterResourcesSyncedCondition = "TerminalError"

	// ClusterConfigurationApplied - This condition indicates whether cluster
	// configuration parameters have currently been applied to a cluster for the
	// given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterConfigurationApplied = "ConfigurationApplied"
	// ClusterConfigurationAppliedReasonApplied - This reason is used with the
	// "ClusterConfigurationApplied" condition when the condition is True.
	ClusterConfigurationAppliedReasonApplied ClusterConfigurationAppliedCondition = "Applied"
	// ClusterConfigurationAppliedReasonError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a
	// retryable error occurring prior to applying the desired cluster state. It
	// should only be set on the conditions currently in scope for the current
	// cluster state, any subsequently derived conditions should use
	// "StillReconciling".
	ClusterConfigurationAppliedReasonError ClusterConfigurationAppliedCondition = "Error"
	// ClusterConfigurationAppliedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Any conditions not already derived should also receive the "TerminalError"
	// reason. The cluster should also no longer be reconciled until it or an
	// underlying resource is changed.
	ClusterConfigurationAppliedReasonTerminalError ClusterConfigurationAppliedCondition = "TerminalError"

	// ClusterQuiesced - This condition is used as to indicate that the cluster is
	// no longer reconciling due to it being in a finalized state for the current
	// generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterQuiesced = "Quiesced"
	// ClusterQuiescedReasonQuiesced - This reason is used with the "Quiesced"
	// condition when the condition is True.
	ClusterQuiescedReasonQuiesced ClusterQuiescedCondition = "Quiesced"
	// ClusterQuiescedReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when a cluster has only been partially reconciled and we
	// have not fully completed reconciliation. This happens when, for example,
	// we're doing a cluster scaling operation.
	ClusterQuiescedReasonStillReconciling ClusterQuiescedCondition = "StillReconciling"

	// ClusterStable - This condition is used as a roll-up status for any sort of
	// automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterStable = "Stable"
	// ClusterStableReasonStable - This reason is used with the "Stable" condition
	// when the condition is True.
	ClusterStableReasonStable ClusterStableCondition = "Stable"
	// ClusterStableReasonUnstable - This reason is used with the "Stable" condition
	// when a cluster has not yet stabilized for automation purposes.
	ClusterStableReasonUnstable ClusterStableCondition = "Unstable"
)

// ClusterStatus - Defines the observed status conditions of a cluster.
type ClusterStatus struct {
	generation                           int64
	conditions                           []metav1.Condition
	hasTerminalError                     bool
	isReadySet                           bool
	isReadyTransientError                bool
	isHealthySet                         bool
	isHealthyTransientError              bool
	isLicenseValidSet                    bool
	isLicenseValidTransientError         bool
	isResourcesSyncedSet                 bool
	isResourcesSyncedTransientError      bool
	isConfigurationAppliedSet            bool
	isConfigurationAppliedTransientError bool
}

// NewCluster() returns a new ClusterStatus
func NewCluster(generation int64) *ClusterStatus {
	return &ClusterStatus{
		generation: generation,
	}
}

// Conditions returns the aggregated status conditions of the ClusterStatus.
func (s *ClusterStatus) Conditions() []metav1.Condition {
	conditions := []metav1.Condition{}

	for _, condition := range s.conditions {
		conditions = append(conditions, condition)
	}
	conditions = append(conditions, s.getQuiesced())
	conditions = append(conditions, s.getStable())

	return conditions
}

// SetReady sets the underlying condition to the given reason.
func (s *ClusterStatus) SetReady(reason ClusterReadyCondition, messages ...string) {
	if s.isReadySet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isReadySet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterReadyReasonReady:
		if message == "" {
			message = "Cluster ready to service requests"
		}
		status = metav1.ConditionTrue
	case ClusterReadyReasonNotReady:
		status = metav1.ConditionFalse
	case ClusterReadyReasonError:
		s.isReadyTransientError = true
		status = metav1.ConditionFalse
	case ClusterReadyReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:               ClusterReady,
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: s.generation,
	})
}

// SetHealthy sets the underlying condition to the given reason.
func (s *ClusterStatus) SetHealthy(reason ClusterHealthyCondition, messages ...string) {
	if s.isHealthySet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isHealthySet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterHealthyReasonHealthy:
		if message == "" {
			message = "Cluster is healthy"
		}
		status = metav1.ConditionTrue
	case ClusterHealthyReasonNotHealthy:
		status = metav1.ConditionFalse
	case ClusterHealthyReasonError:
		s.isHealthyTransientError = true
		status = metav1.ConditionFalse
	case ClusterHealthyReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:               ClusterHealthy,
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: s.generation,
	})
}

// SetLicenseValid sets the underlying condition to the given reason.
func (s *ClusterStatus) SetLicenseValid(reason ClusterLicenseValidCondition, messages ...string) {
	if s.isLicenseValidSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isLicenseValidSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterLicenseValidReasonValid:
		if message == "" {
			message = "Cluster has a valid license"
		}
		status = metav1.ConditionTrue
	case ClusterLicenseValidReasonExpired:
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonNotPresent:
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonError:
		s.isLicenseValidTransientError = true
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:               ClusterLicenseValid,
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: s.generation,
	})
}

// SetResourcesSynced sets the underlying condition to the given reason.
func (s *ClusterStatus) SetResourcesSynced(reason ClusterResourcesSyncedCondition, messages ...string) {
	if s.isResourcesSyncedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isResourcesSyncedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterResourcesSyncedReasonSynced:
		if message == "" {
			message = "Cluster configuration successfully applied"
		}
		status = metav1.ConditionTrue
	case ClusterResourcesSyncedReasonError:
		s.isResourcesSyncedTransientError = true
		status = metav1.ConditionFalse
	case ClusterResourcesSyncedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:               ClusterResourcesSynced,
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: s.generation,
	})
}

// SetConfigurationApplied sets the underlying condition to the given reason.
func (s *ClusterStatus) SetConfigurationApplied(reason ClusterConfigurationAppliedCondition, messages ...string) {
	if s.isConfigurationAppliedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isConfigurationAppliedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterConfigurationAppliedReasonApplied:
		if message == "" {
			message = "Cluster configuration successfully applied"
		}
		status = metav1.ConditionTrue
	case ClusterConfigurationAppliedReasonError:
		s.isConfigurationAppliedTransientError = true
		status = metav1.ConditionFalse
	case ClusterConfigurationAppliedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:               ClusterConfigurationApplied,
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: s.generation,
	})
}

func (s *ClusterStatus) getQuiesced() metav1.Condition {
	transientErrorConditionsSet := s.isReadyTransientError || s.isHealthyTransientError || s.isLicenseValidTransientError || s.isResourcesSyncedTransientError || s.isConfigurationAppliedTransientError
	allConditionsSet := s.isReadySet && s.isHealthySet && s.isLicenseValidSet && s.isResourcesSyncedSet && s.isConfigurationAppliedSet

	if (allConditionsSet || s.hasTerminalError) && !transientErrorConditionsSet {
		return metav1.Condition{
			Type:               ClusterQuiesced,
			Status:             metav1.ConditionTrue,
			Reason:             string(ClusterQuiescedReasonQuiesced),
			Message:            "Cluster reconciliation finished",
			ObservedGeneration: s.generation,
		}
	}

	return metav1.Condition{
		Type:               ClusterQuiesced,
		Status:             metav1.ConditionFalse,
		Reason:             string(ClusterQuiescedReasonStillReconciling),
		Message:            "Cluster still reconciling",
		ObservedGeneration: s.generation,
	}
}

func (s *ClusterStatus) getStable() metav1.Condition {
	allConditionsFoundAndTrue := true
	for _, condition := range []string{ClusterQuiesced, ClusterReady, ClusterLicenseValid, ClusterResourcesSynced, ClusterConfigurationApplied} {
		conditionFoundAndTrue := false
		for _, setCondition := range s.conditions {
			if setCondition.Type == condition {
				conditionFoundAndTrue = setCondition.Status == metav1.ConditionTrue
				break
			}
		}
		if !conditionFoundAndTrue {
			allConditionsFoundAndTrue = false
			break
		}
	}

	if allConditionsFoundAndTrue {
		return metav1.Condition{
			Type:               ClusterStable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ClusterStableReasonStable),
			Message:            "Cluster Stable",
			ObservedGeneration: s.generation,
		}
	}

	return metav1.Condition{
		Type:               ClusterStable,
		Status:             metav1.ConditionFalse,
		Reason:             string(ClusterStableReasonUnstable),
		Message:            "Cluster Unstable",
		ObservedGeneration: s.generation,
	}
}
