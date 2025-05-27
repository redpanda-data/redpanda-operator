package otelutil

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
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
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// TestingM abstracts *testing.M so it can be passed into TestMain for setup.
type TestingM interface {
	Run() int
}

// TestMain is a helper for configuring telemetry for the tests of a given
// package. Once configured, logs and traces can be directed to either a file
// or gRPC endpoint via the OTLP_DIR and OTLP_GRPC environment variables,
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
//		otelutil.TestMain(m, "integration-some-package-here")
//	}
func TestMain(m TestingM, name string, onTypes ...testutil.TestType) {
	if len(onTypes) == 0 {
		// default to setting up logging on integration and unit tests if unspecified
		onTypes = []testutil.TestType{testutil.TestTypeUnit, testutil.TestTypeIntegration}
	}

	shouldSetupLogger := slices.Contains(onTypes, testutil.Type())
	if !shouldSetupLogger {
		os.Exit(m.Run())
	}

	normalizedName := name
	if len(onTypes) != 1 {
		normalizedName = testutil.Type().String() + "-" + name
	}

	cleanup, err := Setup(WithBinaryName(normalizedName))
	if err != nil {
		panic(err)
	}

	code := m.Run()

	_ = cleanup()

	os.Exit(code)
}

type config struct {
	serviceName     string
	name            string
	directory       string
	endpoint        string
	logLevel        log.TypedLevel
	metricsInterval time.Duration
}

func defaultConfig() *config {
	return &config{
		serviceName: "redpanda-operator",
		name:        filepath.Base(os.Args[0]),
		logLevel:    log.DefaultLevel,
	}
}

func (c *config) normalize(options ...Option) *config {
	c.fromEnv()

	for _, option := range options {
		*c = option(*c)
	}
	return c
}

func (c *config) fromEnv() {
	if level, ok := os.LookupEnv("LOG_LEVEL"); ok {
		c.logLevel = log.LevelFromString(level)
	}
	if endpoint, ok := os.LookupEnv("OTLP_GRPC"); ok {
		c.endpoint = endpoint
	}
	if directory, ok := os.LookupEnv("OTLP_DIR"); ok {
		c.directory = directory
	}
	if interval, ok := os.LookupEnv("OTLP_METRIC_INTERVAL"); ok {
		if parsed, err := time.ParseDuration(interval); err == nil {
			c.metricsInterval = parsed
		}
	}
}

func (c *config) hasFile() bool {
	return c.directory != ""
}

func (c *config) hasGRPC() bool {
	return c.endpoint != ""
}

func (c *config) file() string {
	return path.Join(c.directory, fmt.Sprintf("%s-%s.jsonnl", c.name, time.Now().Format(time.RFC3339)))
}

func (c *config) options() (loggerOptions []sdklog.LoggerProviderOption, tracerOptions []sdktrace.TracerProviderOption, err error) {
	if c.hasFile() {
		fmt.Printf("outputting traces to: %q\n", c.file())
		file, err := otlpfile.Open(c.file())
		if err != nil {
			return nil, nil, err
		}

		loggerOptions = append(loggerOptions, sdklog.WithProcessor(log.NewBatchProcessor(file.LogExporter(), c.logLevel.OTELLevel)))
		tracerOptions = append(tracerOptions, sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(file.SpanExporter())))
	}

	if c.hasGRPC() {
		ctx := context.Background()

		grpcLogExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(c.endpoint))
		if err != nil {
			return nil, nil, err
		}

		grpcSpanExporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(c.endpoint),
		))
		if err != nil {
			return nil, nil, err
		}

		loggerOptions = append(
			loggerOptions,
			sdklog.WithProcessor(log.NewBatchProcessor(grpcLogExporter, c.logLevel.OTELLevel)),
		)

		tracerOptions = append(
			tracerOptions,
			sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(grpcSpanExporter)),
		)
	}

	return loggerOptions, tracerOptions, nil
}

type Option func(c config) config

// WithServiceName sets the OTEL service name for logs, traces, and metrics.
func WithServiceName(name string) Option {
	return func(c config) config {
		c.serviceName = name
		return c
	}
}

// WithBinaryName sets the binary name used for file output naming.
func WithBinaryName(name string) Option {
	return func(c config) config {
		c.name = name
		return c
	}
}

// WithGRPCEndpoint sets the OTLP gRPC endpoint.
func WithGRPCEndpoint(endpoint string) Option {
	return func(c config) config {
		c.endpoint = endpoint
		return c
	}
}

// WithLogLevel sets the logging level.
func WithLogLevel(level log.TypedLevel) Option {
	return func(c config) config {
		c.logLevel = level
		return c
	}
}

// WithMetricsInterval sets the interval for metric collection/export.
func WithMetricsInterval(interval time.Duration) Option {
	return func(c config) config {
		c.metricsInterval = interval
		return c
	}
}

// WithTraceOutputDirectory sets the directory where logs, traces, and metrics are written.
func WithTraceOutputDirectory(name string) Option {
	return func(c config) config {
		c.directory = name
		return c
	}
}

// Setup configures logging, tracing, and metrics otel configurations, including
// setting the global instances of logging, tracing, and metrics providers.
// Setup should be called exactly once at startup.
//
// By default traces are ignored and logs are simply forwarded to stdout.
//
// If `OTLP_DIR` is set, traces and logs will be written to a jsonnl file in the
// specified directory. (Paths are relative to the binary's working dir)
//
// if `OTLP_GRPC` is set, traces and logs will be sent via the OTLP gRPC
// exporter to the specified endpoint.
//
// Configuration options can also be passed via With* helpers.
//
// See [config.fromEnv] for more details.
func Setup(options ...Option) (shutdown func() error, err error) {
	config := defaultConfig().normalize(options...)
	logOpts, traceOpts, err := config.options()
	if err != nil {
		return nil, err
	}

	serviceResource, err := resource.New(context.Background(), resource.WithAttributes(
		semconv.ServiceName(config.serviceName),
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
		logr.New(log.NewOTELLogrSink("log").WithTimestamps()).V(config.logLevel.LogrLevel),
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
