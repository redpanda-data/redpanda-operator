// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWipeStaleDiskAfterDefaultsDisabled pins the single-cluster operator's
// conservative default: the destructive stale-disk wipe (K8S-843) is opt-in
// here, so --wipe-stale-disk-after defaults to 0 (disabled). The StretchCluster
// operator keeps its shipped 5m default (see operator/cmd/multicluster). The
// underlying "0 or negative == off" semantics are covered by
// TestRedpandaStaleDiskWipeThresholdAndDisable.
func TestWipeStaleDiskAfterDefaultsDisabled(t *testing.T) {
	f := Command().Flags().Lookup("wipe-stale-disk-after")
	require.NotNil(t, f, "run command must define --wipe-stale-disk-after")
	require.Equal(t, "0s", f.DefValue, "the single-cluster stale-disk wipe must be disabled (opt-in) by default")
}
