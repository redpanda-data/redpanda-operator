// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"github.com/go-logr/logr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	verboseLevel = 4
	traceLevel   = 2
	debugLevel   = 1
	infoLevel    = 0
)

// Reference implementation https://github.com/redpanda-data/console/blob/0ba44b236b6ddd7191da015f44a9302fc13665ec/backend/pkg/kafka/config_helper.go#L44

// kgoZapLogger is a franz-go logger adapter for zap.
type kgoZapLogger struct {
	logger logr.Logger
}

// Level Implements kgo.Logger interface. It returns the log level to log at.
// We pin this to debug as the zap logger decides what to actually send to the output stream.
func (kgoZapLogger) Level() kgo.LogLevel {
	return kgo.LogLevelDebug
}

// Log implements kgo.Logger interface
func (k kgoZapLogger) Log(level kgo.LogLevel, msg string, keyvals ...interface{}) {
	switch level {
	case kgo.LogLevelNone:
		// Don't log anything.
	case kgo.LogLevelDebug:
		k.logger.V(traceLevel).Info(msg, keyvals...)
	case kgo.LogLevelInfo:
		k.logger.V(debugLevel).Info(msg, keyvals...)
	case kgo.LogLevelWarn:
		k.logger.Info(msg, keyvals...)
	case kgo.LogLevelError:
		k.logger.Error(nil, msg, keyvals...)
	}
}

func wrapLogger(logger logr.Logger) kgo.Logger {
	return kgoZapLogger{
		logger: logger,
	}
}
