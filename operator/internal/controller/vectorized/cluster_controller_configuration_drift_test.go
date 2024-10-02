// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
)

func TestHasDrift(t *testing.T) {
	assert := require.New(t)

	schema := map[string]rpadmin.ConfigPropertyMetadata{
		"write_caching_default": {
			Type:         "integer",
			Description:  "wait_for_leader_timeout_ms",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "tunable",
			Units:        "ms",
		},
		"cloud_storage_secret_key": {
			Type:         "string",
			Description:  "AWS secret key",
			Nullable:     true,
			NeedsRestart: true,
			IsSecret:     true,
			Visibility:   "user",
		},
	}

	tests := []struct {
		name          string
		desired       map[string]any
		actual        map[string]any
		driftExpected bool
	}{
		{
			name: "cloud_storage_secret_key is omitted",
			desired: map[string]any{
				"cloud_storage_secret_key": "my-key",
				"write_caching_default":    10,
			},
			actual: map[string]any{
				"cloud_storage_secret_key": "[secret]", // Redpanda replaces secret values with [secret]
				"write_caching_default":    10,
			},
			driftExpected: false,
		},
		{
			name: "diff is detected",
			desired: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_background_jobs_quota": 123,
			},
			driftExpected: true,
		},
		{
			name: "diff is detected along other diffs",
			desired: map[string]any{
				"cloud_storage_secret_key":            "my-key",
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_secret_key":            "[secret]",
				"cloud_storage_background_jobs_quota": 123,
			},
			driftExpected: true,
		},
		{
			name: "no diff is detected if all is equal",
			desired: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			driftExpected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := hasDrift(logr.Discard(), tt.desired, tt.actual, schema)
			assert.Equal(tt.driftExpected, ok)
		})
	}
}
