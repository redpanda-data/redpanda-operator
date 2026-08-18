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

	syncclusterconfig "github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
)

// TestConfigSyncModeMatchesSyncerMode pins the seam's ConfigSyncMode enum to
// syncclusterconfig.SyncerMode: the adapters convert between them by simple
// integer conversion, which is only valid while the values line up 1:1.
func TestConfigSyncModeMatchesSyncerMode(t *testing.T) {
	require.EqualValues(t, syncclusterconfig.SyncerModeAdditive, ConfigSyncModeAdditive)
	require.EqualValues(t, syncclusterconfig.SyncerModeDeclarative, ConfigSyncModeDeclarative)
	require.EqualValues(t, syncclusterconfig.SyncerModeDisabled, ConfigSyncModeDisabled)
}
