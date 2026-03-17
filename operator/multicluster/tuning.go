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
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// tuningToConfiguration converts StretchTuning fields into a map suitable for
// merging into the rpk section of redpanda.yaml. This mirrors the Helm chart's
// Tuning.Translate() method, which serializes all tuning fields as rpk config
// keys (tune_aio_events, tune_clocksource, tune_ballast_file, etc.).
func tuningToConfiguration(t *redpandav1alpha2.StretchTuning) map[string]any {
	if t == nil {
		return nil
	}

	result := map[string]any{}

	if t.TuneAioEvents != nil {
		result["tune_aio_events"] = *t.TuneAioEvents
	}
	if t.TuneClockSource != nil {
		result["tune_clocksource"] = *t.TuneClockSource
	}
	if t.TuneBallastFile != nil {
		result["tune_ballast_file"] = *t.TuneBallastFile
	}
	if t.BallastFilePath != nil {
		result["ballast_file_path"] = *t.BallastFilePath
	}
	if t.BallastFileSize != nil {
		result["ballast_file_size"] = *t.BallastFileSize
	}
	if t.WellKnownIo != nil {
		result["well_known_io"] = *t.WellKnownIo
	}

	return result
}
