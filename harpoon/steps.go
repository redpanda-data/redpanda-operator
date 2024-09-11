// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import "github.com/cucumber/godog"

type stepDefinition struct {
	expression string
	step       interface{}
}

var registeredSteps []stepDefinition

func RegisterStep(expression string, step interface{}) {
	registeredSteps = append(registeredSteps, stepDefinition{expression, step})
}

func getSteps(ctx *godog.ScenarioContext) {
	for _, step := range registeredSteps {
		ctx.Step(step.expression, step.step)
	}
}
