// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func watchParent(ctx context.Context) error {
	ppid := os.Getppid()
	if ppid == 1 {
		return nil
	}
	return watchPID(ctx, ppid)
}

func watchPID(ctx context.Context, pid int) error {
	// A pidfd becomes readable once the process it refers to exits, so we can
	// wait for pid without polling and without being its parent.
	// Requires Linux 5.3+. pidfd_open always sets O_CLOEXEC.
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		// pid is already gone and reaped, nothing to wait for.
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		return err
	}
	defer unix.Close(pidfd)

	// Used to wake the poll below when the context is canceled.
	eventfd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		return err
	}

	done := make(chan struct{})
	waiterExited := make(chan struct{})

	go func() {
		defer close(waiterExited)

		select {
		case <-ctx.Done():
			var buf [8]byte
			binary.NativeEndian.PutUint64(buf[:], 1)
			_, _ = unix.Write(eventfd, buf[:])
		case <-done:
		}
	}()

	// Ensure the waiter is finished with eventfd before closing it, otherwise
	// it could write to an unrelated fd that has since reused the number.
	defer func() {
		close(done)
		<-waiterExited
		_ = unix.Close(eventfd)
	}()

	fds := []unix.PollFd{
		{Fd: int32(pidfd), Events: unix.POLLIN},
		{Fd: int32(eventfd), Events: unix.POLLIN},
	}

	for {
		// Any signal delivered to this thread, including the runtime's own
		// preemption signals, will interrupt poll. Just resume waiting.
		if _, err := unix.Poll(fds, -1); errors.Is(err, unix.EINTR) {
			continue
		} else if err != nil {
			return err
		}

		return nil
	}
}
