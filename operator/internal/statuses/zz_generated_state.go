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
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RetryError can be used to explicitly requeue reconciliation
// early due to some known status condition that is not an "error"
// per-se, but a known point at which reconciliation needs to wait
// for some condition to be true.
type RetryError struct {
	message string
	after   time.Duration
}

// Error returns the underlying cause of the retry
func (r RetryError) Error() string { return r.message }

// IsRetryError returns true if the passed in error has a RetryError in
// its wrapped chain
func IsRetryError(err error) bool {
	if err == nil {
		return false
	}
	return errors.As(err, &RetryError{})
}

func retryIn(err error) time.Duration {
	var rerr RetryError
	if errors.As(err, &rerr) {
		return rerr.after
	}
	return 0
}

// Retry constructs a RetryError
func Retry(message string) RetryError { return RetryError{message: message} }

// RetryIn constructs a RetryError
func RetryIn(message string, after time.Duration) RetryError {
	return RetryError{message: message, after: after}
}

// TerminalError can be used to explicitly mark a reconciliation as
// finished prematurely due to some unrecoverable error. It should
// only be used with known terminal error states.
type TerminalError struct{ error }

// Terminal constructs a TerminalError
func Terminal(err error) TerminalError { return TerminalError{err} }

// IsTerminalError returns true if the passed in error has a TerminalError in
// its wrapped chain
func IsTerminalError(err error) bool {
	if err == nil {
		return false
	}
	return errors.As(err, &TerminalError{})
}

var (
	// ErrClusterStateCheckHealthRetryable is the error set on any condition that is derived in
	// a subsequent transition when a retryable error has occurred while checking health
	ErrClusterStateCheckHealthRetryable = errors.New("retrying after error checking health")
	// ErrClusterStateCheckLicenseRetryable is the error set on any condition that is derived in
	// a subsequent transition when a retryable error has occurred while checking license validity
	ErrClusterStateCheckLicenseRetryable = errors.New("retrying after error checking license validity")
	// ErrClusterStateSyncResourcesRetryable is the error set on any condition that is derived in
	// a subsequent transition when a retryable error has occurred while synchronizing cluster resources
	ErrClusterStateSyncResourcesRetryable = errors.New("retrying after error synchronizing cluster resources")
	// ErrClusterStateApplyConfigurationRetryable is the error set on any condition that is derived in
	// a subsequent transition when a retryable error has occurred while applying cluster configuration
	ErrClusterStateApplyConfigurationRetryable = errors.New("retrying after error applying cluster configuration")
	// ErrClusterStateCheckHealthTerminal is the error set on any any condition that is derived in
	// a subsequent transition when a known terminal error has occurred while checking health
	ErrClusterStateCheckHealthTerminal = errors.New("terminal error when checking health")
	// ErrClusterStateCheckLicenseTerminal is the error set on any any condition that is derived in
	// a subsequent transition when a known terminal error has occurred while checking license validity
	ErrClusterStateCheckLicenseTerminal = errors.New("terminal error when checking license validity")
	// ErrClusterStateSyncResourcesTerminal is the error set on any any condition that is derived in
	// a subsequent transition when a known terminal error has occurred while synchronizing cluster resources
	ErrClusterStateSyncResourcesTerminal = errors.New("terminal error when synchronizing cluster resources")
	// ErrClusterStateApplyConfigurationTerminal is the error set on any any condition that is derived in
	// a subsequent transition when a known terminal error has occurred while applying cluster configuration
	ErrClusterStateApplyConfigurationTerminal = errors.New("terminal error when applying cluster configuration")
	// ClusterStateRequeueBeforeCheckHealthMessage is the message used to requeue a reconciliation
	// request before transitioning away from checking health
	ClusterStateRequeueBeforeCheckHealthMessage = "requeueing before finishing checking health"
	// ClusterStateRequeueBeforeCheckLicenseMessage is the message used to requeue a reconciliation
	// request before transitioning away from checking license validity
	ClusterStateRequeueBeforeCheckLicenseMessage = "requeueing before finishing checking license validity"
	// ClusterStateRequeueBeforeSyncResourcesMessage is the message used to requeue a reconciliation
	// request before transitioning away from synchronizing cluster resources
	ClusterStateRequeueBeforeSyncResourcesMessage = "requeueing before finishing synchronizing cluster resources"
	// ClusterStateRequeueBeforeApplyConfigurationMessage is the message used to requeue a reconciliation
	// request before transitioning away from applying cluster configuration
	ClusterStateRequeueBeforeApplyConfigurationMessage = "requeueing before finishing applying cluster configuration"
)

// ClusterInitialInputOption is used to set any initial conditions for the status state.
type ClusterInitialInputOption interface {
	applyInitialInput(status *ClusterStatus)
}

type optionClusterReadyCondition struct {
	applyFn func(status *ClusterStatus)
}

func (o *optionClusterReadyCondition) applyInitialInput(status *ClusterStatus) {
	o.applyFn(status)
}

// WithClusterNotReady is used to mark the underlying Ready with the NotReady reason.
func WithClusterNotReady(message string) ClusterInitialInputOption {
	return &optionClusterReadyCondition{
		applyFn: func(status *ClusterStatus) {
			status.Ready.NotReady = errors.New(message)
		},
	}
}

type argumentClusterState struct{}

func defaultArgumentClusterState() *argumentClusterState {
	return &argumentClusterState{}
}

// ClusterStateCheckHealth represents a controller which is currently checking health
type ClusterStateCheckHealth struct {
	generation   int64
	arguments    *argumentClusterState
	syncFn       func(conditions []metav1.Condition) (ctrl.Result, error)
	status       *ClusterStatus
	transitioned bool
	synced       bool
}

func (s *ClusterStateCheckHealth) conditions(errs ...error) []metav1.Condition {
	err := errors.Join(errs...)
	isRetry := errors.As(err, &RetryError{})
	isTerminal := errors.As(err, &TerminalError{})

	if isRetry {
		s.status.Healthy.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckHealthMessage, err)
		s.status.LicenseValid.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckHealthMessage, err)
		s.status.ClusterResourcesSynced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckHealthMessage, err)
		s.status.ClusterConfigurationApplied.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckHealthMessage, err)
		s.status.Quiesced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckHealthMessage, err)

		return s.status.Conditions(s.generation)
	}

	if isTerminal {
		// we only set terminal errors on non-final conditions, namely because
		// the errors are *terminal* - hence we won't retry.
		s.status.Healthy.TerminalError = err
		s.status.LicenseValid.TerminalError = ErrClusterStateCheckHealthTerminal
		s.status.ClusterResourcesSynced.TerminalError = ErrClusterStateCheckHealthTerminal
		s.status.ClusterConfigurationApplied.TerminalError = ErrClusterStateCheckHealthTerminal

		return s.status.Conditions(s.generation)
	}

	s.status.Healthy.Error = err
	s.status.LicenseValid.StillReconciling = ErrClusterStateCheckHealthRetryable
	s.status.ClusterResourcesSynced.StillReconciling = ErrClusterStateCheckHealthRetryable
	s.status.ClusterConfigurationApplied.StillReconciling = ErrClusterStateCheckHealthRetryable
	s.status.Quiesced.StillReconciling = ErrClusterStateCheckHealthRetryable

	return s.status.Conditions(s.generation)
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync can only ever be called once and cannot be called
// if the state is transitioned away from with a TransitionTo* call. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *ClusterStateCheckHealth) Sync(errs ...error) (ctrl.Result, error) {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it should no longer be used for synchronization")
	}
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true
	// final roll-up is set here, there will be an error because we synchronized early in this case
	s.status.Stable.Unstable = fmt.Errorf("cluster not stable: %w", errors.Join(
		s.status.Quiesced.MaybeError(),
		s.status.Ready.MaybeError(),
		s.status.ClusterResourcesSynced.MaybeError(),
		s.status.ClusterConfigurationApplied.MaybeError(),
	))

	result, err := s.syncFn(s.conditions(errs...))
	combinedErr := errors.Join(append(errs, err)...)
	if IsTerminalError(combinedErr) {
		// this is marked as terminal, only requeue if we've errored when synchronizing
		return ctrl.Result{}, err
	}
	if IsRetryError(combinedErr) && err == nil {
		// this is retryable and we did not have an error synchronizing, so ignore the
		// original error and just retry directly
		return ctrl.Result{Requeue: true, RequeueAfter: retryIn(combinedErr)}, nil
	}
	if combinedErr != nil {
		// this is not an explicit retry, so just propagate whatever error
		return ctrl.Result{}, combinedErr
	}
	// no errors, so we halt
	return result, nil
}

// InitializeClusterState returns the initial state used for tracking reconciliation state, it takes a synchronization function
// called when statuses are to be fully synchronized with Kubernetes and a list of initial states to set by way of ClusterInitialInputOption.
func InitializeClusterState(generation int64, syncFn func(conditions []metav1.Condition) (ctrl.Result, error), options ...ClusterInitialInputOption) *ClusterStateCheckHealth {
	status := NewCluster()

	for _, option := range options {
		option.applyInitialInput(status)
	}

	return &ClusterStateCheckHealth{generation: generation, status: status, syncFn: syncFn, arguments: defaultArgumentClusterState()}
}

// TransitionToCheckLicense returns the next state in the state machine, marking the current state as transitioned and no longer usable
func (s *ClusterStateCheckHealth) TransitionToCheckLicense() *ClusterStateCheckLicense {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}

	s.transitioned = true
	return &ClusterStateCheckLicense{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}

// NotHealthy sets the  NotHealthy reason on the Healthy condition
func (s *ClusterStateCheckHealth) NotHealthy(message string) *ClusterStateCheckHealth {
	s.status.Healthy.NotHealthy = errors.New(message)
	return s
}

// ClusterStateCheckLicense represents a controller which is currently checking license validity
type ClusterStateCheckLicense struct {
	generation   int64
	arguments    *argumentClusterState
	syncFn       func(conditions []metav1.Condition) (ctrl.Result, error)
	status       *ClusterStatus
	transitioned bool
	synced       bool
}

func (s *ClusterStateCheckLicense) conditions(errs ...error) []metav1.Condition {
	err := errors.Join(errs...)
	isRetry := errors.As(err, &RetryError{})
	isTerminal := errors.As(err, &TerminalError{})

	if isRetry {
		s.status.LicenseValid.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckLicenseMessage, err)
		s.status.ClusterResourcesSynced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckLicenseMessage, err)
		s.status.ClusterConfigurationApplied.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckLicenseMessage, err)
		s.status.Quiesced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeCheckLicenseMessage, err)

		return s.status.Conditions(s.generation)
	}

	if isTerminal {
		// we only set terminal errors on non-final conditions, namely because
		// the errors are *terminal* - hence we won't retry.
		s.status.LicenseValid.TerminalError = err
		s.status.ClusterResourcesSynced.TerminalError = ErrClusterStateCheckLicenseTerminal
		s.status.ClusterConfigurationApplied.TerminalError = ErrClusterStateCheckLicenseTerminal

		return s.status.Conditions(s.generation)
	}

	s.status.LicenseValid.Error = err
	s.status.ClusterResourcesSynced.StillReconciling = ErrClusterStateCheckLicenseRetryable
	s.status.ClusterConfigurationApplied.StillReconciling = ErrClusterStateCheckLicenseRetryable
	s.status.Quiesced.StillReconciling = ErrClusterStateCheckLicenseRetryable

	return s.status.Conditions(s.generation)
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync can only ever be called once and cannot be called
// if the state is transitioned away from with a TransitionTo* call. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *ClusterStateCheckLicense) Sync(errs ...error) (ctrl.Result, error) {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it should no longer be used for synchronization")
	}
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true
	// final roll-up is set here, there will be an error because we synchronized early in this case
	s.status.Stable.Unstable = fmt.Errorf("cluster not stable: %w", errors.Join(
		s.status.Quiesced.MaybeError(),
		s.status.Ready.MaybeError(),
		s.status.ClusterResourcesSynced.MaybeError(),
		s.status.ClusterConfigurationApplied.MaybeError(),
	))

	result, err := s.syncFn(s.conditions(errs...))
	combinedErr := errors.Join(append(errs, err)...)
	if IsTerminalError(combinedErr) {
		// this is marked as terminal, only requeue if we've errored when synchronizing
		return ctrl.Result{}, err
	}
	if IsRetryError(combinedErr) && err == nil {
		// this is retryable and we did not have an error synchronizing, so ignore the
		// original error and just retry directly
		return ctrl.Result{Requeue: true, RequeueAfter: retryIn(combinedErr)}, nil
	}
	if combinedErr != nil {
		// this is not an explicit retry, so just propagate whatever error
		return ctrl.Result{}, combinedErr
	}
	// no errors, so we halt
	return result, nil
}

// TransitionToSyncResources returns the next state in the state machine, marking the current state as transitioned and no longer usable
func (s *ClusterStateCheckLicense) TransitionToSyncResources() *ClusterStateSyncResources {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}

	s.transitioned = true
	return &ClusterStateSyncResources{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}

// LicenseExpired sets the  LicenseExpired reason on the LicenseValid condition
func (s *ClusterStateCheckLicense) LicenseExpired(message string) *ClusterStateCheckLicense {
	s.status.LicenseValid.LicenseExpired = errors.New(message)
	return s
}

// LicenseNotPresent sets the  LicenseNotPresent reason on the LicenseValid condition
func (s *ClusterStateCheckLicense) LicenseNotPresent(message string) *ClusterStateCheckLicense {
	s.status.LicenseValid.LicenseNotPresent = errors.New(message)
	return s
}

// ClusterStateSyncResources represents a controller which is currently synchronizing cluster resources
type ClusterStateSyncResources struct {
	generation   int64
	arguments    *argumentClusterState
	syncFn       func(conditions []metav1.Condition) (ctrl.Result, error)
	status       *ClusterStatus
	transitioned bool
	synced       bool
}

func (s *ClusterStateSyncResources) conditions(errs ...error) []metav1.Condition {
	err := errors.Join(errs...)
	isRetry := errors.As(err, &RetryError{})
	isTerminal := errors.As(err, &TerminalError{})

	if isRetry {
		s.status.ClusterResourcesSynced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeSyncResourcesMessage, err)
		s.status.ClusterConfigurationApplied.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeSyncResourcesMessage, err)
		s.status.Quiesced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeSyncResourcesMessage, err)

		return s.status.Conditions(s.generation)
	}

	if isTerminal {
		// we only set terminal errors on non-final conditions, namely because
		// the errors are *terminal* - hence we won't retry.
		s.status.ClusterResourcesSynced.TerminalError = err
		s.status.ClusterConfigurationApplied.TerminalError = ErrClusterStateSyncResourcesTerminal

		return s.status.Conditions(s.generation)
	}

	s.status.ClusterResourcesSynced.Error = err
	s.status.ClusterConfigurationApplied.StillReconciling = ErrClusterStateSyncResourcesRetryable
	s.status.Quiesced.StillReconciling = ErrClusterStateSyncResourcesRetryable

	return s.status.Conditions(s.generation)
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync can only ever be called once and cannot be called
// if the state is transitioned away from with a TransitionTo* call. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *ClusterStateSyncResources) Sync(errs ...error) (ctrl.Result, error) {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it should no longer be used for synchronization")
	}
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true
	// final roll-up is set here, there will be an error because we synchronized early in this case
	s.status.Stable.Unstable = fmt.Errorf("cluster not stable: %w", errors.Join(
		s.status.Quiesced.MaybeError(),
		s.status.Ready.MaybeError(),
		s.status.ClusterResourcesSynced.MaybeError(),
		s.status.ClusterConfigurationApplied.MaybeError(),
	))

	result, err := s.syncFn(s.conditions(errs...))
	combinedErr := errors.Join(append(errs, err)...)
	if IsTerminalError(combinedErr) {
		// this is marked as terminal, only requeue if we've errored when synchronizing
		return ctrl.Result{}, err
	}
	if IsRetryError(combinedErr) && err == nil {
		// this is retryable and we did not have an error synchronizing, so ignore the
		// original error and just retry directly
		return ctrl.Result{Requeue: true, RequeueAfter: retryIn(combinedErr)}, nil
	}
	if combinedErr != nil {
		// this is not an explicit retry, so just propagate whatever error
		return ctrl.Result{}, combinedErr
	}
	// no errors, so we halt
	return result, nil
}

// TransitionToApplyConfiguration returns the next state in the state machine, marking the current state as transitioned and no longer usable
func (s *ClusterStateSyncResources) TransitionToApplyConfiguration() *ClusterStateApplyConfiguration {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}

	s.transitioned = true
	return &ClusterStateApplyConfiguration{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}

// ClusterStateApplyConfiguration represents a controller which is currently applying cluster configuration
type ClusterStateApplyConfiguration struct {
	generation   int64
	arguments    *argumentClusterState
	syncFn       func(conditions []metav1.Condition) (ctrl.Result, error)
	status       *ClusterStatus
	transitioned bool
	synced       bool
}

func (s *ClusterStateApplyConfiguration) conditions(errs ...error) []metav1.Condition {
	err := errors.Join(errs...)
	isRetry := errors.As(err, &RetryError{})
	isTerminal := errors.As(err, &TerminalError{})

	if isRetry {
		s.status.ClusterConfigurationApplied.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeApplyConfigurationMessage, err)
		s.status.Quiesced.StillReconciling = fmt.Errorf("%s: %w", ClusterStateRequeueBeforeApplyConfigurationMessage, err)

		return s.status.Conditions(s.generation)
	}

	if isTerminal {
		// we only set terminal errors on non-final conditions, namely because
		// the errors are *terminal* - hence we won't retry.
		s.status.ClusterConfigurationApplied.TerminalError = err

		return s.status.Conditions(s.generation)
	}

	s.status.ClusterConfigurationApplied.Error = err
	s.status.Quiesced.StillReconciling = ErrClusterStateApplyConfigurationRetryable

	return s.status.Conditions(s.generation)
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync can only ever be called once and cannot be called
// if the state is transitioned away from with a TransitionTo* call. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *ClusterStateApplyConfiguration) Sync(errs ...error) (ctrl.Result, error) {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it should no longer be used for synchronization")
	}
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true
	// final roll-up is set here, there will be an error because we synchronized early in this case
	s.status.Stable.Unstable = fmt.Errorf("cluster not stable: %w", errors.Join(
		s.status.Quiesced.MaybeError(),
		s.status.Ready.MaybeError(),
		s.status.ClusterResourcesSynced.MaybeError(),
		s.status.ClusterConfigurationApplied.MaybeError(),
	))

	result, err := s.syncFn(s.conditions(errs...))
	combinedErr := errors.Join(append(errs, err)...)
	if IsTerminalError(combinedErr) {
		// this is marked as terminal, only requeue if we've errored when synchronizing
		return ctrl.Result{}, err
	}
	if IsRetryError(combinedErr) && err == nil {
		// this is retryable and we did not have an error synchronizing, so ignore the
		// original error and just retry directly
		return ctrl.Result{Requeue: true, RequeueAfter: retryIn(combinedErr)}, nil
	}
	if combinedErr != nil {
		// this is not an explicit retry, so just propagate whatever error
		return ctrl.Result{}, combinedErr
	}
	// no errors, so we halt
	return result, nil
}

// Finish marks the state machine has having completed all of its processing.
func (s *ClusterStateApplyConfiguration) Finish() *ClusterStateFinished {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}
	if s.synced {
		panic("states are meant to transition only before synchronization, this state is already marked as synced, so it cannot transition again")
	}

	s.transitioned = true
	return &ClusterStateFinished{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}

// ClusterStateFinished represents a reconciliation loop that has finished all of its processing
type ClusterStateFinished struct {
	generation int64
	status     *ClusterStatus
	syncFn     func(conditions []metav1.Condition) (ctrl.Result, error)
	arguments  *argumentClusterState
	synced     bool
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *ClusterStateFinished) Sync() (ctrl.Result, error) {
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true

	result, err := s.syncFn(s.conditions())
	if err != nil {
		return ctrl.Result{}, err
	}
	return result, nil
}

func (s *ClusterStateFinished) conditions() []metav1.Condition {
	// final roll-up status determined here
	if err := errors.Join(
		s.status.Quiesced.MaybeError(),
		s.status.Ready.MaybeError(),
		s.status.ClusterResourcesSynced.MaybeError(),
		s.status.ClusterConfigurationApplied.MaybeError(),
	); err != nil {
		s.status.Stable.Unstable = fmt.Errorf("cluster not stable: %w", err)
	}
	return s.status.Conditions(s.generation)
}
