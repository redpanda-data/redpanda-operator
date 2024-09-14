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

func WrapWithPanicHandler[U any](prefix string, exitBehavior ExitBehavior, fn U) U {
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
					fmt.Print(prefix + message)
				case ExitBehaviorTerminateProgram:
					fmt.Print(prefix + message)
					os.Exit(1)
				case ExitBehaviorTestFail:
					TerminationChan <- prefix + message
				}
			}
			returnSize := fnValue.Type().NumOut()
			results = make([]reflect.Value, returnSize)
			for i := 0; i < returnSize; i++ {
				outType := fnValue.Type().Out(i)
				if len(args) > 0 && i == 0 && outType.Name() == "Context" {
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
