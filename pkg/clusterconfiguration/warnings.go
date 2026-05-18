// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package clusterconfiguration

import "strings"

// FormatWarnings returns a single user-facing string summarizing the
// non-fatal warnings collected during cluster-config Reify. Intended for
// use as a status condition message on the Cluster / Redpanda CR.
//
// Shared between Operator v1 (`vectorized.Cluster`) and v2 (`Redpanda`)
// so both code paths surface the same wording for the same failure mode.
// Returns the empty string when there are no warnings.
func FormatWarnings(warnings []error) string {
	if len(warnings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(warnings))
	for _, w := range warnings {
		if w == nil {
			continue
		}
		parts = append(parts, w.Error())
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}
