// Copyright 2025 Redpanda Data, Inc.
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
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

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
