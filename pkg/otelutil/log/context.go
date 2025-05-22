package log

import (
	"context"

	"github.com/go-logr/logr"
)

type contextFreeSink struct {
	logr.LogSink
}

func ContextFree(sink logr.LogSink) logr.LogSink {
	return &contextFreeSink{
		LogSink: sink,
	}
}

func (l contextFreeSink) WithValues(kvList ...any) logr.LogSink {
	return ContextFree(l.LogSink.WithValues(filterContexts(kvList)...))
}

func (l contextFreeSink) Info(level int, msg string, kvList ...any) {
	l.LogSink.Info(level, msg, filterContexts(kvList)...)
}

func (l contextFreeSink) Error(err error, msg string, kvList ...any) {
	l.LogSink.Error(err, msg, filterContexts(kvList)...)
}

func filterContexts(kvList []any) []any {
	filtered := []any{}
	if len(kvList)%2 != 0 {
		kvList = append(kvList, nil)
	}
	for i := 0; i < len(kvList); i += 2 {
		if _, ok := kvList[i+1].(context.Context); !ok {
			filtered = append(filtered, kvList[i], kvList[i+1])
		}
	}

	return filtered
}
