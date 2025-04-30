package trace

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	// sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var WithAttributes = trace.WithAttributes

func Start(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.GetTracerProvider().Tracer(callerPackage(0)).Start(ctx, name, options...)
}

func EndSpan(span trace.Span, err error, options ...trace.SpanEndOption) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End(options...)
}

func Test(t *testing.T) context.Context {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)

	ctx, span := otel.GetTracerProvider().Tracer(fileToPackage(file)).Start(context.Background(), t.Name())

	t.Cleanup(func() {
		if t.Failed() {
			span.SetStatus(codes.Error, "FAILED")
		} else {
			span.SetStatus(codes.Ok, "PASSED")
		}

		span.End()
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
