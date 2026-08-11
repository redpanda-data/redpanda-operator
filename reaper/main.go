// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sync/errgroup"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE)
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	binary := filepath.Join(os.Getenv("KUBEBUILDER_ASSETS"), filepath.Base(os.Args[0]))
	args := os.Args[1:]

	//nolint:gosec // This is a development tool and the binary itself MUST be in
	//KUBEBUILDER_ASSETS. There's no real concern about command injection here.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	group.Go(func() error {
		// Ensure that the watcher get shutdown even if Run exits without error.
		defer cancel()

		return cmd.Run()
	})

	group.Go(func() error {
		defer cancel()

		return watchParent(ctx)
	})

	err := group.Wait()

	if err, ok := errors.AsType[*exec.ExitError](err); ok {
		os.Exit(err.ExitCode())
	}

	if err != nil {
		panic(err)
	}
}
