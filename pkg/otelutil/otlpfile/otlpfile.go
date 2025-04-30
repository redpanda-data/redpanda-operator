package otlpfile

import (
	"context"
	"io"
	"os"
	"sync"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	// logpb "go.opentelemetry.io/proto/otlp/log/v1"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	Path string

	mu           sync.Mutex
	file         *os.File
	jsonEncoder  ptrace.Marshaler
	protoDecoder ptrace.Unmarshaler
}

var _ otlptrace.Client = &Client{}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO should error if start called twice?

	var err error
	c.file, err = os.OpenFile(c.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0775)
	if err != nil {
		return err
	}

	c.jsonEncoder = &ptrace.JSONMarshaler{}
	c.protoDecoder = &ptrace.ProtoUnmarshaler{}

	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.file.Close()
}

func (c *Client) UploadTraces(ctx context.Context, spans []*tracepb.ResourceSpans) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// The OTLP file format is incompatible with the default JSON encoding of
	// the tracepb package:
	// - OTLP == camelCase vs tracepb == snake_case
	// - OTLP == bytes as hex vs tracepb == bytes as base64
	// https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding

	// The easiest way to serialize correctly is through the otel collector
	// package's which don't expose a mutable interface.
	// Therefore the only way to actually serialize traces correctly is to:
	// go -> proto -> ptrace -> json
	// In theory an otlp file exporter is on the way but the standard isn't
	// official yet so it'll be a while before there's an upstream package.

	protobytes, err := proto.Marshal(&tracepb.TracesData{ResourceSpans: spans})
	if err != nil {
		return err
	}

	traces, err := c.protoDecoder.UnmarshalTraces(protobytes)
	if err != nil {
		return err
	}

	data, err := c.jsonEncoder.MarshalTraces(traces)
	if err != nil {
		return err
	}

	data = append(data, '\n')

	n, err := c.file.Write(data)
	if err != nil {
		return err
	}

	if n != len(data) {
		return io.ErrShortWrite
	}

	return nil
}

type LogExporter struct {
	Path string

	mu           sync.Mutex
	file         *os.File
	jsonEncoder  plog.Marshaler
	protoDecoder plog.Unmarshaler
}

var _ sdklog.Exporter = &LogExporter{}

func (e *LogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 	protobytes, err := proto.Marshal(&tracepb.TracesData{ResourceSpans: spans})
	// 	if err != nil {
	// 		return err
	// 	}
	//
	// 	traces, err := c.protoDecoder.UnmarshalTraces(protobytes)
	// 	if err != nil {
	// 		return err
	// 	}
	//
	// 	data, err := c.jsonEncoder.MarshalTraces(traces)
	// 	if err != nil {
	// 		return err
	// 	}
	//
	// 	data = append(data, '\n')
	//
	// 	n, err := c.file.Write(data)
	// 	if err != nil {
	// 		return err
	// 	}
	//
	// 	if n != len(data) {
	// 		return io.ErrShortWrite
	// 	}
	//
	return nil
}

func (e *LogExporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.file.Close()
}

func (e *LogExporter) ForceFlush(ctx context.Context) error {
	return nil // TODO fsync here?
}
