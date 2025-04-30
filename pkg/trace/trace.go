package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var WithAttributes = trace.WithAttributes

func Tracer(name string) func() trace.Tracer {
	return sync.OnceValue(func() trace.Tracer {
		_, file, _, _ := runtime.Caller(2)
		fmt.Printf("TRACER CALLED WITH FILE: %s", file)
		return otel.GetTracerProvider().Tracer(name)
	})
}

func ForTest(t *testing.T) context.Context {
	ctx := context.Background()

	logexport, err := otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
	require.NoError(t, err)

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
	))
	require.NoError(t, err)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)),
	)

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logexport)),
	)

	log := logr.New(otellogr.NewLogSink("todo",
		otellogr.WithLoggerProvider(lp),
	))

	ctx, span := tp.Tracer("go-test").Start(ctx, t.Name())

	t.Cleanup(func() {
		span.End()
		require.NoError(t, exporter.Shutdown(ctx))
	})

	otel.SetTracerProvider(tp)
	// otel.SetLoggingProvider(lp)

	return ctrl.LoggerInto(ctx, log)
}

// OTLPFileClient is
type OTLPFileClient struct {
	file *os.File
	enc  *json.Encoder
}

func (c *OTLPFileClient) Start(ctx context.Context) error {
	var err error
	c.file, err = os.OpenFile("traces.jsonln", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0775)
	if err != nil {
		return err
	}

	c.enc = json.NewEncoder(c.file)
	return nil
}

func (c *OTLPFileClient) Stop(ctx context.Context) error {
	return c.file.Close()

}

func (c *OTLPFileClient) UploadTraces(ctx context.Context, spans []*tracepb.ResourceSpans) error {
	if err := c.enc.Encode(&tracepb.TracesData{ResourceSpans: spans}); err != nil {
		return err
	}

	return nil
}
