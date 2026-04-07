// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package testutils is a collection of generic test-centric utilities.
//
// Functions that accept a [testing.T] and return no error may call
// [testing.T.Fatal].
package testutils

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ContainerLogReader is the subset of the testcontainers.Container interface
// needed to capture logs.
type ContainerLogReader interface {
	Logs(ctx context.Context) (io.ReadCloser, error)
}

// DumpContainerLogsOnFailure registers a cleanup function that captures and logs
// container output if the test has failed. Call this right after creating a
// container to ensure logs are captured before the container is terminated.
func DumpContainerLogsOnFailure(t *testing.T, ctx context.Context, name string, container ContainerLogReader) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		logReader, err := container.Logs(ctx)
		if err != nil {
			t.Logf("[diagnostics] failed to get logs for container %s: %v", name, err)
			return
		}
		defer logReader.Close()

		logs, err := io.ReadAll(logReader)
		if err != nil {
			t.Logf("[diagnostics] failed to read logs for container %s: %v", name, err)
			return
		}
		// Truncate to last 5000 bytes to avoid flooding test output.
		if len(logs) > 5000 {
			logs = logs[len(logs)-5000:]
		}
		t.Logf("[diagnostics] container %s logs (last %d bytes):\n%s", name, len(logs), string(logs))
	})
}

// WriteFile writes data to a temporary file following the semantics of
// [os.CreateTemp] and returns the full path to the written file.
// Usage:
//
//	WriteFile(t, "*.json", []byte("{}"))
func WriteFile(t *testing.T, pattern string, data []byte) string {
	tmpFile, err := os.CreateTemp(t.TempDir(), pattern)
	require.NoError(t, err)

	_, err = tmpFile.Write(data)
	require.NoError(t, err)

	require.NoError(t, tmpFile.Close())

	return tmpFile.Name()
}
