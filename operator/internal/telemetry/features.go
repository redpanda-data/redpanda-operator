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
	"fmt"
	"strconv"
)

// MergeAdHocFeatures folds operator-provided ad-hoc feature flags (from the
// --telemetry-features key=bool flag) into the built-in features map and
// returns the result. Ad-hoc entries take precedence over built-ins, so a
// deployer can both add new signals (e.g. "redpanda-cloud": true for
// cloud-managed installs) and explicitly override a derived one. Values must
// parse as booleans; anything else is a deployment configuration error and is
// returned as such so the operator fails fast at startup instead of silently
// reporting skewed data.
func MergeAdHocFeatures(features map[string]bool, adHoc map[string]string) (map[string]bool, error) {
	if features == nil {
		features = map[string]bool{}
	}
	for name, raw := range adHoc {
		if name == "" {
			return nil, fmt.Errorf("telemetry feature with value %q has an empty name", raw)
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("telemetry feature %q: value %q is not a boolean: %w", name, raw, err)
		}
		features[name] = value
	}
	return features, nil
}
