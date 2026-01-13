// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package featuregates provides information on Redpanda versions where
// features are enabled.
package featuregates

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

//nolint:stylecheck // the linter suggests camel case for one letter!?!?
var (
	V23_2 = mustSemVer("v23.2.0")
)

// MinimumSupportedVersion encompasses previously conditional behaviour that was wrapped up in featuregates.
func MinimumSupportedVersion(version string) bool {
	return atLeastVersion(V23_2, version)
}

// atLeastVersion tells if the given version is greater or equal than the
// minVersion.
// All semver incompatible versions (such as "dev" or "latest") and non-version
// tags (such as v0.0.0-xxx) are always considered newer than minVersion.
func atLeastVersion(minVersion *semver.Version, version string) bool {
	v, err := semver.NewVersion(version)
	if err != nil {
		return true
	}
	if v.Major() == 0 && v.Minor() == 0 && v.Patch() == 0 {
		return true
	}
	return v.Major() == minVersion.Major() && v.Minor() >= minVersion.Minor() || v.Major() > minVersion.Major()
}

func mustSemVer(version string) *semver.Version {
	v, err := semver.NewVersion(version)
	if err != nil {
		panic(fmt.Sprintf("version %q is not semver compatible", version))
	}
	return v
}
