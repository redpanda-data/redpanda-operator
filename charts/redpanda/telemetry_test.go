// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetricsFromDir(t *testing.T) {
	for tag, want := range map[string]bool{
		"v26.2.9":         false,
		"v26.3.0-rc1":     true,
		"v26.3.0":         true,
		"v26.3.1+build.1": true,
		"v27.0.0":         true,
		// Not semver: the broker's version is unknown, so it keeps env vars.
		"latest": false,
		"dev":    false,
		"":       false,
	} {
		require.Equal(t, want, MetricsFromDir(tag), "tag %q", tag)
	}
}
