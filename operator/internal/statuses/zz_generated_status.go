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
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// ClusterReadyCondition - This condition indicates whether a cluster is ready
// to serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterReadyCondition string

// ClusterHealthyCondition - This condition indicates whether a cluster is
// healthy as defined by the Redpanda Admin API's cluster health endpoint.
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

// ClusterResourcesSyncedCondition - This condition indicates whether the
// Kubernetes resources for a cluster have been synchronized.
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
	// it evaluates to True because a cluster can service traffic.
	ClusterReadyReasonReady ClusterReadyCondition = "Ready"
	// ClusterReadyReasonNotReady - This reason is used with the "Ready" condition
	// when it evaluates to False because a cluster is not ready to service traffic.
	ClusterReadyReasonNotReady ClusterReadyCondition = "NotReady"
	// ClusterReadyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterReadyReasonError ClusterReadyCondition = "Error"
	// ClusterReadyReasonTerminalError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a known terminal
	// error occurring prior to applying the desired cluster state. Because the
	// cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterReadyReasonTerminalError ClusterReadyCondition = "TerminalError"

	// ClusterHealthy - This condition indicates whether a cluster is healthy as
	// defined by the Redpanda Admin API's cluster health endpoint.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterHealthy = "Healthy"
	// ClusterHealthyReasonHealthy - This reason is used with the "Healthy"
	// condition when it evaluates to True because a cluster's health endpoint says
	// the cluster is healthy.
	ClusterHealthyReasonHealthy ClusterHealthyCondition = "Healthy"
	// ClusterHealthyReasonNotHealthy - This reason is used with the "Healthy"
	// condition when it evaluates to False because a cluster's health endpoint says
	// the cluster is not healthy.
	ClusterHealthyReasonNotHealthy ClusterHealthyCondition = "NotHealthy"
	// ClusterHealthyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterHealthyReasonError ClusterHealthyCondition = "Error"
	// ClusterHealthyReasonTerminalError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Because
	// the cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterHealthyReasonTerminalError ClusterHealthyCondition = "TerminalError"

	// ClusterLicenseValid - This condition indicates whether a cluster has a valid
	// license.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterLicenseValid = "LicenseValid"
	// ClusterLicenseValidReasonValid - This reason is used with the "LicenseValid"
	// condition when it evaluates to True because a cluster has a valid license.
	ClusterLicenseValidReasonValid ClusterLicenseValidCondition = "Valid"
	// ClusterLicenseValidReasonExpired - This reason is used with the
	// "LicenseValid" condition when it evaluates to False because a cluster has an
	// expired license.
	ClusterLicenseValidReasonExpired ClusterLicenseValidCondition = "Expired"
	// ClusterLicenseValidReasonNotPresent - This reason is used with the
	// "LicenseValid" condition when it evaluates to False because a cluster has no
	// license.
	ClusterLicenseValidReasonNotPresent ClusterLicenseValidCondition = "NotPresent"
	// ClusterLicenseValidReasonError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterLicenseValidReasonError ClusterLicenseValidCondition = "Error"
	// ClusterLicenseValidReasonTerminalError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Because
	// the cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterLicenseValidReasonTerminalError ClusterLicenseValidCondition = "TerminalError"

	// ClusterResourcesSynced - This condition indicates whether the Kubernetes
	// resources for a cluster have been synchronized.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterResourcesSynced = "ResourcesSynced"
	// ClusterResourcesSyncedReasonSynced - This reason is used with the
	// "ResourcesSynced" condition when it evaluates to True because a cluster has
	// had all of its Kubernetes resources synced.
	ClusterResourcesSyncedReasonSynced ClusterResourcesSyncedCondition = "Synced"
	// ClusterResourcesSyncedReasonError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired cluster state. If it is set on
	// any non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterResourcesSyncedReasonError ClusterResourcesSyncedCondition = "Error"
	// ClusterResourcesSyncedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Because the cluster should no longer be reconciled when a terminal error
	// occurs, the "Quiesced" status should be set to True.
	ClusterResourcesSyncedReasonTerminalError ClusterResourcesSyncedCondition = "TerminalError"

	// ClusterConfigurationApplied - This condition indicates whether cluster
	// configuration parameters have currently been applied to a cluster for the
	// given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterConfigurationApplied = "ConfigurationApplied"
	// ClusterConfigurationAppliedReasonApplied - This reason is used with the
	// "ConfigurationApplied" condition when it evaluates to True because a cluster
	// has had its cluster configuration parameters applied.
	ClusterConfigurationAppliedReasonApplied ClusterConfigurationAppliedCondition = "Applied"
	// ClusterConfigurationAppliedReasonError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a
	// retryable error occurring prior to applying the desired cluster state. If it
	// is set on any non-final condition, then the condition "Quiesced" will be
	// False with a reason of "SillReconciling".
	ClusterConfigurationAppliedReasonError ClusterConfigurationAppliedCondition = "Error"
	// ClusterConfigurationAppliedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Because the cluster should no longer be reconciled when a terminal error
	// occurs, the "Quiesced" status should be set to True.
	ClusterConfigurationAppliedReasonTerminalError ClusterConfigurationAppliedCondition = "TerminalError"

	// ClusterQuiesced - This condition is used as to indicate that the cluster is
	// no longer reconciling due to it being in a finalized state for the current
	// generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterQuiesced = "Quiesced"
	// ClusterQuiescedReasonQuiesced - This reason is used with the "Quiesced"
	// condition when it evaluates to True because the operator has finished
	// reconciling the cluster at its current generation.
	ClusterQuiescedReasonQuiesced ClusterQuiescedCondition = "Quiesced"
	// ClusterQuiescedReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when it evaluates to False because the operator has not
	// finished reconciling the cluster at its current generation. This can happen
	// when, for example, we're doing a cluster scaling operation or a non-terminal
	// error has been encountered during reconciliation.
	ClusterQuiescedReasonStillReconciling ClusterQuiescedCondition = "StillReconciling"

	// ClusterStable - This condition is used as a roll-up status for any sort of
	// automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterStable = "Stable"
	// ClusterStableReasonStable - This reason is used with the "Stable" condition
	// when it evaluates to True because all dependent conditions also evaluate to
	// True.
	ClusterStableReasonStable ClusterStableCondition = "Stable"
	// ClusterStableReasonUnstable - This reason is used with the "Stable" condition
	// when it evaluates to True because at least one dependent condition evaluates
	// to False.
	ClusterStableReasonUnstable ClusterStableCondition = "Unstable"
)

// ClusterStatus - Defines the observed status conditions of a cluster.
type ClusterStatus struct {
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
func NewCluster() *ClusterStatus {
	return &ClusterStatus{}
}

// UpdateConditions updates any conditions for the passed in object that need to be updated.
func (s *ClusterStatus) UpdateConditions(o client.Object) bool {
	var conditions *[]metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		conditions = &kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	updated := false
	for _, condition := range s.getConditions(o.GetGeneration()) {
		if setStatusCondition(conditions, condition) {
			updated = true
		}
	}

	return updated
}

// StatusConditionConfigs returns a set of configurations that can be used with Server Side Apply.
func (s *ClusterStatus) StatusConditionConfigs(o client.Object) []*applymetav1.ConditionApplyConfiguration {
	var conditions []metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		conditions = kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	return utils.StatusConditionConfigs(conditions, o.GetGeneration(), s.getConditions(o.GetGeneration()))
}

// conditions returns the aggregated status conditions of the ClusterStatus.
func (s *ClusterStatus) getConditions(generation int64) []metav1.Condition {
	conditions := append([]metav1.Condition{}, s.conditions...)
	conditions = append(conditions, s.getQuiesced())
	conditions = append(conditions, s.getStable(conditions))

	for i, condition := range conditions {
		condition.ObservedGeneration = generation
		conditions[i] = condition
	}

	return conditions
}

// SetReadyFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetReadyFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterReady)
	if condition == nil {
		return
	}

	s.SetReady(ClusterReadyCondition(condition.Reason), condition.Message)
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
		Type:    ClusterReady,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetHealthyFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetHealthyFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterHealthy)
	if condition == nil {
		return
	}

	s.SetHealthy(ClusterHealthyCondition(condition.Reason), condition.Message)
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
		Type:    ClusterHealthy,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetLicenseValidFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetLicenseValidFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterLicenseValid)
	if condition == nil {
		return
	}

	s.SetLicenseValid(ClusterLicenseValidCondition(condition.Reason), condition.Message)
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
		if message == "" {
			message = "Cluster license has expired"
		}
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonNotPresent:
		if message == "" {
			message = "No cluster license is present"
		}
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
		Type:    ClusterLicenseValid,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetResourcesSyncedFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetResourcesSyncedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterResourcesSynced)
	if condition == nil {
		return
	}

	s.SetResourcesSynced(ClusterResourcesSyncedCondition(condition.Reason), condition.Message)
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
			message = "Cluster resources successfully synced"
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
		Type:    ClusterResourcesSynced,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetConfigurationAppliedFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetConfigurationAppliedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterConfigurationApplied)
	if condition == nil {
		return
	}

	s.SetConfigurationApplied(ClusterConfigurationAppliedCondition(condition.Reason), condition.Message)
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
		Type:    ClusterConfigurationApplied,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

func (s *ClusterStatus) getQuiesced() metav1.Condition {
	transientErrorConditionsSet := s.isReadyTransientError || s.isHealthyTransientError || s.isLicenseValidTransientError || s.isResourcesSyncedTransientError || s.isConfigurationAppliedTransientError
	allConditionsSet := s.isReadySet && s.isHealthySet && s.isLicenseValidSet && s.isResourcesSyncedSet && s.isConfigurationAppliedSet

	if (allConditionsSet || s.hasTerminalError) && !transientErrorConditionsSet {
		return metav1.Condition{
			Type:    ClusterQuiesced,
			Status:  metav1.ConditionTrue,
			Reason:  string(ClusterQuiescedReasonQuiesced),
			Message: "Cluster reconciliation finished",
		}
	}

	return metav1.Condition{
		Type:    ClusterQuiesced,
		Status:  metav1.ConditionFalse,
		Reason:  string(ClusterQuiescedReasonStillReconciling),
		Message: "Cluster still reconciling",
	}
}

func (s *ClusterStatus) getStable(conditions []metav1.Condition) metav1.Condition {
	allConditionsFoundAndTrue := true
	for _, condition := range []string{ClusterQuiesced, ClusterReady, ClusterResourcesSynced, ClusterConfigurationApplied} {
		conditionFoundAndTrue := false
		for _, setCondition := range conditions {
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
			Type:    ClusterStable,
			Status:  metav1.ConditionTrue,
			Reason:  string(ClusterStableReasonStable),
			Message: "Cluster Stable",
		}
	}

	return metav1.Condition{
		Type:    ClusterStable,
		Status:  metav1.ConditionFalse,
		Reason:  string(ClusterStableReasonUnstable),
		Message: "Cluster Unstable",
	}
}

// HasRecentCondition returns whether or not an object has a given condition with the given value that is up-to-date and set
// within the given time period.
func HasRecentCondition[T ~string](o client.Object, conditionType T, value metav1.ConditionStatus, period time.Duration) bool {
	condition := apimeta.FindStatusCondition(GetConditions(o), string(conditionType))
	if condition == nil {
		return false
	}

	recent := time.Since(condition.LastTransitionTime.Time) > period
	matchedCondition := condition.Status == value
	generationChanged := condition.ObservedGeneration != 0 && condition.ObservedGeneration < o.GetGeneration()

	return matchedCondition && !(generationChanged || recent)
}

// GetConditions returns the conditions for a given object.
func GetConditions(o client.Object) []metav1.Condition {
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		return kind.Status.Conditions
	default:
		panic("unsupported kind")
	}
}

// setStatusCondition is a copy of the apimeta.SetStatusCondition with one primary change. Rather
// than only change the .LastTransitionTime if the .Status field of the condition changes, it
// sets it if .Status, .Reason, .Message, or .ObservedGeneration changes, which works nicely with our recent check leveraged
// for rate limiting above. It also normalizes this to be the same as what utils.StatusConditionConfigs does
func setStatusCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) (changed bool) {
	if conditions == nil {
		return false
	}
	existingCondition := apimeta.FindStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		*conditions = append(*conditions, newCondition)
		return true
	}

	setTransitionTime := func() {
		if !newCondition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		setTransitionTime()
		changed = true
	}

	if existingCondition.Reason != newCondition.Reason {
		existingCondition.Reason = newCondition.Reason
		setTransitionTime()
		changed = true
	}
	if existingCondition.Message != newCondition.Message {
		existingCondition.Message = newCondition.Message
		setTransitionTime()
		changed = true
	}
	if existingCondition.ObservedGeneration != newCondition.ObservedGeneration {
		existingCondition.ObservedGeneration = newCondition.ObservedGeneration
		setTransitionTime()
		changed = true
	}

	return changed
}
