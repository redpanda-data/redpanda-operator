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

	"github.com/redpanda-data/common-go/license"
	"github.com/stretchr/testify/require"
)

func TestLicenseChecksum(t *testing.T) {
	require.Equal(t, "v1-checksum", LicenseChecksum(&license.V1RedpandaLicense{Checksum: "v1-checksum"}))
	require.Equal(t, "v0-checksum", LicenseChecksum(&license.V0RedpandaLicense{Checksum: "v0-checksum"}))
	// Unrecognized implementations yield an empty checksum rather than panicking.
	require.Empty(t, LicenseChecksum(nil))
}
