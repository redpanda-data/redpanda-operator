package otelutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otlpfile"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	// tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type TestingM interface {
	Run() int
}

func TestMain(m TestingM) {
	cleanup, err := Setup()
	if err != nil {
		panic(err)
	}

	code := m.Run()

	_ = cleanup()

	os.Exit(code)
}

func Setup() (func() error, error) {
	ctx := context.Background()

	tp, err := SetupTracing(ctx)
	if err != nil {
		return nil, err
	}

	lp, err := SetupLogging(ctx)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	global.SetLoggerProvider(lp)

	log.SetLogger(
		logr.New(
			otellogr.NewLogSink(
				"todo",
				otellogr.WithLoggerProvider(lp),
			),
		),
	)

	return func() error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		return errors.Join(
			lp.Shutdown(ctx),
			tp.Shutdown(ctx),
		)
	}, nil
}

type foo struct {
}

func (f *foo) OnEmit(ctx context.Context, record *sdklog.Record) error {
	return nil
}

func (f *foo) Shutdown(ctx context.Context) error {
	return nil
}
func (f *foo) ForceFlush(ctx context.Context) error {
	return nil
}

func SetupLogging(ctx context.Context) (*sdklog.LoggerProvider, error) {
	logexport, err := otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	lp := sdklog.NewLoggerProvider(
		// sdklog.WithProcessor(&foo{}),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logexport)),
	)

	return lp, nil
}

func SetupTracing(ctx context.Context) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	}

	if endpoint, ok := os.LookupEnv("OTLP_GRPC"); ok {
		grpcExporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		))
		if err != nil {
			return nil, err
		}

		options = append(
			options,
			sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(grpcExporter)),
		)
	}

	if dir, ok := os.LookupEnv("OTLP_DIR"); ok {
		file := filepath.Join(dir, fmt.Sprintf("traces-%d.jsonnl", time.Now().Unix()))
		fileExporter, err := otlptrace.New(ctx, &otlpfile.Client{Path: file})
		if err != nil {
			return nil, err
		}

		options = append(
			options,
			sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(fileExporter)),
		)
	}

	return sdktrace.NewTracerProvider(options...), nil
}
