package timing

import (
	"io"

	"github.com/go-logr/logr"
	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type timingOnlyCore struct {
	level int8
	zapcore.Core
}

func (t *timingOnlyCore) With(field []zapcore.Field) zapcore.Core {
	return &timingOnlyCore{
		Core:  t.Core.With(field),
		level: t.level,
	}
}

func (t *timingOnlyCore) Enabled(level zapcore.Level) bool {
	return int8(level) == t.level
}

func (t *timingOnlyCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if t.Enabled(ent.Level) {
		return ce.AddCore(ent, t)
	}
	return ce
}

// SetupTimingOnlyLogger changes the logging paradigm
// for our controller such that only lines instrumented
// with timing.Execution(ctx).Stop(...) will be output
// this is particularly useful when you want to get
// a set of timings for profiling how long is spent in
// different parts of an execution trace, primarily
// during local development.
func SetupTimingOnlyLogger() {
	logger := zapLogger(nil)
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)
}

func zapLogger(output io.Writer) logr.Logger {
	return zap.New(zap.JSONEncoder([]zap.EncoderConfigOption{
		func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
			config.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
				enc.AppendString("timing")
			}
		},
	}...), func(o *zap.Options) {
		o.ZapOpts = append(o.ZapOpts, uzap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return &timingOnlyCore{Core: core, level: -10}
		}))
		o.DestWriter = output
	})
}
