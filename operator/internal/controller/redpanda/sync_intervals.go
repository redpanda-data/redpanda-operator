// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import "time"

// Default controller reconcile (sync) intervals.
//
// These are the operator-wide defaults applied when no interval is supplied via
// the operator's --<resource>-sync-interval flag (and, for the Topic resource,
// when the per-CR spec.interval is also unset). The operator's run command uses
// these as its CLI flag defaults; the setup paths that are not flag-driven
// (e.g. the multicluster managers) fall back to these constants directly.
//
// Introduced in the v26.2 operator. Previously the Topic interval default
// ("3s") was baked into the Topic CRD; it now lives here / in the flag instead.
const (
	DefaultTopicSyncInterval      = 30 * time.Second
	DefaultUserSyncInterval       = 5 * time.Minute
	DefaultGroupSyncInterval      = 5 * time.Minute
	DefaultSchemaSyncInterval     = 5 * time.Minute
	DefaultRoleSyncInterval       = 5 * time.Minute
	DefaultShadowLinkSyncInterval = 5 * time.Minute
)

// intervalOrDefault returns interval when it is positive, otherwise def. Used by
// the controller setup paths so a zero/unset flag falls back to the operator
// default rather than disabling the periodic reconcile.
func intervalOrDefault(interval, def time.Duration) time.Duration {
	if interval <= 0 {
		return def
	}
	return interval
}
