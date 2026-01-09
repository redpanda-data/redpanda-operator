// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"errors"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

var errKgo = errors.New("kgo client error")

// Reference implementation https://github.com/redpanda-data/console/blob/0ba44b236b6ddd7191da015f44a9302fc13665ec/backend/pkg/kafka/config_helper.go#L44

// kgoLogger is a franz-go logger adapter for logr.
type kgoLogger struct {
	logger logr.Logger
}

// Level Implements kgo.Logger interface. It returns the log level to log at.
// We pin this to debug as the underlying logger decides what to actually send to the output stream.
func (kgoLogger) Level() kgo.LogLevel {
	return kgo.LogLevelDebug
}

// Log implements kgo.Logger interface
func (k kgoLogger) Log(level kgo.LogLevel, msg string, keyvals ...interface{}) {
	switch level {
	case kgo.LogLevelNone:
		// Don't log anything.
	case kgo.LogLevelDebug:
		k.logger.V(log.DebugLevel).Info(msg, keyvals...)
	case kgo.LogLevelInfo:
		k.logger.V(log.InfoLevel).Info(msg, keyvals...)
	case kgo.LogLevelWarn:
		k.logger.Info(msg, keyvals...)
	case kgo.LogLevelError:
		k.logger.Error(errKgo, msg, keyvals...)
	}
}

func wrapLogger(logger logr.Logger) kgo.Logger {
	return kgoLogger{
		logger: logger,
	}
}
