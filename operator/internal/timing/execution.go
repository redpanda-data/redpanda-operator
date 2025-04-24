package timing

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ExecutionTimer struct {
	start  time.Time
	logger logr.Logger

	stopped bool
}

func (e *ExecutionTimer) Stop(message string) {
	if e.stopped {
		return
	}

	e.stopped = true
	e.logger.V(10).Info(message, "duration", time.Since(e.start).String())
}

func Execution(ctx context.Context) *ExecutionTimer {
	return &ExecutionTimer{
		start:  time.Now(),
		logger: ctrl.LoggerFrom(ctx),
	}
}
