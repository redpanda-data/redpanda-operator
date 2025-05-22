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
