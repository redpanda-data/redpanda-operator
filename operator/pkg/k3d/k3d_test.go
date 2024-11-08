// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package k3d

import (
	"errors"
	"fmt"
	"sync"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultiInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi k3d cluster bootup test")
	}

	clusters := 3

	var running sync.WaitGroup
	running.Add(clusters)

	errCh := make(chan error, clusters)
	done := make(chan struct{})
	defer func() {
		close(done)
		running.Wait()
	}()

	for i := 0; i < clusters; i++ {
		go func() {
			defer running.Done()

			name := fmt.Sprintf("cluster-%d", i)
			_, err := GetOrCreate(name, WithAgents(1))
			defer forceCleanup(name)
			errCh <- err

			<-done
		}()
	}

	errs := []error{}
	for i := 0; i < clusters; i++ {
		if err := <-errCh; err != nil {
			errs = append(errs, err)
		}
	}

	assert.NoError(t, errors.Join(errs...))
}

func TestReservePort(t *testing.T) {
	const network = "tcp4"
	const iterations = 100

	reserved := map[int]struct{}{}
	for i := 0; i < iterations; i++ {
		port, err := reserveEphemeralPort("tcp4")
		assert.NoError(t, err)
		assert.NotZero(t, port)

		// Assert that we can listen on the provided port.
		lis, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		assert.NoError(t, err)
		assert.NoError(t, lis.Close())

		reserved[port] = struct{}{}
	}

	// Not the best test as failures are exceptionally unlikely to be
	// reproducible.
	// Bind a bunch of ephemeral ports and assert that we don't get allocated
	// any of the ports we've reserved.
	for i := 0; i < iterations; i++ {
		lis, err := net.Listen(network, "127.0.0.1:0")
		assert.NoError(t, err)

		// Defer closing of this listener to ensure we always get new ports
		// from listening to 0.
		defer lis.Close()

		_, portStr, err := net.SplitHostPort(lis.Addr().String())
		assert.NoError(t, err)

		port, err := strconv.Atoi(portStr)
		assert.NoError(t, err)

		t.Logf("%d", port)

		assert.NotContains(t, reserved, port)
	}
}
