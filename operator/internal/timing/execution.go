package timing

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ExecutionTimer records execution traces as logs when the controller
// is setup to emit timing traces only.
type ExecutionTimer struct {
	start  time.Time
	logger logr.Logger

	stopped bool
}

// Stop stops the execution timer and logs the duration results with
// the given message.
func (e *ExecutionTimer) Stop(message string) {
	if e.stopped {
		return
	}

	e.stopped = true
	e.logger.V(10).Info(message, "duration", time.Since(e.start).String())
}

// Execution returns an ExecutionTimer that can have Stop called on it.
func Execution(ctx context.Context) *ExecutionTimer {
	return &ExecutionTimer{
		start:  time.Now(),
		logger: ctrl.LoggerFrom(ctx),
	}
}
