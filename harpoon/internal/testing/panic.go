// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
)

const godogFailMessage = "FailNow or SkipNow called"

type stringer interface {
	String() string
}

// WrapWithPanicHandler wraps an arbitrary function with panic handling that attempts
// to gracefully handle godog's wonky usage of panic when calls to godog.TestingT FailNow
// or SkipNow occur. This predominantly happens when you try and use standard go testing
// idioms like checking some condition and then calling t.Fatal, or t.FailNow. Internally
// testify's ubiquitous require library also leverages these functions. What this means is
// that if you have some code that needs to get run during cleanup time or want tests to
// continue running in the case of a requirement failure, stock godog cannot call either of
// these functions.
//
// This function attempts to mitigate this in a couple of ways:
// 1. Constructs a function wrapper via reflection that installs a panic/recover handler that
// looks for panics with the godog panic message
// 2. If "exitBehavior" is set to nothing, then just rescue and continue. This is primarily used
// when we know that godog is going to do an internal check on the failure status of a test
// immediately after us recovering, such as when a test step or before hook fails.
// 3. If "exitBehavior" is set to log, then just log the error that last occurred in our TestingT
// prior to the panic, because that is likely what is to have caused the panic.
// 4. If "exitBehavior" is set to exit, then call os.Exit after logging the last error.
// 5. If "exitBehavior" is set to fail, then propagate the last error up via a global channel
// that the top-level test runner receives, at the end of the test run, even if all of the tests pass
// then the overall test will fail.
//
// The first of the above behaviors, as mentioned, are used before or after an individual step function
// as we know godog will subsequently check the error status of the test. The last 3 need to be used in
// any after test hooks or calls to t.Cleanup due to the error check already having happened in godog, instead
// we need to bypass the godog stack and either fail the test directly or, in some other way, signal that
// something bad/unexpected happened.
func WrapWithPanicHandler[U any](needsLog bool, exitBehavior ExitBehavior, fn U) U {
	fnValue := reflect.ValueOf(fn)
	return reflect.MakeFunc(fnValue.Type(), func(args []reflect.Value) (results []reflect.Value) {
		runTestingFunc := func(fn func(t *TestingT)) {
			if len(args) > 0 {
				if ctx, ok := args[0].Interface().(context.Context); ok {
					t, ok := ctx.Value(testingContextKey).(*TestingT)
					if ok && t != nil {
						fn(t)
					}
				}
			}
		}

		defer func() {
			if err := recover(); err != nil {
				// swallow any panics from godog
				message, ok := isGodogLogPanic(err)
				if !ok {
					// otherwise re-panic
					panic(err)
				}
				runTestingFunc(func(t *TestingT) {
					if priorError := t.PriorError(); priorError != "" {
						message = priorError
					}
				})
				switch exitBehavior {
				case ExitBehaviorNone:
				case ExitBehaviorLog:
					fmt.Print(message)
				case ExitBehaviorTerminateProgram:
					fmt.Print(message)
					os.Exit(1)
				case ExitBehaviorTestFail:
					select {
					case TerminationChan <- TerminationError{
						Message:  message,
						NeedsLog: needsLog,
					}:
					default:
					}
				}
			}
			// We need to construct results here because if we rescue from a panic
			// but wind up returning nothing, then go freaks out saying we returned
			// the wrong number of return values from a function constructed with
			// reflection.
			//
			// Generally we can just construct an array of zero-values by type, but
			// we special-case having to return context.Context since you can't initialize
			// it directly, instead just passing back any unomdified context we found
			// as a first argument to the function handed to us.
			returnSize := fnValue.Type().NumOut()
			results = make([]reflect.Value, returnSize)
			for i := 0; i < returnSize; i++ {
				outType := fnValue.Type().Out(i)
				if len(args) > 0 && i == 0 && reflect.TypeFor[context.Context]() == outType {
					if ctx, ok := args[0].Interface().(context.Context); ok {
						results[i] = reflect.ValueOf(ctx)
						continue
					}
				}
				results[i] = reflect.New(outType).Elem()
			}
		}()
		results = fnValue.Call(args)
		return
	}).Interface().(U)
}

func isGodogLogPanic(s interface{}) (string, bool) {
	if errString, ok := s.(string); ok {
		if strings.HasPrefix(errString, godogFailMessage) {
			return errString, true
		}
	}

	if errStringer, ok := s.(stringer); ok {
		message := errStringer.String()
		if strings.HasPrefix(message, godogFailMessage) {
			return message, true
		}
	}

	if err, ok := s.(error); ok {
		message := err.Error()
		if strings.HasPrefix(message, godogFailMessage) {
			return message, true
		}
	}

	return "", false
}
