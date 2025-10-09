// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package otlpfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otlpfile"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestClient(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonln")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o664)
	require.NoError(t, err)

	client := otlpfile.NewClient(file)

	require.NoError(t, client.Start(ctx))

	// Minimal data required to showcase that files are JSONNL and that they
	// follow the absurdly specific JSON format which is massively divergent
	// from go's JSON encoding of these spans.
	require.NoError(t, client.UploadTraces(ctx, []*tracepb.ResourceSpans{
		{
			ScopeSpans: []*tracepb.ScopeSpans{
				{
					Spans: []*tracepb.Span{
						{
							SpanId:  []byte{2, 2, 2, 2, 2, 2, 2, 2},
							TraceId: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
						},
					},
				},
			},
		},
	}))

	require.NoError(t, client.Stop(ctx))

	actual, err := os.ReadFile(path)
	require.NoError(t, err)

	testutil.AssertGolden(t, testutil.Text, "testdata/traces.jsonln.golden", actual)
}

func TestLogExporter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonln")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o664)
	require.NoError(t, err)

	client := otlpfile.NewLogExporter(file)

	var record sdklog.Record
	record.SetBody(log.StringValue("Hello world!"))
	record.SetSpanID(trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2})
	record.SetTraceID(trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})

	// Minimal data required to showcase that files are JSONNL and that they
	// follow the absurdly specific JSON format which is massively divergent
	// from go's JSON encoding of these spans.
	require.NoError(t, client.Export(ctx, []sdklog.Record{record}))

	require.NoError(t, client.Shutdown(ctx))

	actual, err := os.ReadFile(path)
	require.NoError(t, err)

	testutil.AssertGolden(t, testutil.Text, "testdata/logs.jsonln.golden", actual)
}
