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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
)

// TestLicenseChecksum verifies that our local SHA256 checksum calculation
// matches the checksum Redpanda reports via GetLicenseInfo after a license
// is set. This is the check used by setupLicense to skip redundant
// SetLicense calls.
func TestLicenseChecksum(t *testing.T) {
	const envVar = "REDPANDA_SAMPLE_LICENSE"

	license := os.Getenv(envVar)
	if license == "" {
		t.Skipf("%s is not set, skipping license checksum test", envVar)
	}

	ctx := context.Background()

	container, err := redpanda.Run(ctx,
		os.Getenv("TEST_REDPANDA_REPO")+":"+os.Getenv("TEST_REDPANDA_VERSION"),
	)
	require.NoError(t, err)

	adminAddr, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	adminClient, err := rpadmin.NewAdminAPI([]string{adminAddr}, nil, nil)
	require.NoError(t, err)
	defer adminClient.Close()

	// Set the license on the cluster.
	err = adminClient.SetLicense(ctx, strings.NewReader(license))
	require.NoError(t, err)

	// Retrieve the license info that Redpanda computed.
	info, err := adminClient.GetLicenseInfo(ctx)
	require.NoError(t, err)
	require.True(t, info.Loaded, "license should be loaded after SetLicense")

	// Verify our local SHA256 matches what Redpanda reports.
	h := sha256.Sum256([]byte(license))
	localChecksum := hex.EncodeToString(h[:])

	require.Equal(t, localChecksum, info.Properties.Checksum,
		"local SHA256 checksum should match Redpanda's reported checksum")

	t.Run("changed license triggers new SetLicense", func(t *testing.T) {
		const secondEnvVar = "REDPANDA_SECOND_SAMPLE_LICENSE"

		secondLicense := os.Getenv(secondEnvVar)
		if secondLicense == "" {
			t.Skipf("%s is not set, skipping license change test", secondEnvVar)
		}

		// The second license must produce a different checksum.
		h2 := sha256.Sum256([]byte(secondLicense))
		secondChecksum := hex.EncodeToString(h2[:])
		require.NotEqual(t, localChecksum, secondChecksum,
			"second license should have a different checksum than the first")

		// Simulate what setupLicense does: the loaded checksum no longer
		// matches the desired license, so SetLicense should be called.
		require.NotEqual(t, info.Properties.Checksum, secondChecksum,
			"loaded checksum should not match the new license, triggering SetLicense")

		// Actually set the new license.
		err := adminClient.SetLicense(ctx, strings.NewReader(secondLicense))
		require.NoError(t, err)

		// Verify the cluster now reports the new license's checksum.
		newInfo, err := adminClient.GetLicenseInfo(ctx)
		require.NoError(t, err)
		require.True(t, newInfo.Loaded, "new license should be loaded")
		require.Equal(t, secondChecksum, newInfo.Properties.Checksum,
			"cluster checksum should match the second license after SetLicense")

		// Verify the old checksum no longer matches — setupLicense would
		// have correctly detected the change.
		require.NotEqual(t, localChecksum, newInfo.Properties.Checksum,
			"old license checksum should no longer match")
	})
}
