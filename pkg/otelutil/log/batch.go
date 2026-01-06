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

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// BatchProcessoris a wrapper around an sdklog.BatchProcessor with level filtering support
type BatchProcessor struct {
	*sdklog.BatchProcessor
	severity otellog.Severity
}

func (p *BatchProcessor) Enabled(ctx context.Context, param sdklog.EnabledParameters) bool {
	return shouldLogOTEL(p.severity, param.Severity)
}

func NewBatchProcessor(exporter sdklog.Exporter, severity otellog.Severity, opts ...sdklog.BatchProcessorOption) *BatchProcessor {
	return &BatchProcessor{
		BatchProcessor: sdklog.NewBatchProcessor(exporter, opts...),
		severity:       severity,
	}
}
