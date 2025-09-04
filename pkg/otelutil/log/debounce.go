package log

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type Debouncer struct {
	lastLines map[string]time.Time
	period    time.Duration
	mutex     sync.Mutex
}

func NewDebouncer(period time.Duration) *Debouncer {
	return &Debouncer{
		lastLines: make(map[string]time.Time),
		period:    period,
	}
}

func (d *Debouncer) Error(logger logr.Logger, err error, msg string, keysAndValues ...any) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if time.Since(d.lastLines[err.Error()]) > d.period {
		d.lastLines[err.Error()] = time.Now()
		logger.Error(err, msg, keysAndValues...)
	}
}

var GlobalDebouncer = NewDebouncer(1 * time.Minute)

func DebounceError(logger logr.Logger, err error, msg string, keysAndValues ...any) {
	GlobalDebouncer.Error(logger, err, msg, keysAndValues...)
}
