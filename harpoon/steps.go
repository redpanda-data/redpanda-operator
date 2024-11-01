// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"context"
	"reflect"

	"github.com/cucumber/godog"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
)

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
		ctx.Step(step.expression, injectTestingT(internaltesting.WrapWithPanicHandler(false, internaltesting.ExitBehaviorNone, step.step)))
	}
}

func injectTestingT(fn interface{}) interface{} {
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()
	inTypes := []reflect.Type{}
	for i := 0; i < fnType.NumIn(); i++ {
		inTypes = append(inTypes, fnType.In(i))
	}
	outTypes := []reflect.Type{}
	for i := 0; i < fnType.NumOut(); i++ {
		outTypes = append(outTypes, fnType.Out(i))
	}

	// handle the case of func(ctx.Context, TestingT, ...)
	if len(inTypes) > 1 && reflect.TypeFor[context.Context]() == inTypes[0] && reflect.TypeFor[TestingT]() == inTypes[1] {
		inTypes = append([]reflect.Type{inTypes[0]}, inTypes[2:]...)
		fnType := reflect.FuncOf(inTypes, outTypes, false)
		newFn := reflect.MakeFunc(fnType, func(args []reflect.Value) (results []reflect.Value) {
			ctx := args[0].Interface().(context.Context)
			t := T(ctx)
			newArgs := append([]reflect.Value{args[0], reflect.ValueOf(t)}, args[1:]...)
			return fnValue.Call(newArgs)
		}).Interface()
		return newFn
	}

	// handle the case of func(TestingT, ...)
	if len(inTypes) > 0 && reflect.TypeFor[TestingT]() == inTypes[0] {
		// inject a ctx.Context, pull TestingT out, and then drop the context
		inTypes = append([]reflect.Type{reflect.TypeFor[context.Context]()}, inTypes[1:]...)
		fnType := reflect.FuncOf(inTypes, outTypes, false)
		newFn := reflect.MakeFunc(fnType, func(args []reflect.Value) (results []reflect.Value) {
			ctx := args[0].Interface().(context.Context)
			t := T(ctx)
			newArgs := append([]reflect.Value{reflect.ValueOf(t)}, args[1:]...)
			return fnValue.Call(newArgs)
		}).Interface()
		return newFn
	}

	return fn
}
