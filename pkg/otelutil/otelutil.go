package otelutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otlpfile"
)

type TestingM interface {
	Run() int
}

// TestMain is a helper for configuring telemetry for the tests of a given
// package. Once configured, logs and traces can be directed to either a file
// or gRPC endpoint with the OTLP_DIR and OTLP_GRPC environment variables,
// respectively.
//
// Traces can be viewed with any OTLP compatible tooling.
// [otel-tui](https://github.com/ymtdzzz/otel-tui/tree/main) is an excellent
// choice for local environments.
//
// Usage:
//
//	import (
//		"testing"
//
//		"github.com/redpanda-data/redpanda-operator/pkg/otelutil"
//	)
//
//	func TestMain(m *testing.M) {
//		otelutil.TestMain(m)
//	}
func TestMain(m TestingM) {
	cleanup, err := Setup()
	if err != nil {
		panic(err)
	}

	code := m.Run()

	_ = cleanup()

	os.Exit(code)
}

// Setup configures both logging and tracing otel configurations, including
// setting the global instances of tracing and logging providers. Setup should be called exactly once at startup
//
// By default traces are ignored and logs are simply forwarded to stdout.
//
// If `OTLP_DIR` is set, traces and logs will be written to a jsonnl file in the
// specified directory. (Paths are relative to the binary's working dir)
//
// if `OTLP_GRPC` is set, traces and logs will be sent via the OTLP gRPC
// exporter to the specified endpoint.
//
// See [optionsFromEnv] for more details.
func Setup() (shutdown func() error, err error) {
	logOpts, traceOpts, logLevel, err := optionsFromEnv()
	if err != nil {
		return nil, err
	}

	serviceResource, err := resource.New(context.Background(), resource.WithAttributes(
		semconv.ServiceName("redpanda-operator"),
	))
	if err != nil {
		return nil, err
	}
	baseResource, err := resource.Merge(
		resource.Default(),
		serviceResource,
	)
	if err != nil {
		return nil, err
	}
	logOpts = append(logOpts, sdklog.WithResource(baseResource))
	traceOpts = append(traceOpts, sdktrace.WithResource(baseResource))

	lp := sdklog.NewLoggerProvider(logOpts...)
	tp := sdktrace.NewTracerProvider(traceOpts...)

	otel.SetTracerProvider(tp)
	global.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.SetGlobals(
		logr.New(log.NewOTELLogrSink("log").WithTimestamps()).V(logLevel.LogrLevel),
	)

	return func() error {
		// This context controls how long lp and tp will attempt to shutdown
		// gracefully (e.g. flushing buffers / waiting to get ACKs from
		// remotes) before forcing a shutdown.
		// We may want to make this configurable in the future.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return errors.Join(
			lp.Shutdown(ctx),
			tp.Shutdown(ctx),
		)
	}, nil
}

func optionsFromEnv() (loggerOptions []sdklog.LoggerProviderOption, tracerOptions []sdktrace.TracerProviderOption, level log.TypedLevel, err error) {
	logLevel := log.DefaultLevel
	if level, ok := os.LookupEnv("LOG_LEVEL"); ok {
		logLevel = log.LevelFromString(level)
	}

	if dir, ok := os.LookupEnv("OTLP_DIR"); ok {
		// at this point the only thing that uses OTLP_DIR should be tests, which should all use `TestMain`, so the stack
		// level should be the 4th entry, this is brittle, but it'll work for now
		pkg := callerPackage(3)

		path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.jsonnl", binaryName(), pkg, time.Now().Format(time.RFC3339)))
		file, err := otlpfile.Open(path)
		if err != nil {
			return nil, nil, logLevel, err
		}

		loggerOptions = append(loggerOptions, sdklog.WithProcessor(log.NewBatchProcessor(file.LogExporter(), logLevel.OTELLevel)))
		tracerOptions = append(tracerOptions, sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(file.SpanExporter())))
	}

	if endpoint, ok := os.LookupEnv("OTLP_GRPC"); ok {
		ctx := context.Background()

		grpcLogExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(endpoint))
		if err != nil {
			return nil, nil, logLevel, err
		}

		grpcSpanExporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
		))
		if err != nil {
			return nil, nil, logLevel, err
		}

		loggerOptions = append(
			loggerOptions,
			sdklog.WithProcessor(log.NewBatchProcessor(grpcLogExporter, logLevel.OTELLevel)),
		)

		tracerOptions = append(
			tracerOptions,
			sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(grpcSpanExporter)),
		)
	}

	return loggerOptions, tracerOptions, logLevel, nil
}

func binaryName() string {
	return filepath.Base(os.Args[0])
}

func callerPackage(skip int) string {
	_, file, _, _ := runtime.Caller(skip + 1)
	return fileToPackage(file)
}

func fileToPackage(file string) string {
	pidx := strings.Index(file, "redpanda-operator/")
	fidx := strings.LastIndex(file, "/")
	return strings.ReplaceAll(strings.TrimPrefix(file[pidx:fidx], "redpanda-operator/"), "/", "__")
}
