// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import "github.com/redpanda-data/common-go/license"

// LicenseChecksum returns the hex SHA-256 checksum of a parsed enterprise
// license — the same value Redpanda core reports as id_hash. The
// license.RedpandaLicense interface doesn't expose the checksum, so we type
// switch to the concrete versions. Returns "" for an unrecognized type.
//
// Callers must gate on license.AllowsEnterpriseFeatures() first: the checksum
// is only emitted for licensed clusters so OSS/unlicensed reports stay
// anonymous.
func LicenseChecksum(l license.RedpandaLicense) string {
	switch v := l.(type) {
	case *license.V1RedpandaLicense:
		return v.Checksum
	case *license.V0RedpandaLicense:
		return v.Checksum
	default:
		return ""
	}
}
