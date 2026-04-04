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
	otellog "go.opentelemetry.io/otel/log"
)

// These are for convenience when doing log.V(...) to log at a particular level.
const (
	// Verbose is the level at which all logs will be shown, only set this level
	// if you want to output with the most verbose of any logs
	VerboseLevel = 4
	// Timing shows any additional logs related to timing, one level above trace
	// logs
	TimingLevel = 3
	// Trace is for detailed execution flow
	TraceLevel = 2
	// Debug is used when information is helpful, but not necessary normally
	DebugLevel = 1
	// Info is the default log level
	InfoLevel = 0
)

type TypedLevel struct {
	Name      string
	LogrLevel int
	OTELLevel otellog.Severity
}

var (
	levelStrings map[string]TypedLevel
	levelOTEL    map[otellog.Severity]TypedLevel
	levelLogr    map[int]TypedLevel
	DefaultLevel = TypedLevel{
		Name:      "info",
		LogrLevel: InfoLevel,
		OTELLevel: otellog.SeverityInfo,
	}
	CatchAllLevel = TypedLevel{
		Name:      "verbose",
		LogrLevel: VerboseLevel,
		OTELLevel: otellog.SeverityTrace3,
	}
	levels = []TypedLevel{
		CatchAllLevel,
		{
			Name:      "timing",
			LogrLevel: TimingLevel,
			OTELLevel: otellog.SeverityTrace2,
		},
		{
			Name:      "trace",
			LogrLevel: TraceLevel,
			OTELLevel: otellog.SeverityTrace,
		},
		{
			Name:      "debug",
			LogrLevel: DebugLevel,
			OTELLevel: otellog.SeverityDebug,
		},
		DefaultLevel,
	}
)

func init() {
	levelStrings = make(map[string]TypedLevel)
	levelOTEL = make(map[otellog.Severity]TypedLevel)
	levelLogr = make(map[int]TypedLevel)

	for _, level := range levels {
		levelStrings[level.Name] = level
		levelOTEL[level.OTELLevel] = level
		levelLogr[level.LogrLevel] = level
	}
}

func LevelFromString(level string) TypedLevel {
	if l, ok := levelStrings[level]; ok {
		return l
	}
	return levelStrings[CatchAllLevel.Name]
}

func LevelFromLogr(level int) TypedLevel {
	if l, ok := levelLogr[level]; ok {
		return l
	}
	return levelLogr[CatchAllLevel.LogrLevel]
}

func LevelFromOTEL(level otellog.Severity) TypedLevel {
	if l, ok := levelOTEL[level]; ok {
		return l
	}
	return levelOTEL[CatchAllLevel.OTELLevel]
}

func shouldLog(loggerLevel, messageLevel TypedLevel) bool {
	// we always use the logr representation to compare since it
	// assumes we're always increasing
	return loggerLevel.LogrLevel >= messageLevel.LogrLevel
}

func shouldLogOTEL(loggerLevel, messageLevel otellog.Severity) bool {
	return shouldLog(LevelFromOTEL(loggerLevel), LevelFromOTEL(messageLevel))
}
