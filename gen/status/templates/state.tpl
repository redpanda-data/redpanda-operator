// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ $.Package }}

// GENERATED from {{ $.File }}, DO NOT EDIT DIRECTLY

import (
	"errors"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RetryError can be used to explicitly requeue reconciliation
// early due to some known status condition that is not an "error"
// per-se, but a known point at which reconciliation needs to wait
// for some condition to be true.
type RetryError struct{ 
	message string
	after time.Duration
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
func RetryIn(message string, after time.Duration) RetryError { return RetryError{message: message, after: after} }

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

{{ range $status := $.Statuses }}
{{ if $status.HasStateMachine }}
var (
	{{- range $transition := $status.Transitions }}
	// {{ $transition.RetryErrorName }} is the error set on any condition that is derived in
	// a subsequent transition when a retryable error has occurred while {{ $transition.Action }}
	{{ $transition.RetryErrorName }} = errors.New("retrying after error {{ $transition.Action }}")
	{{- end }}

	{{- range $transition := $status.Transitions }}
	// {{ $transition.TerminalErrorName }} is the error set on any any condition that is derived in
	// a subsequent transition when a known terminal error has occurred while {{ $transition.Action }}
	{{ $transition.TerminalErrorName }} = errors.New("terminal error when {{ $transition.Action }}")
	{{- end }}

	{{- range $transition := $status.Transitions }}
	// {{ $transition.RequeueBeforeName }} is the message used to requeue a reconciliation
	// request before transitioning away from {{ $transition.Action }}
	{{ $transition.RequeueBeforeName }} = "requeueing before finishing {{ $transition.Action }}"
	{{- end }}
)

// {{ $status.Kind }}InitialInputOption is used to set any initial conditions for the status state.
type {{ $status.Kind }}InitialInputOption interface {
	applyInitialInput(status *{{ $status.Kind }}Status)
}

{{ range $conditionType := $status.InitialConditions }}
type option{{ $status.Kind }}{{ $conditionType.Name }}Condition struct{
	applyFn func(status *{{ $status.Kind }}Status)
}

func (o *option{{ $status.Kind }}{{ $conditionType.Name }}Condition) applyInitialInput(status *{{ $status.Kind }}Status) {
	o.applyFn(status)
}

	{{ range $reason := $conditionType.NonStateErrorReasons }}
// With{{ $status.Kind }}{{ $reason.Name }} is used to mark the underlying {{ $conditionType.Name }} with the {{ $reason.Name }} reason.
func With{{ $status.Kind }}{{ $reason.Name }}(message string) {{ $status.Kind }}InitialInputOption {
	return &option{{ $status.Kind }}{{ $conditionType.Name }}Condition{
		applyFn: func(status *{{ $status.Kind }}Status) {
			status.{{ $conditionType.Name }}.{{ $reason.Name }} = errors.New(message)
		},
	}
}
	{{ end }}
{{ end }}

{{ if eq (len $status.States.Arguments) 0 }}{{/* this is here to fix linter issues */}}
type argument{{ $status.Kind }}State struct {}
{{- else }}
type argument{{ $status.Kind }}State struct {
	{{- range $arg := $status.States.Arguments }}
	{{ $arg.Name }} {{ $arg.Type }}
	{{- end }}
}
{{- end }}

func defaultArgument{{ $status.Kind }}State() *argument{{ $status.Kind }}State {
	return &argument{{ $status.Kind }}State{
	{{- range $arg := $status.States.Arguments }}
	{{ $arg.Name }}: {{ if eq $arg.Type "string" }}"{{ $arg.Default }}"{{ else }}{{ $arg.Default }}{{ end }},
	{{- end }}
	}
}

{{/* State transitions */}}
{{- range $transition := $status.States.Transitions }}
// {{ $status.Kind }}State{{ $transition.Name }} represents a controller which is currently {{ $transition.Action }}
type {{ $status.Kind }}State{{ $transition.Name }} struct{
	generation int64
	arguments *argument{{ $status.Kind }}State
	syncFn func(conditions []metav1.Condition{{ range $arg := $status.States.Arguments }}, {{ $arg.Name }} {{ $arg.Type }}{{ end }}) (ctrl.Result, error)
	status *{{ $status.Kind }}Status
	transitioned bool
	synced bool
}

{{ range $arg := $transition.ProvidedArguments }}
// Set{{ title $arg.Name }} sets the value of "{{ $arg.Name }}" when it is passed to the underlying status synchronization function.
func (s *{{ $status.Kind }}State{{ $transition.Name }}) Set{{ title $arg.Name }}({{ $arg.Name }} {{ $arg.Type }}) *{{ $status.Kind }}State{{ $transition.Name }} {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot be used to set status state")
	}
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to set status state")
	}

	s.arguments.{{ $arg.Name }} = {{ $arg.Name }}
	return s
}
{{- end }}

func (s *{{ $status.Kind }}State{{ $transition.Name }}) conditions(errs ...error) []metav1.Condition {
	err := errors.Join(errs...)
	isRetry := errors.As(err, &RetryError{})
	isTerminal := errors.As(err, &TerminalError{})

	if isRetry {
	{{- range $conditionType := $transition.StatusConditions }}
	s.status.{{ $conditionType.Name }}.StillReconciling = fmt.Errorf("%s: %w", {{ $transition.RequeueBeforeName }}, err)
	{{- end }}

	{{- range $laterCondition := $transition.LaterTransitionConditions }}
		s.status.{{ $laterCondition.Name }}.StillReconciling = fmt.Errorf("%s: %w", {{ $transition.RequeueBeforeName }}, err)
	{{- end }}
		s.status.{{ $status.States.FinalCondition }}.StillReconciling = fmt.Errorf("%s: %w", {{ $transition.RequeueBeforeName }}, err)

		return s.status.Conditions(s.generation)
	}

	if isTerminal {
		// we only set terminal errors on non-final conditions, namely because
		// the errors are *terminal* - hence we won't retry.

	{{- range $conditionType := $transition.StatusConditions }}
		s.status.{{ $conditionType.Name }}.TerminalError = err
	{{- end }}

	{{- range $laterCondition := $transition.LaterTransitionConditions }}
		s.status.{{ $laterCondition.Name }}.TerminalError = {{ $transition.TerminalErrorName }}
	{{- end }}

		return s.status.Conditions(s.generation)
	}

	{{ range $conditionType := $transition.StatusConditions }}
	s.status.{{ $conditionType.Name }}.Error = err
	{{- end }}

	{{- range $laterCondition := $transition.LaterTransitionConditions }}
	s.status.{{ $laterCondition.Name }}.StillReconciling = {{ $transition.RetryErrorName }}
	{{- end }}
	s.status.{{ $status.States.FinalCondition }}.StillReconciling = {{ $transition.RetryErrorName }}

	return s.status.Conditions(s.generation)
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync can only ever be called once and cannot be called 
// if the state is transitioned away from with a TransitionTo* call. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *{{ $status.Kind }}State{{ $transition.Name }}) Sync(errs ...error) (ctrl.Result, error) {
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it should no longer be used for synchronization")
	}	
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true

	{{- if $status.States.Rollup }}
	// final roll-up is set here, there will be an error because we synchronized early in this case
	s.status.{{$status.States.Rollup.Condition}}.{{$status.States.Rollup.OnValidationFail.Reason}} = fmt.Errorf("{{ $status.States.Rollup.OnValidationFail.Message }}: %w", errors.Join({{- range $rollupCondition := $status.States.Rollup.CheckedConditions }}
		s.status.{{ $rollupCondition }}.MaybeError(),
	{{- end }}
	))
	{{- end }}

	result, err := s.syncFn(s.conditions(errs...){{ range $i, $arg := $status.States.Arguments }}, s.arguments.{{ $arg.Name }}{{ end }})
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

{{ if $transition.IsFinal }}
// Finish marks the state machine has having completed all of its processing.
func (s *{{ $status.Kind }}State{{ $transition.Name }}) Finish() *{{ $status.Kind }}StateFinished {	
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}	
	if s.synced {
		panic("states are meant to transition only before synchronization, this state is already marked as synced, so it cannot transition again")
	}

	s.transitioned = true
	return &{{ $status.Kind }}StateFinished{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}
{{ else }}
	{{ if $transition.IsInitial }}
// Initialize{{ $status.Kind }}State returns the initial state used for tracking reconciliation state, it takes a synchronization function
// called when statuses are to be fully synchronized with Kubernetes and a list of initial states to set by way of {{ $status.Kind }}InitialInputOption.
func Initialize{{ $status.Kind }}State(generation int64, syncFn func(conditions []metav1.Condition{{ range $arg := $status.States.Arguments }}, {{ $arg.Name }} {{ $arg.Type }}{{ end }}) (ctrl.Result, error), options ...{{ $status.Kind }}InitialInputOption) *{{ $status.Kind }}State{{ $transition.Name }} {
	status := New{{ $status.Kind }}()

	for _, option := range options {
		option.applyInitialInput(status)
	}

	return &{{ $status.Kind }}State{{ $transition.Name }}{generation: generation, status: status, syncFn: syncFn, arguments: defaultArgument{{ $status.Kind }}State()}
}
	{{- end }}

// TransitionTo{{ $transition.NextTransitionName }} returns the next state in the state machine, marking the current state as transitioned and no longer usable
func (s *{{ $status.Kind }}State{{ $transition.Name }}) TransitionTo{{ $transition.NextTransitionName }}() *{{ $status.Kind }}State{{ $transition.NextTransitionName }} {	
	if s.transitioned {
		panic("states are meant to transition linearly, this state is already marked as transitioned, so it cannot transition again")
	}	

	s.transitioned = true
	return &{{ $status.Kind }}State{{ $transition.NextTransitionName }}{generation: s.generation, status: s.status, syncFn: s.syncFn, arguments: s.arguments}
}
{{- end }}

{{ range $conditionType := $transition.StatusConditions }}
	{{- range $reason := $conditionType.NonStateErrorReasons }}
// {{ $reason.Name }} sets the  {{ $reason.Name }} reason on the {{ $conditionType.Name }} condition
func (s *{{ $status.Kind }}State{{ $transition.Name }}) {{ $reason.Name }}(message string) *{{ $status.Kind }}State{{ $transition.Name }} {	
	s.status.{{ $conditionType.Name }}.{{ $reason.Name }} = errors.New(message)
	return s
}
	{{- end }}
{{- end }}
{{- end }}{{/* END State transitions */}}

// {{ $status.Kind }}StateFinished represents a reconciliation loop that has finished all of its processing
type {{ $status.Kind }}StateFinished struct{
	generation int64
	status *{{ $status.Kind }}Status
	syncFn func(conditions []metav1.Condition{{ range $arg := $status.States.Arguments }}, {{ $arg.Name }} {{ $arg.Type }}{{ end }}) (ctrl.Result, error)
	arguments *argument{{ $status.Kind }}State
	synced bool
}

// Sync synchronizes the current state with the Kubernetes api. It wraps whatever synchronization closure
// was passed when initializing the state. Sync should only be called when you
// no longer want to process anymore in a reconciliation loop.
func (s *{{ $status.Kind }}StateFinished) Sync() (ctrl.Result, error) {
	if s.synced {
		panic("states are meant to synchronize statuses only once, this state is already marked as synced, so it cannot be used to sync again")
	}

	s.synced = true

	result, err := s.syncFn(s.conditions(){{ range $i, $arg := $status.States.Arguments }}, s.arguments.{{ $arg.Name }}{{ end }})
	if err != nil {
		return ctrl.Result{}, err
	}
	return result, nil
}

func (s *{{ $status.Kind }}StateFinished) conditions() []metav1.Condition {
	{{- if $status.States.Rollup }}
	// final roll-up status determined here
	if err := errors.Join({{- range $rollupCondition := $status.States.Rollup.CheckedConditions }}
		s.status.{{ $rollupCondition }}.MaybeError(),
	{{- end }}
	); err != nil {
		s.status.{{$status.States.Rollup.Condition}}.{{$status.States.Rollup.OnValidationFail.Reason}} = fmt.Errorf("{{ $status.States.Rollup.OnValidationFail.Message }}: %w", err)
	}
	{{- end }}
	return s.status.Conditions(s.generation)
}

{{ end }}
{{ end }}