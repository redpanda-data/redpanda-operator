// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

import (
	"fmt"
	"path/filepath"
	"strings"
)

type status struct {
	Kind                   string
	AppliesTo              []string
	Description            string
	Errors                 []*errorType
	DefaultConditionReason *reasonType
	Conditions             []*conditionType

	basePkg string
}

func (s *status) Comment() string {
	if s.Description == "" {
		return ""
	}
	return writeComment(s.GoName(), s.Description)
}

func (s *status) Imports() []string {
	applies := []string{}
	appliesMap := map[string]struct{}{}
	for _, appliesTo := range s.AppliesTo {
		appliesMap[filepath.Dir(appliesTo)] = struct{}{}
	}

	for appliesTo := range appliesMap {
		alias := strings.Join(strings.Split(appliesTo, "/"), "")
		applies = append(applies, fmt.Sprintf("%s %q", alias, s.basePkg+"/"+appliesTo))
	}

	return applies
}

func (s *status) ImportedKinds() []string {
	impts := []string{}
	for _, appliesTo := range s.AppliesTo {
		impt := strings.Join(strings.SplitN(appliesTo, "/", 2), "")
		impt = "*" + strings.Join(strings.SplitN(impt, "/", 2), ".")
		impts = append(impts, impt)
	}

	return impts
}

func (s *status) GoName() string {
	return fmt.Sprintf("%sStatus", s.Kind)
}

func (s *status) DefaultStatusComment() string {
	defaultStatuses := []string{}
	for _, condition := range s.Conditions {
		defaultStatuses = append(defaultStatuses, condition.defaultStatus())
	}
	return fmt.Sprintf("// +kubebuilder:default={conditions: {%s}}", strings.Join(defaultStatuses, ", "))
}

func (s *status) ManualConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, condition := range s.Conditions {
		if !condition.IsCalculated() {
			conditions = append(conditions, condition)
		}
	}

	return conditions
}

func (s *status) FinalConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, condition := range s.Conditions {
		if condition.Final {
			conditions = append(conditions, condition)
		}
	}

	return conditions
}

func (s *status) RollupConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, condition := range s.Conditions {
		if len(condition.Rollup) > 0 {
			conditions = append(conditions, condition)
		}
	}

	return conditions
}

func (s *status) HasFinalConditions() bool {
	return len(s.FinalConditions()) > 0
}

func (s *status) HasRollupConditions() bool {
	return len(s.RollupConditions()) > 0
}

func (s *status) HasTransientError() bool {
	return len(s.TransientErrorConditions()) > 0
}

func (s *status) HasTerminalError() bool {
	return len(s.TerminalErrorConditions()) > 0
}

func (s *status) TransientErrors() []*errorType {
	errs := []*errorType{}
	for _, err := range s.Errors {
		if err.Transient {
			errs = append(errs, err)
		}
	}
	return errs
}

func (s *status) TerminalErrors() []*errorType {
	errs := []*errorType{}
	for _, err := range s.Errors {
		if !err.Transient {
			errs = append(errs, err)
		}
	}
	return errs
}

func (s *status) TransientErrorConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, condition := range s.Conditions {
		if condition.HasTransientError() {
			conditions = append(conditions, condition)
		}
	}

	return conditions
}

func (s *status) TerminalErrorConditions() []*conditionType {
	conditions := []*conditionType{}

	for _, condition := range s.Conditions {
		if condition.HasTerminalError() {
			conditions = append(conditions, condition)
		}
	}

	return conditions
}

func (s *status) normalize(basePkg string) {
	if s.DefaultConditionReason == nil {
		s.DefaultConditionReason = &reasonType{
			Name:    "NotReconciled",
			Message: "Waiting for controller",
		}
	}

	s.basePkg = basePkg

	for _, conditionType := range s.Conditions {
		conditionType.normalize(s)
	}
}

type printerFormat struct {
	Name        string
	Description string
	Message     bool

	condition *conditionType
}

func (p *printerFormat) Comment() string {
	field := "status"
	if p.Message {
		field = "message"
	}
	return fmt.Sprintf(
		`// +kubebuilder:printcolumn:name="%s",type="string",JSONPath=".status.conditions[?(@.type==\"%s\")].%s",description="%s"`,
		p.Name, p.condition.Name, field, p.Description,
	)
}

func (p *printerFormat) normalize(condition *conditionType) {
	p.condition = condition
}

type conditionType struct {
	Name           string
	Description    string
	PrinterColumns []*printerFormat
	Reasons        []*reasonType
	Final          bool
	Rollup         []string
	RateLimit      string

	kind               string
	defaultReason      *reasonType
	rolledUpConditions []*conditionType
}

func (c *conditionType) IsCalculated() bool {
	return len(c.Rollup) > 0 || c.Final
}

func (c *conditionType) Comment() string {
	if c.Description == "" {
		return ""
	}
	return writeComment(c.GoName(), c.Description)
}

func (c *conditionType) GoName() string {
	return fmt.Sprintf("%s%s", c.kind, c.Name)
}

func (c *conditionType) ConditionComment() string {
	if c.Description == "" {
		return ""
	}
	return writeComment(c.GoConditionName(), c.Description)
}

func (c *conditionType) GoConditionName() string {
	return fmt.Sprintf("%s%sCondition", c.kind, c.Name)
}

func (c *conditionType) HasTransientError() bool {
	for _, reason := range c.Reasons {
		if reason.isTransientError {
			return true
		}
	}
	return false
}

func (c *conditionType) HasRateLimit() bool {
	return c.RateLimit != ""
}

func (c *conditionType) HasTerminalError() bool {
	for _, reason := range c.Reasons {
		if reason.isTerminalError {
			return true
		}
	}
	return false
}

func (c *conditionType) TransientErrorReasonNamed(name string) string {
	for _, reason := range c.Reasons {
		if reason.isTransientError && reason.Name == name {
			return reason.GoName()
		}
	}
	return ""
}

func (c *conditionType) TerminalErrorReasonNamed(name string) string {
	for _, reason := range c.Reasons {
		if reason.isTerminalError && reason.Name == name {
			return reason.GoName()
		}
	}
	return ""
}

func (c *conditionType) RollupConditions() []*conditionType {
	return c.rolledUpConditions
}

func (c *conditionType) defaultStatus() string {
	return fmt.Sprintf(`{type: %q, status: "Unknown", reason: %q, message: %q, lastTransitionTime: "1970-01-01T00:00:00Z"}`, c.Name, c.defaultReason.Name, c.defaultReason.Message)
}

func (c *conditionType) normalize(status *status) {
	c.kind = status.Kind
	c.defaultReason = status.DefaultConditionReason

	if c.Final && len(c.Reasons) != 2 {
		panic("Final conditions must be boolean in nature and have exactly two reasons")
	}
	if len(c.Rollup) != 0 && len(c.Reasons) != 2 {
		panic("Rollup conditions must be boolean in nature and have exactly two reasons")
	}

	if !c.Final && len(c.Rollup) == 0 {
		for _, err := range status.Errors {
			// make copies here
			c.Reasons = append(c.Reasons, &reasonType{
				Name:             err.Name,
				Description:      err.Description,
				isTransientError: err.Transient,
				isTerminalError:  !err.Transient,
			})
		}
	}

	for _, rollup := range c.Rollup {
		found := false
		for _, condition := range status.Conditions {
			if condition.Name == rollup {
				found = true
				c.rolledUpConditions = append(c.rolledUpConditions, condition)
				break
			}
		}
		if !found {
			panic("Rollup relies on a condition that doesn't exist: " + rollup)
		}
	}

	for _, reason := range c.Reasons {
		reason.normalize(status, c)
	}

	for _, column := range c.PrinterColumns {
		column.normalize(c)
	}
}

type reasonType struct {
	Name             string
	Description      string
	Message          string
	kind             string
	condition        *conditionType
	isTransientError bool
	isTerminalError  bool
}

func (r *reasonType) Comment() string {
	if r.Description == "" {
		return ""
	}
	return writeComment(r.GoName(), r.Description)
}

func (r *reasonType) GoName() string {
	return fmt.Sprintf("%s%sReason%s", r.kind, r.condition.Name, r.Name)
}

func (r *reasonType) DefaultValue() string {
	return "False"
}

func (r *reasonType) IsTransientError() bool {
	return r.isTransientError
}

func (r *reasonType) IsTerminalError() bool {
	return r.isTerminalError
}

func (r *reasonType) normalize(status *status, condition *conditionType) {
	r.kind = status.Kind
	r.condition = condition
}

type errorType struct {
	Name        string
	Description string
	Transient   bool
}
