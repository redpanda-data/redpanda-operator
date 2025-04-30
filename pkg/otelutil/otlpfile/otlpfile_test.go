package otlpfile_test

import (
	// "context"
	"os"
	"path/filepath"
	"testing"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otlpfile"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/stretchr/testify/require"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestMain(m *testing.M) {
	cleanup, err := otelutil.Setup()
	if err != nil {
		panic(err)
	}

	code := m.Run()

	cleanup()

	os.Exit(code)
}

func TestClient(t *testing.T) {
	ctx := trace.Test(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonln")

	client := otlpfile.Client{Path: path}

	require.NoError(t, client.Start(ctx))

	log.Info(ctx, "hello world!")

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
