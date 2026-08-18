// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

import (
	"errors"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
)

// validateTuning rejects tuning combinations that would render config no
// container ever acts on. It mirrors the Helm chart's render-time check in
// Tuning.Translate(): apply_host_tuners default-enables host tuners in
// redpanda.yaml, but the tuning init container as a whole is gated on
// tune_aio_events — without it the tuner config would be a silent no-op,
// which is the exact failure class apply_host_tuners exists to eliminate.
// Called from both the StatefulSet and ConfigMap render paths so neither
// can ship the contradiction on its own.
func validateTuning(t *redpandav1alpha2.StretchTuning) error {
	if t.IsApplyHostTunersEnabled() && !t.IsTuneAioEventsEnabled() {
		return errors.New("spec.tuning.apply_host_tuners requires spec.tuning.tune_aio_events=true: the host-mode tuning init container is gated on tune_aio_events, so this combination would render tuner config that nothing ever applies")
	}
	return nil
}

// tuningToConfiguration converts StretchTuning fields into a map suitable for
// merging into the rpk section of redpanda.yaml. This mirrors the Helm chart's
// Tuning.Translate() method, which serializes all tuning fields as rpk config
// keys (tune_aio_events, tune_clocksource, tune_ballast_file, etc.).
// ApplyHostTuners is deliberately NOT emitted: it is operator-level plumbing
// that selects which init container renders, not an rpk config key. The
// per-tuner flags it default-enables are merged in rpkNodeConfig (at lower
// precedence than user-provided config.rpk) via HostTunerDefaults.
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
