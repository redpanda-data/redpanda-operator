// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validBaseOptions returns a MulticlusterOptions that passes validate() so a
// single field under test can be varied in isolation.
func validBaseOptions() MulticlusterOptions {
	return MulticlusterOptions{
		Name:                       "node-a",
		Address:                    "10.0.0.1:9999",
		CAFile:                     "/etc/raft/ca.crt",
		PrivateKeyFile:             "/etc/raft/tls.key",
		CertificateFile:            "/etc/raft/tls.crt",
		BaseImage:                  "redpanda",
		BaseTag:                    "v25.1.1",
		PostRestartCaughtUpPercent: 100,
	}
}

// TestValidatePostRestartCaughtUpPercent covers the StretchCluster port of the
// roll-loop safety: like the single-cluster `run` command, the multicluster
// command rejects --post-restart-caught-up-percent outside [1,100] (>100 stalls
// the rolling restart forever, 0 silently disables the per-broker post-restart
// gate).
func TestValidatePostRestartCaughtUpPercent(t *testing.T) {
	t.Run("baseline is valid", func(t *testing.T) {
		o := validBaseOptions()
		require.NoError(t, o.validate())
	})

	for _, p := range []int{0, -1, 101, 200} {
		t.Run("rejects out-of-range value", func(t *testing.T) {
			o := validBaseOptions()
			o.PostRestartCaughtUpPercent = p
			err := o.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "post-restart-caught-up-percent")
		})
	}

	for _, p := range []int{1, 50, 100} {
		t.Run("accepts in-range value", func(t *testing.T) {
			o := validBaseOptions()
			o.PostRestartCaughtUpPercent = p
			require.NoError(t, o.validate())
		})
	}
}
