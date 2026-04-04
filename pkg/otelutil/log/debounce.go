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
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// ErrorStringDebouncer returns a debouncer function that debounces based on the error string
func ErrorStringDebouncer(err error, msg string, keysAndValues ...any) (string, bool) {
	if err == nil {
		// always let errors that are nil go through
		return "", false
	}
	return err.Error(), true
}

// DebounceKeyingFn returns a string used as a key to the debouncing map and a boolean
// indicating whether or not the debouncer should check to debounce the message based
// on the given key
type DebounceKeyingFn func(err error, msg string, keysAndValues ...any) (string, bool)

// Debouncer is used for debouncing noisy log messages based some function passed in to determine
// whether the message should be debounced or not. Ideally it should be used as a package-level
// resource.
type Debouncer struct {
	lastLines             map[string]time.Time
	period                time.Duration
	mutex                 sync.Mutex
	shouldDebounceKeyFunc DebounceKeyingFn
}

// NewDebouncer creates a new log debouncer.
func NewDebouncer(period time.Duration, shouldDebounceKeyFunc DebounceKeyingFn) *Debouncer {
	return &Debouncer{
		lastLines:             make(map[string]time.Time),
		period:                period,
		shouldDebounceKeyFunc: shouldDebounceKeyFunc,
	}
}

// Error logs a message if it should not be debounced
func (d *Debouncer) Error(logger logr.Logger, err error, msg string, keysAndValues ...any) {
	key, shouldCheck := d.shouldDebounceKeyFunc(err, msg, keysAndValues...)

	if !shouldCheck {
		logger.Error(err, msg, keysAndValues...)
		return
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	if time.Since(d.lastLines[key]) > d.period {
		d.lastLines[err.Error()] = time.Now()
		logger.Error(err, msg, keysAndValues...)
	}
}

// GC prunes any entries from our internal tracker that have
// expired.
func (d *Debouncer) GC() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	for key, lastLogged := range d.lastLines {
		if time.Since(lastLogged) > d.period {
			delete(d.lastLines, key)
		}
	}
}

// GlobalDebouncer returns a globally initialized debouncer that keys off of the
// error string of a message
var GlobalDebouncer = NewDebouncer(1*time.Minute, ErrorStringDebouncer)

// DebounceError debounces with the GlobalDebouncer
func DebounceError(logger logr.Logger, err error, msg string, keysAndValues ...any) {
	GlobalDebouncer.Error(logger, err, msg, keysAndValues...)
}

func init() {
	go func() {
		for {
			// just GC the global debouncer every 3 minutes, not too aggressive, but guards
			// against leaks if this ever gets used widely
			<-time.After(3 * time.Minute)
			GlobalDebouncer.GC()
		}
	}()
}
