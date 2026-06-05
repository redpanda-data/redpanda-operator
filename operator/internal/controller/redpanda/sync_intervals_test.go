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
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIntervalOrDefault(t *testing.T) {
	def := 5 * time.Minute

	// A positive interval is used as-is (operator flag / chart value supplied).
	assert.Equal(t, 30*time.Second, intervalOrDefault(30*time.Second, def))

	// Zero falls back to the default (flag unset, e.g. the multicluster path or a
	// `--user-sync-interval=0`), rather than disabling the periodic reconcile.
	assert.Equal(t, def, intervalOrDefault(0, def))

	// Negative is treated as unset.
	assert.Equal(t, def, intervalOrDefault(-1*time.Second, def))
}

// TestDefaultSyncIntervals locks in the operator-wide defaults referenced by the
// run command's flag defaults and documented in the v26.2 changelog. The Topic
// default in particular replaces the CRD's old baked-in "3s".
func TestDefaultSyncIntervals(t *testing.T) {
	assert.Equal(t, 30*time.Second, DefaultTopicSyncInterval, "Topic default replaces the removed 3s CRD default")
	assert.Equal(t, 5*time.Minute, DefaultUserSyncInterval)
	assert.Equal(t, 5*time.Minute, DefaultGroupSyncInterval)
	assert.Equal(t, 5*time.Minute, DefaultSchemaSyncInterval)
	assert.Equal(t, 5*time.Minute, DefaultRoleSyncInterval)
	assert.Equal(t, 5*time.Minute, DefaultShadowLinkSyncInterval)
}
