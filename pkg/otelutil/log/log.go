package log

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Info(ctx context.Context, msg string, keysAndValues ...any) {
	log.FromContext(ctx, "ctx", ctx).Info(msg, keysAndValues...)
}

func Error(ctx context.Context, err error, msg string, keysAndValues ...any) {
	log.FromContext(ctx, "ctx", ctx).Error(err, msg, keysAndValues...)
}
