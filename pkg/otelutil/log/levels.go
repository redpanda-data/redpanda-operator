package log

import (
	otellog "go.opentelemetry.io/otel/log"
)

// These are for convenience when doing log.V(...) to log at a particular level.
const (
	VerboseLevel = 4
	TimingLevel  = 3
	TraceLevel   = 2
	DebugLevel   = 1
	InfoLevel    = 0
)

func LogrLevelFromString(level string) int {
	switch level {
	case "verbose":
		return VerboseLevel
	case "timing":
		return TimingLevel
	case "trace":
		return TraceLevel
	case "debug":
		return DebugLevel
	default:
		return InfoLevel
	}
}

func OTELLevelFromString(level string) otellog.Severity {
	switch level {
	case "verbose":
		return otellog.SeverityTrace3
	case "timing":
		return otellog.SeverityTrace2
	case "trace":
		return otellog.SeverityTrace
	case "debug":
		return otellog.SeverityDebug
	default:
		return otellog.SeverityInfo
	}
}

func ShouldLogOTEL(level, logLevel otellog.Severity) bool {
	switch logLevel {
	// special case our additional trace levels since otel doesn't have increasing
	// numbers for their log levels
	case otellog.SeverityTrace3:
		if level == otellog.SeverityTrace2 || level == otellog.SeverityTrace {
			return true
		}
	case otellog.SeverityTrace2:
		if level == otellog.SeverityTrace {
			return true
		}
	}

	return logLevel >= level
}
