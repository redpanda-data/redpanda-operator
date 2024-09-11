// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"
	"sync"
	"time"
)

// Logger is an interface for a logger that the Cleaner
// can use on cleanup failures.
type Logger interface {
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

// Cleaner manages the registering and running of lifecycle
// hooks.
type Cleaner struct {
	logger          Logger
	retainOnFailure bool
	cleanupTimeout  time.Duration
	cleanupFns      []func(ctx context.Context, failed bool)
	mutex           sync.Mutex
}

// NewCleaner returns a new Cleaner object.
func NewCleaner(logger Logger, opt *TestingOptions) *Cleaner {
	return &Cleaner{
		logger:          logger,
		retainOnFailure: opt.RetainOnFailure,
		cleanupTimeout:  opt.CleanupTimeout,
	}
}

// Cleanup registers the callback to be called when DoCleanup is called.
func (c *Cleaner) Cleanup(fn CleanupFunc) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cleanupFns = append(c.cleanupFns, func(ctx context.Context, failed bool) {
		if failed && c.retainOnFailure {
			c.logger.Log("skipping cleanup due to test failure and retain flag being set")
			return
		}
		fn(ctx)
	})
}

// DoCleanup calls all of the registered cleanup callbacks.
func (c *Cleaner) DoCleanup(ctx context.Context, failed bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cancel := func() {}
	if c.cleanupTimeout != 0 {
		ctx, cancel = context.WithTimeout(ctx, c.cleanupTimeout)
	}
	defer cancel()

	// run cleanup in backwards order so that we don't
	// remove dependencies prior to their dependents
	// being cleaned up
	for i := len(c.cleanupFns) - 1; i >= 0; i-- {
		c.cleanupFns[i](ctx, failed)
	}

	c.cleanupFns = nil
}
