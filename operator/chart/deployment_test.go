// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package operator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAddControllerSyncIntervalArgs covers the controllers.<resource>.interval ->
// --<resource>-sync-interval rendering shared by the single-cluster and
// multicluster operator deployments. A set value renders its flag; an empty
// value renders nothing (so the operator's built-in default applies and the
// default chart render stays flag-free).
func TestAddControllerSyncIntervalArgs(t *testing.T) {
	t.Run("renders set intervals and omits empty ones", func(t *testing.T) {
		defaults := map[string]string{}
		addControllerSyncIntervalArgs(defaults, Values{Controllers: Controllers{
			Topic: ControllerSyncConfig{Interval: "45s"},
			User:  ControllerSyncConfig{Interval: "20s"},
			// group/schema/role/shadowLink intentionally left empty
		}})

		assert.Equal(t, "45s", defaults["--topic-sync-interval"])
		assert.Equal(t, "20s", defaults["--user-sync-interval"])
		for _, flag := range []string{
			"--group-sync-interval",
			"--schema-sync-interval",
			"--role-sync-interval",
			"--shadowlink-sync-interval",
		} {
			_, ok := defaults[flag]
			assert.Falsef(t, ok, "%s should be omitted when its interval is empty", flag)
		}
	})

	t.Run("all empty renders no flags", func(t *testing.T) {
		defaults := map[string]string{}
		addControllerSyncIntervalArgs(defaults, Values{})
		assert.Empty(t, defaults)
	})
}
