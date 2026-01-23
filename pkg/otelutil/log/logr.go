// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package log

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// Most of this is heavily modified from:
// - https://github.com/open-telemetry/opentelemetry-go-contrib/blob/a91d74714434fcf5b54c577036df09e9e1b660e4/bridges/otellogr/logsink.go
// - https://github.com/open-telemetry/opentelemetry-go-contrib/blob/a91d74714434fcf5b54c577036df09e9e1b660e4/bridges/otellogr/convert.go

// NewOTELLogrSink returns a new [LogSink] to be used as a [logr.LogSink].
func NewOTELLogrSink(name string, options ...log.LoggerOption) *LogSink {
	return &LogSink{
		name:     name,
		provider: global.GetLoggerProvider(),
		logger:   global.GetLoggerProvider().Logger(name, options...),
		opts:     options,
		ctx:      context.Background(),
	}
}

// LogSink is a [logr.LogSink] that sends all logging records it receives to
// OpenTelemetry. See package documentation for how conversions are made.
type LogSink struct {
	// Ensure forward compatibility by explicitly making this not comparable.
	noCmp [0]func() //nolint: unused  // This is indeed used.

	name          string
	provider      log.LoggerProvider
	logger        log.Logger
	useTimestamps bool
	opts          []log.LoggerOption
	attr          []log.KeyValue
	ctx           context.Context
}

// Compile-time check *Handler implements logr.LogSink.
var _ logr.LogSink = (*LogSink)(nil)

// Enabled tests whether this LogSink is enabled at the specified V-level.
// For example, commandline flags might be used to set the logging
// verbosity and disable some info logs.
func (l *LogSink) Enabled(level int) bool {
	ctx := context.Background()
	param := log.EnabledParameters{Severity: LevelFromLogr(level).OTELLevel}
	return l.logger.Enabled(ctx, param)
}

// Error logs an error, with the given message and key/value pairs.
func (l *LogSink) Error(err error, msg string, keysAndValues ...any) {
	var record log.Record
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityError)
	record.SetSeverityText("error")

	if err != nil {
		record.AddAttributes(
			log.String(string(semconv.ExceptionMessageKey), err.Error()),
		)
	}

	if l.useTimestamps {
		record.SetTimestamp(time.Now())
	}

	record.AddAttributes(l.attr...)

	ctx, attr := convertKVs(l.ctx, keysAndValues...)
	record.AddAttributes(attr...)

	l.logger.Emit(ctx, record)
}

// Info logs a non-error message with the given key/value pairs.
func (l *LogSink) Info(level int, msg string, keysAndValues ...any) {
	var record log.Record
	record.SetBody(log.StringValue(msg))

	typedLevel := LevelFromLogr(level)
	record.SetSeverity(typedLevel.OTELLevel)
	record.SetSeverityText(typedLevel.Name)

	record.AddAttributes(l.attr...)

	if l.useTimestamps {
		record.SetTimestamp(time.Now())
	}

	ctx, attr := convertKVs(l.ctx, keysAndValues...)
	record.AddAttributes(attr...)

	l.logger.Emit(ctx, record)
}

// Init receives optional information about the logr library this
// implementation does not use it.
func (l *LogSink) Init(logr.RuntimeInfo) {
	// We don't need to do anything here.
	// CallDepth is used to calculate the caller's PC.
	// PC is dropped as part of the conversion to the OpenTelemetry log.Record.
}

// WithName returns a new LogSink with the specified name appended.
func (l LogSink) WithName(name string) logr.LogSink {
	l.name = l.name + "/" + name
	l.logger = l.provider.Logger(l.name, l.opts...)
	return &l
}

// WithTimestamps uses timestamps on each log record
func (l *LogSink) WithTimestamps() *LogSink {
	l.useTimestamps = true
	return l
}

// WithValues returns a new LogSink with additional key/value pairs.
func (l LogSink) WithValues(keysAndValues ...any) logr.LogSink {
	ctx, attr := convertKVs(l.ctx, keysAndValues...)
	l.attr = append(l.attr, attr...)
	l.ctx = ctx
	return &l
}

// convertKVs converts a list of key-value pairs to a list of [log.KeyValue].
// The last [context.Context] value is returned as the context.
// If no context is found, the original context is returned.
func convertKVs(ctx context.Context, keysAndValues ...any) (context.Context, []log.KeyValue) {
	if len(keysAndValues) == 0 {
		return ctx, nil
	}
	if len(keysAndValues)%2 != 0 {
		// Ensure an odd number of items here does not corrupt the list.
		keysAndValues = append(keysAndValues, nil)
	}

	kvs := make([]log.KeyValue, 0, len(keysAndValues)/2)
	for i := 0; i < len(keysAndValues); i += 2 {
		k, ok := keysAndValues[i].(string)
		if !ok {
			// Ensure that the key is a string.
			k = fmt.Sprintf("%v", keysAndValues[i])
		}

		v := keysAndValues[i+1]
		if vCtx, ok := v.(context.Context); ok {
			// Special case when a field is of context.Context type.
			ctx = vCtx
			continue
		}

		kvs = append(kvs, log.KeyValue{
			Key:   k,
			Value: convertValue(v),
		})
	}

	return ctx, kvs
}

type stringer interface {
	String() string
}

// convertValue converts various types to log.Value.
func convertValue(v any) log.Value {
	// Handling the most common types without reflect is a small perf win.
	switch val := v.(type) {
	case bool:
		return log.BoolValue(val)
	case string:
		return log.StringValue(val)
	case int:
		return log.Int64Value(int64(val))
	case int8:
		return log.Int64Value(int64(val))
	case int16:
		return log.Int64Value(int64(val))
	case int32:
		return log.Int64Value(int64(val))
	case int64:
		return log.Int64Value(val)
	case uint:
		return convertUintValue(uint64(val))
	case uint8:
		return log.Int64Value(int64(val))
	case uint16:
		return log.Int64Value(int64(val))
	case uint32:
		return log.Int64Value(int64(val))
	case uint64:
		return convertUintValue(val)
	case uintptr:
		return convertUintValue(uint64(val))
	case float32:
		return log.Float64Value(float64(val))
	case float64:
		return log.Float64Value(val)
	case time.Duration:
		return log.Int64Value(val.Nanoseconds())
	case complex64:
		r := log.Float64("r", real(complex128(val)))
		i := log.Float64("i", imag(complex128(val)))
		return log.MapValue(r, i)
	case complex128:
		r := log.Float64("r", real(val))
		i := log.Float64("i", imag(val))
		return log.MapValue(r, i)
	case time.Time:
		return log.Int64Value(val.UnixNano())
	case []byte:
		return log.BytesValue(val)
	case error:
		return log.StringValue(val.Error())
	}

	t := reflect.TypeOf(v)
	if t == nil {
		return log.Value{}
	}
	val := reflect.ValueOf(v)
	switch t.Kind() {
	case reflect.Struct:
		return log.StringValue(fmt.Sprintf("%+v", v))
	case reflect.Slice, reflect.Array:
		items := make([]log.Value, 0, val.Len())
		for i := 0; i < val.Len(); i++ {
			items = append(items, convertValue(val.Index(i).Interface()))
		}
		return log.SliceValue(items...)
	case reflect.Map:
		kvs := make([]log.KeyValue, 0, val.Len())
		for _, k := range val.MapKeys() {
			var key string
			switch k.Kind() {
			case reflect.String:
				key = k.String()
			default:
				key = fmt.Sprintf("%+v", k.Interface())
			}
			kvs = append(kvs, log.KeyValue{
				Key:   key,
				Value: convertValue(val.MapIndex(k).Interface()),
			})
		}
		return log.MapValue(kvs...)
	case reflect.Ptr, reflect.Interface:
		if val.IsNil() {
			return log.Value{}
		}
		return convertValue(val.Elem().Interface())
	}

	if s, ok := v.(stringer); ok {
		return log.StringValue(s.String())
	}

	// Try to handle this as gracefully as possible.
	//
	// Don't panic here. it is preferable to have user's open issue
	// asking why their attributes have a "unhandled: " prefix than
	// say that their code is panicking.
	return log.StringValue(fmt.Sprintf("unhandled: (%s) %+v", t, v))
}

// convertUintValue converts a uint64 to a log.Value.
// If the value is too large to fit in an int64, it is converted to a string.
func convertUintValue(v uint64) log.Value {
	if v > math.MaxInt64 {
		return log.StringValue(strconv.FormatUint(v, 10))
	}
	return log.Int64Value(int64(v))
}
