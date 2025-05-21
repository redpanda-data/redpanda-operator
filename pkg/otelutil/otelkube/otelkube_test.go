package otelkube_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
)

func TestPropagation(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	// NB: In most cases, you'd rely on the globally set TextMapPropagator but
	// we don't want to mutate globals in a test so there's some additional
	// verbosity here.

	ctx := context.Background()
	var ns corev1.Namespace

	// Inject and Extract do nothing if there's no information to propagate.
	{
		otelkube.Inject(ctx, &ns, otelkube.WithTextMapPropagator(propagation.TraceContext{}))
		require.Equal(t, ns.GetAnnotations(), map[string]string{})

		ctx := otelkube.Extract(ctx, &ns, otelkube.WithTextMapPropagator(propagation.TraceContext{}))
		require.False(t, trace.SpanContextFromContext(ctx).IsValid())
	}

	// Start a span
	{
		ctx, span := tp.Tracer("otelkube_test").Start(ctx, t.Name())

		otelkube.Inject(ctx, &ns, otelkube.WithTextMapPropagator(propagation.TraceContext{}))
		require.Contains(t, ns.GetAnnotations(), "traceparent")

		// Span's won't be exactly that same as the extract one will
		// technically have a "remote" context.
		ctx = otelkube.Extract(ctx, &ns, otelkube.WithTextMapPropagator(propagation.TraceContext{}))
		require.Equal(t, trace.SpanContextFromContext(ctx).SpanID(), span.SpanContext().SpanID())
		require.Equal(t, trace.SpanContextFromContext(ctx).TraceID(), span.SpanContext().TraceID())
	}
}

func TestClient(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer(t.Name())
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx := context.Background()
	c := otelkube.NewClient(fake.NewFakeClient(), otelkube.WithTextMapPropagator(
		propagation.TraceContext{},
	))

	for _, tc := range []struct {
		name string
		op   func(context.Context, client.Object) error
	}{
		{
			name: "Create",
			op:   func(ctx context.Context, obj client.Object) error { return c.Create(ctx, obj) },
		},
		{
			name: "Update",
			op:   func(ctx context.Context, obj client.Object) error { return c.Update(ctx, obj) },
		},
		{
			name: "Patch",
			op:   func(ctx context.Context, obj client.Object) error { return c.Patch(ctx, obj, client.MergeFrom(obj)) },
		},
	} {
		ctx, span := tracer.Start(ctx, tc.name)

		// Perform some write operation
		require.NoError(t, tc.op(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Annotations: map[string]string{
					"not": "overwritten",
				},
			},
		}))

		// Assert that our trace information has propagated through it.
		var ns corev1.Namespace
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "test"}, &ns))
		require.Contains(t, ns.GetAnnotations(), "not")

		ctx = otelkube.Extract(ctx, &ns, otelkube.WithTextMapPropagator(
			propagation.TraceContext{},
		))

		require.Equal(t, trace.SpanContextFromContext(ctx).SpanID(), span.SpanContext().SpanID())
		require.Equal(t, trace.SpanContextFromContext(ctx).TraceID(), span.SpanContext().TraceID())
	}
}
