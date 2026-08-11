// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build darwin

package main

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

func watchParent(ctx context.Context) error {
	// Getppid returns 1 if the parent process is already dead.
	ppid := os.Getppid()
	if ppid == 1 {
		return nil
	}
	return watchPID(ctx, ppid)
}

func watchPID(ctx context.Context, pid int) error {
	fd, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	watches := make([]unix.Kevent_t, 2)

	// Thanks to https://stackoverflow.com/questions/24689728/detect-process-exit-on-osx
	unix.SetKevent(&watches[0], pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_RECEIPT)
	watches[0].Fflags = unix.NOTE_EXIT

	// Listen for USER events as well so we can handle context cancellations.
	unix.SetKevent(&watches[1], 1, unix.EVFILT_USER, unix.EV_ADD)

	if _, err := unix.Kevent(fd, watches, nil, nil); err != nil {
		return err
	}

	// Wake kqueue poll when context is canceled.
	go func() {
		<-ctx.Done()

		var ev unix.Kevent_t
		unix.SetKevent(&ev, 1, unix.EVFILT_USER, 0)
		ev.Fflags = unix.NOTE_TRIGGER
		_, _ = unix.Kevent(fd, []unix.Kevent_t{ev}, nil, nil)
	}()

	// Wait until something wakes us.
	events := make([]unix.Kevent_t, 1)
	_, err = unix.Kevent(fd, nil, events, nil)
	return err
}
