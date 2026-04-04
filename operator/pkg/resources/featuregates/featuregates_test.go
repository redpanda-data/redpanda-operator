// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package featuregates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
)

func TestFeatureGates(t *testing.T) { //nolint:funlen // table tests can be longer
	cases := []struct {
		version   string
		supported bool
	}{
		{
			version:   "v21.1.1",
			supported: false,
		},
		{
			version:   "v23.1.9",
			supported: false,
		},
		{
			version:   "v23.2",
			supported: true,
		},
		{
			version:   "dev",
			supported: true,
		},
		// Versions from: https://hub.docker.com/r/vectorized/redpanda/tags
		{
			version:   "latest",
			supported: true,
		},
		{
			version:   "v23.2.3-arm64",
			supported: true,
		},
		// Versions from: https://hub.docker.com/r/vectorized/redpanda-nightly/tags
		{
			version:   "v0.0.0-20221006git23a658b",
			supported: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			supported := featuregates.MinimumSupportedVersion(tc.version)
			assert.Equal(t, tc.supported, supported)
		})
	}
}
