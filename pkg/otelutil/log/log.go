package log

import (
	"context"
	"log/slog"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Info(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Info(msg, keysAndValues...)
}

func Error(ctx context.Context, err error, msg string, keysAndValues ...any) {
	FromContext(ctx).Error(err, msg, keysAndValues...)
}

// IntoContext takes a context and sets the logger as one of its values.
// Use FromContext function to retrieve the logger.
var IntoContext = log.IntoContext

// FromContext returns a logger with predefined values from a context.Context.
func FromContext(ctx context.Context, keysAndValues ...any) logr.Logger {
	keysAndValues = append(keysAndValues, "ctx", ctx)
	return log.FromContext(ctx, keysAndValues...)
}

// SetGlobals sets the global [logr.Logger] instance for this package and all
// logging libraries that may be used by 3rd party dependencies.
func SetGlobals(l logr.Logger) {
	// TODO set "log" global?
	log.SetLogger(l)
	klog.SetLogger(l)
	slog.SetDefault(
		slog.New(logr.ToSlogHandler(l)),
	)
}

// MultiSink is a [logr.LogSink] that delegates to one or more other
// [logr.LogSink]s.
type MultiSink struct {
	Sinks []logr.LogSink
}

var _ logr.LogSink = &MultiSink{}

func (s *MultiSink) Init(info logr.RuntimeInfo) {
	for _, sink := range s.Sinks {
		sink.Init(info)
	}
}

func (s *MultiSink) Enabled(level int) bool {
	for _, sink := range s.Sinks {
		if sink.Enabled(level) {
			return true
		}
	}
	return false
}

func (s *MultiSink) Info(level int, msg string, keysAndValues ...any) {
	for _, sink := range s.Sinks {
		sink.Info(level, msg, keysAndValues...)
	}
}

func (s *MultiSink) Error(err error, msg string, keysAndValues ...any) {
	for _, sink := range s.Sinks {
		sink.Error(err, msg, keysAndValues...)
	}
}

func (s *MultiSink) WithValues(keysAndValues ...any) logr.LogSink {
	sinks := make([]logr.LogSink, len(s.Sinks))
	for i, sink := range s.Sinks {
		sinks[i] = sink.WithValues(keysAndValues...)
	}
	return &MultiSink{Sinks: sinks}
}

func (s *MultiSink) WithName(name string) logr.LogSink {
	sinks := make([]logr.LogSink, len(s.Sinks))
	for i, sink := range s.Sinks {
		sinks[i] = sink.WithName(name)
	}
	return &MultiSink{Sinks: sinks}
}
