package log

import (
	"context"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	// "go.opentelemetry.io/otel/log/global"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Info(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Info(msg, keysAndValues...)
}

func Error(ctx context.Context, err error, msg string, keysAndValues ...any) {
	FromContext(ctx).Error(err, msg, keysAndValues...)
}

func FromContext(ctx context.Context, keysAndValues ...any) logr.Logger {
	// TODO might be better served by creating our own logger here??
	// TODO: Would be nice to get the calling package....
	keysAndValues = append(keysAndValues, "ctx", ctx)
	return logr.New(otellogr.NewLogSink("log")).WithValues(keysAndValues...)
}

func init() {
	log.SetLogger(logr.Discard()) // Silence the dramatic logger messages.
}
