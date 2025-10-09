// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package trace

import (
	"context"
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

var WithAttributes = trace.WithAttributes

func Start(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.GetTracerProvider().Tracer(callerPackage(0)).Start(ctx, name, options...)
}

// EndSpan is a helper to end a span and annotate it with an error, if an error
// occurred.
//
// Usage:
//
//	func myTracedFunction(ctx context.Context) (err error) { // Named return values are required!
//		ctx, span := trace.Start(ctx, "myTracedFunction")
//		// The defer is called as such to ensure we get the returned error rather
//		// than capturing the value.
//		defer func() { trace.EndSpan(span, err) }()
func EndSpan(span trace.Span, err error, options ...trace.SpanEndOption) {
	// TODO should this function recover and re-panic panics?
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End(options...)
}

type TestingT interface {
	Name() string
	Helper()
	Failed() bool
	Cleanup(func())
	Log(args ...any)
}

// Test is a helper for generating a span around a test case and closing it
// when the test finishes. The returned [context.Context] MUST be used to
// propagate span information.
//
// NOTE: [otelutil.TestMain] MUST be called for this function to do anything.
//
// Usage:
//
//	func TestMyCode(t *testing.T) {
//		ctx := trace.Test(t)
//		// Test your code here
//	}
func Test(t TestingT) context.Context {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)

	// TODO upgrade to go 1.24 and use t.Context()
	ctx, cancel := context.WithCancel(context.Background())
	ctx, span := otel.GetTracerProvider().Tracer(fileToPackage(file)).Start(ctx, t.Name())

	// Place a multi-logger into the returned context so we get logs via go's
	// testing output and OTLP.
	// TODO: probably want to be be a bit more strict about the verbosity of
	// the testr to minimize noise in stdout?
	ctx = log.IntoContext(ctx, logr.New(&log.MultiSink{
		Sinks: []logr.LogSink{
			log.FromContext(ctx).GetSink(),
			log.ContextFree(testr.NewWithInterface(t, testr.Options{}).GetSink()),
		},
	}))

	t.Cleanup(func() {
		if t.Failed() {
			span.SetStatus(codes.Error, "FAILED")
		} else {
			span.SetStatus(codes.Ok, "PASSED")
		}

		span.End()
		cancel()
	})

	return ctx
}

func callerPackage(skip int) string {
	_, file, _, _ := runtime.Caller(skip + 1)
	return fileToPackage(file)
}

func fileToPackage(file string) string {
	pidx := strings.Index(file, "redpanda-operator/")

	fidx := strings.LastIndex(file, "/")

	if pidx == -1 || fidx == -1 {
		return file
	}

	return "github.com/redpanda-data/redpanda-operator/" + file[pidx:fidx]
}
