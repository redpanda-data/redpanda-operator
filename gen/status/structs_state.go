// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

import "fmt"

type stateMachine struct {
	InitialConditions []string
	TransitionReasons []stateTransitionReasons
	FinalCondition    string
	Arguments         []stateArgument
	Rollup            *stateRollup
	Transitions       []*stateTransition
}

func (s *stateMachine) normalize(status *status) {
	for _, transition := range s.Transitions {
		transition.normalize(status)
	}
}

type stateTransitionReasons struct {
	Name        string
	Description string
}

type stateRollup struct {
	Condition         string
	CheckedConditions []string
	OnValidationFail  stateRollupFail
}

type stateArgument struct {
	Name    string
	Type    string
	Default string
}

type stateRollupFail struct {
	Reason  string
	Message string
}

type stateTransition struct {
	Name        string
	Action      string
	Provides    []string
	Description string
	Conditions  []string

	isInitial          bool
	isFinal            bool
	nextTransitionName string
	nextTransition     *stateTransition
	laterTransitions   []*stateTransition
	conditions         []*conditionType
	provides           []stateArgument
	kind               string
}

func (s *stateTransition) normalize(status *status) {
	s.kind = status.Kind
	for _, arg := range s.Provides {
		for _, stateArg := range status.States.Arguments {
			if arg == stateArg.Name {
				s.provides = append(s.provides, stateArg)
			}
		}
	}

	for _, cond := range s.Conditions {
		for _, condition := range status.Types {
			if cond == condition.Name {
				s.conditions = append(s.conditions, condition)
			}
		}
	}

	before := true
	for index, transition := range status.States.Transitions {
		if transition.Name == s.Name {
			if index == 0 {
				s.isInitial = true
			}
			before = false
			continue
		}
		if !before {
			if s.nextTransitionName == "" {
				s.nextTransition = status.States.Transitions[index]
				s.nextTransitionName = transition.Name
			}
			s.laterTransitions = append(s.laterTransitions, transition)
		}
	}

	if s.nextTransitionName == "" {
		s.isFinal = true
		s.nextTransitionName = "Final"
	}
}

func (s *stateTransition) RequeueBeforeName() string {
	return fmt.Sprintf("%sStateRequeueBefore%sMessage", s.kind, s.Name)
}

func (s *stateTransition) RetryErrorName() string {
	return fmt.Sprintf("Err%sState%sRetryable", s.kind, s.Name)
}

func (s *stateTransition) TerminalErrorName() string {
	return fmt.Sprintf("Err%sState%sTerminal", s.kind, s.Name)
}

func (s *stateTransition) StatusConditions() []*conditionType {
	return s.conditions
}

func (s *stateTransition) ProvidedArguments() []stateArgument {
	return s.provides
}

func (s *stateTransition) NextTransitionName() string {
	return s.nextTransitionName
}

func (s *stateTransition) NextTransition() *stateTransition {
	return s.nextTransition
}

func (s *stateTransition) LaterTransitions() []*stateTransition {
	return s.laterTransitions
}

func (s *stateTransition) IsFinal() bool {
	return s.isFinal
}

func (s *stateTransition) IsInitial() bool {
	return s.isInitial
}

func (s *stateTransition) LaterTransitionConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, transition := range s.laterTransitions {
		conditions = append(conditions, transition.conditions...)
	}

	return conditions
}
