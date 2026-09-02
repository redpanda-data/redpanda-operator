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
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/leaderelection"
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

// TestLeaderElectionTimingFlags pins the leader-election tunables
// (--leader-election-lease-duration, --leader-election-renew-deadline,
// --leader-election-retry-period): their defaults must equal
// controller-runtime's shipped values, so that an unset flag changes no
// behavior, and parsed values must actually land in the ctrl.Options handed
// to the manager — the flags bind ctrl.Options' *time.Duration fields
// directly via Flags().Duration, a different idiom from the DurationVar
// calls around them, and one a refactor could silently break.
func TestLeaderElectionTimingFlags(t *testing.T) {
	t.Run("defaults match controller-runtime", func(t *testing.T) {
		for flag, def := range map[string]string{
			"leader-election-lease-duration": "15s",
			"leader-election-renew-deadline": "10s",
			"leader-election-retry-period":   "2s",
		} {
			f := Command().Flags().Lookup(flag)
			require.NotNil(t, f, "run command must define --%s", flag)
			require.Equal(t, def, f.DefValue, "--%s must default to controller-runtime's shipped value", flag)
		}
	})

	t.Run("parsed values reach the manager options", func(t *testing.T) {
		var options RunOptions
		cmd := &cobra.Command{}
		options.BindFlags(cmd)

		require.NoError(t, cmd.ParseFlags([]string{
			"--leader-election-lease-duration=45s",
			"--leader-election-renew-deadline=30s",
			"--leader-election-retry-period=5s",
		}))

		require.NotNil(t, options.managerOptions.LeaseDuration)
		require.NotNil(t, options.managerOptions.RenewDeadline)
		require.NotNil(t, options.managerOptions.RetryPeriod)
		require.Equal(t, 45*time.Second, *options.managerOptions.LeaseDuration)
		require.Equal(t, 30*time.Second, *options.managerOptions.RenewDeadline)
		require.Equal(t, 5*time.Second, *options.managerOptions.RetryPeriod)
	})

	// client-go's leader elector rejects its config at manager start unless
	// leaseDuration > renewDeadline and renewDeadline > JitterFactor *
	// retryPeriod. There is no fail-fast validation on the flags (a bad combo
	// surfaces as a manager start error), so at minimum the shipped defaults
	// must satisfy the invariants.
	t.Run("defaults satisfy the client-go invariants", func(t *testing.T) {
		var options RunOptions
		cmd := &cobra.Command{}
		options.BindFlags(cmd)
		require.NoError(t, cmd.ParseFlags(nil))

		lease := *options.managerOptions.LeaseDuration
		renew := *options.managerOptions.RenewDeadline
		retry := *options.managerOptions.RetryPeriod
		require.Greater(t, lease, renew, "client-go requires leaseDuration > renewDeadline")
		require.Greater(t, float64(renew), leaderelection.JitterFactor*float64(retry),
			"client-go requires renewDeadline > JitterFactor * retryPeriod")
	})
}
