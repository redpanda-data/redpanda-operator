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

type status struct {
	Kind        string
	Description string
	States      *stateMachine
	Types       []*conditionType
}

func (s *status) HasStateMachine() bool {
	return s.States != nil
}

func (s *status) Comment() string {
	if s.Description == "" {
		return ""
	}
	return writeComment(s.GoName(), s.Description)
}

func (s *status) GoName() string {
	return fmt.Sprintf("%sStatus", s.Kind)
}

func (s *status) Transitions() []*stateTransition {
	if s.States == nil {
		return nil
	}
	return s.States.Transitions
}

func (s *status) InitialConditions() []*conditionType {
	conditionTypes := []*conditionType{}
	for _, condition := range s.States.InitialConditions {
		for i := range s.Types {
			conditionType := s.Types[i]
			if conditionType.Name == condition {
				conditionTypes = append(conditionTypes, conditionType)
			}
		}
	}
	return conditionTypes
}

func (s *status) normalize() {
	if s.States != nil {
		s.States.normalize(s)
	}
	for _, conditionType := range s.Types {
		conditionType.normalize(s)
	}
}

type conditionType struct {
	Name        string
	Description string
	Operation   string
	Ignore      bool
	Base        *reasonType
	Errors      []*reasonType

	kind string
}

func (c *conditionType) StructComment() string {
	if c.Description == "" {
		return ""
	}
	return writeComment(c.GoStructName(), c.Description)
}

func (c *conditionType) GoStructName() string {
	return fmt.Sprintf("%s%sStatus", c.kind, c.Name)
}

func (c *conditionType) ConditionComment() string {
	if c.Description == "" {
		return ""
	}
	return writeComment(c.GoConditionName(), c.Description)
}

func (c *conditionType) ConditionFuncComment() string {
	return writeComment("", fmt.Sprintf("Condition returns the status condition of the %s based off of the underlying errors that are set.", c.GoStructName()))
}

func (c *conditionType) GoConditionName() string {
	return fmt.Sprintf("%s%sCondition", c.kind, c.Name)
}

func (c *conditionType) NonStateErrorReasons() []*reasonType {
	reasons := []*reasonType{}

	for i := range c.Errors {
		reason := c.Errors[i]
		if !reason.isStateMachine {
			reasons = append(reasons, reason)
		}
	}

	return reasons
}

func (c *conditionType) normalize(status *status) {
	c.kind = status.Kind

	if c.Base == nil {
		c.Base = &reasonType{}
	}

	if c.Base.Name == "" {
		c.Base.Name = c.Name
	}
	if c.Base.Message == "" {
		c.Base.Message = c.Base.Name
	}

	found := false

	if status.States != nil {
	LOOP:
		for _, transition := range status.States.Transitions {
			for _, condition := range transition.Conditions {
				if condition == c.Name {
					found = true
					break LOOP
				}
			}
		}
	}

	if found {
		for _, reason := range status.States.TransitionReasons {
			c.Errors = append([]*reasonType{{
				Name:           reason.Name,
				Description:    reason.Description,
				isStateMachine: true,
			}}, c.Errors...)
		}
	}

	c.Base.normalize(status, c)
	for _, reason := range c.Errors {
		reason.normalize(status, c)
	}
}

func (c *conditionType) Reasons() []*reasonType {
	return c.Errors
}

type reasonType struct {
	Name        string
	Description string
	Message     string
	// to say whether this came from our state machine
	// configuration
	isStateMachine bool
	kind           string
	condition      *conditionType
}

func (r *reasonType) Comment() string {
	if r.Description == "" {
		return ""
	}
	return writeComment(r.GoName(), r.Description)
}

func (r *reasonType) GoName() string {
	return fmt.Sprintf("%s%sConditionReason%s", r.kind, r.condition.Name, r.Name)
}

func (r *reasonType) DefaultValue() string {
	return "False"
}

func (r *reasonType) normalize(status *status, condition *conditionType) {
	r.kind = status.Kind
	r.condition = condition
}
