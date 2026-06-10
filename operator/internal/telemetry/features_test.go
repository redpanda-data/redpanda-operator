// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeAdHocFeatures(t *testing.T) {
	// Ad-hoc entries are added and override built-ins.
	merged, err := MergeAdHocFeatures(
		map[string]bool{"pvcUnbinder": true, "webhook": false},
		map[string]string{"redpanda-cloud": "true", "webhook": "true"},
	)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{
		"pvcUnbinder":    true,
		"webhook":        true, // overridden by the ad-hoc entry
		"redpanda-cloud": true,
	}, merged)

	// Nil inputs are fine.
	merged, err = MergeAdHocFeatures(nil, map[string]string{"redpanda-cloud": "false"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"redpanda-cloud": false}, merged)

	merged, err = MergeAdHocFeatures(map[string]bool{"a": true}, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"a": true}, merged)

	// Non-boolean values and empty names fail fast.
	_, err = MergeAdHocFeatures(nil, map[string]string{"redpanda-cloud": "yes-please"})
	require.ErrorContains(t, err, `"redpanda-cloud"`)

	_, err = MergeAdHocFeatures(nil, map[string]string{"": "true"})
	require.ErrorContains(t, err, "empty name")
}
