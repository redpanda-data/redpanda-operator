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
			returnSize := fnValue.Type().NumOut()
			results = make([]reflect.Value, returnSize)
			for i := 0; i < returnSize; i++ {
				outType := fnValue.Type().Out(i)
				path := outType.PkgPath()
				name := outType.Name()
				if len(args) > 0 && i == 0 && path == "context" && name == "Context" {
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
