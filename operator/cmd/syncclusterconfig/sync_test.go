// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package syncclusterconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils/testutils"
)

func TestSync(t *testing.T) {
	const user = "syncer"
	const admin = "admin"
	const password = "password"
	const saslMechanism = "SCRAM-SHA-256"

	ctx := context.Background()
	logger := testr.New(t)
	ctx = log.IntoContext(ctx, logger)

	// No auth is easy, only test on a cluster with auth on admin API.
	container, err := redpanda.Run(
		ctx,
		"docker.redpanda.com/redpandadata/redpanda:"+os.Getenv("TEST_REDPANDA_VERSION"),
		// TODO: Upgrade to testcontainers 0.33.0 so we get
		// WithBootstrapConfig. For whatever reason, it seems to not get along
		// with CI.
		// redpanda.WithBootstrapConfig("admin_api_require_auth", true),
		redpanda.WithSuperusers("syncer"),
		testcontainers.WithEnv(map[string]string{
			"RP_BOOTSTRAP_USER": fmt.Sprintf("%s:%s:%s", user, password, saslMechanism),
		}),
	)
	require.NoError(t, err)

	// Configure the same environment as we'll be executed in within the helm
	// chart:
	// https://github.com/redpanda-data/helm-charts/commit/081c08b6b83ba196994ec3312a7c6011e4ef0a22#diff-84c6555620e4e5f79262384a9fa3e8f4876b36bb3a64748cbd8fbdcb66e8c1b9R966
	t.Setenv("RPK_USER", user)
	t.Setenv("RPK_PASS", password)
	t.Setenv("RPK_SASL_MECHANISM", saslMechanism)

	adminAPIAddr, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	adminAPIClient, err := rpadmin.NewAdminAPI([]string{adminAPIAddr}, &rpadmin.BasicAuth{
		Username: user,
		Password: password,
	}, nil)
	require.NoError(t, err)
	defer adminAPIClient.Close()

	_, err = adminAPIClient.PatchClusterConfig(ctx, map[string]any{
		"admin_api_require_auth": true,
	}, []string{})
	require.NoError(t, err)

	rpkConfigBytes, err := yaml.Marshal(map[string]any{
		"rpk": map[string]any{
			"admin_api": map[string]any{
				"addresses": []string{adminAPIAddr},
				"tls":       nil,
			},
		},
	})
	require.NoError(t, err)

	redpandaYAMLPath := testutils.WriteFile(t, "redpanda-*.yaml", rpkConfigBytes)
	usersTXTPath := testutils.WriteFile(t, "users-*.txt", []byte(strings.Join([]string{user, password, saslMechanism}, ":")))

	cases := []struct {
		Config      map[string]any
		Expected    map[string]any
		UsersTXTDir string
		Mode        SyncerMode
	}{
		{
			// Superusers only set to what is pulled from usersTXT
			// NB: `user` can't be removed from `superusers` when
			// `admin_api_require_auth` is enabled as redpanda validates this:
			// > superusers must contain the user making the change when auth is enabled
			Config:      map[string]any{},
			UsersTXTDir: filepath.Dir(usersTXTPath),
			Expected: map[string]any{
				"admin_api_require_auth": true,
				"superusers":             []any{user},
			},
		},
		{
			// Passing a superusers entry to show that the value from
			// the users.txt gets merged.
			//
			// Note that all subsequent runs build off of previous runs
			// in this test, so all subsequent test cases will have both
			// superusers.
			Config: map[string]any{
				"superusers": []string{admin},
			},
			UsersTXTDir: filepath.Dir(usersTXTPath),
			Expected: map[string]any{
				"admin_api_require_auth": true,
				"superusers":             []any{admin, user},
			},
		},
		{
			Config: map[string]any{
				"abort_index_segment_size":      10,
				"audit_queue_drain_interval_ms": 60,
			},
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"admin_api_require_auth":        true,
				"audit_queue_drain_interval_ms": 60,
				"superusers":                    []any{admin, user},
			},
		},
		{
			Config: map[string]any{
				"abort_index_segment_size": 10,
			},
			// Showcasing that settings are not unset if/when they're removed.
			// This is to showcase feature parity with the helm chart's job(s).
			// Improvements are welcome.
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"admin_api_require_auth":        true,
				"audit_queue_drain_interval_ms": 60,
				"superusers":                    []any{admin, user},
			},
		},
		{
			Config: map[string]any{
				"audit_queue_drain_interval_ms": 70,
			},
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"admin_api_require_auth":        true,
				"audit_queue_drain_interval_ms": 70,
				"superusers":                    []any{admin, user},
			},
		},
		{
			Config: map[string]any{
				// Tricky tricky: Disable admin API auth to showcase that
				// auth is ignored if it's not required.
				"admin_api_require_auth": false,
			},
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"audit_queue_drain_interval_ms": 70,
				"superusers":                    []any{admin, user},
			},
		},
		{
			Config: map[string]any{
				"admin_api_require_auth": false,
				"superusers":             []string{user},
			},
			UsersTXTDir: os.TempDir() + "/this-path-does-not-exist",
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"audit_queue_drain_interval_ms": 70,
				"superusers":                    []any{user},
			},
		},
		{
			// In declarative mode, push everything supplied through
			Config: map[string]any{
				"abort_index_segment_size":      10,
				"audit_queue_drain_interval_ms": 70,
				"superusers":                    []any{user},
			},
			UsersTXTDir: os.TempDir() + "/this-path-does-not-exist",
			Expected: map[string]any{
				"abort_index_segment_size":      10,
				"audit_queue_drain_interval_ms": 70,
				"superusers":                    []any{user},
			},
		},
		{
			// In declarative mode, we'll drop any unspecified configuration
			Config:      map[string]any{},
			UsersTXTDir: os.TempDir() + "/this-path-does-not-exist",
			Mode:        SyncerModeDeclarative,
			Expected:    map[string]any{},
		},
		{
			// Even in declarative mode, the UsersTXT
			// is still applied.
			Mode:        SyncerModeDeclarative,
			UsersTXTDir: filepath.Dir(usersTXTPath),
			Config: map[string]any{
				"superusers": []string{admin},
			},
			Expected: map[string]any{
				"superusers": []any{admin, user},
			},
		},
		{
			Mode: SyncerModeDeclarative,
			Config: map[string]any{
				// An aliased config value.
				"schema_registry_normalize_on_startup": true,
			},
			Expected: map[string]any{
				"schema_registry_always_normalize": true,
			},
		},
		{
			Mode: SyncerModeDeclarative,
			Config: map[string]any{
				// Conflicting values of aliased keys, we perform client side
				// processing to normalize our formation of
				"cloud_storage_graceful_transfer_timeout":    1000,
				"cloud_storage_graceful_transfer_timeout_ms": 2000,
				"delete_retention_ms":                        200,
				"log_retention_ms":                           100,
				"schema_registry_always_normalize":           false,
				"schema_registry_normalize_on_startup":       true,
			},
			Expected: map[string]any{
				"log_retention_ms": 100,
				// "schema_registry_always_normalize":           false, <- Excluded, this is the default
				"cloud_storage_graceful_transfer_timeout_ms": 2000,
			},
		},
	}

	for i, tc := range cases {
		t.Logf("case %d", i)

		configBytes, err := yaml.Marshal(tc.Config)
		require.NoError(t, err)

		cmd := Command()
		args := []string{
			"--users-directory", tc.UsersTXTDir,
			"--redpanda-yaml", redpandaYAMLPath,
			"--bootstrap-yaml", testutils.WriteFile(t, "bootstrap-*.yaml", configBytes),
			"--mode", tc.Mode.String(),
		}
		cmd.SetArgs(args)

		require.NoError(t, cmd.ExecuteContext(ctx))

		actual, err := adminAPIClient.Config(ctx, false)
		require.NoError(t, err)

		// By excluding defaults we'll receive only settings modified by us
		// _plus_ cluster_id. Remove it for the purpose of comparing.
		delete(actual, "cluster_id")

		// NB: Utilize JSON equality as go will fuss about ints vs floats.
		requireJSONEq(t, tc.Expected, actual)
	}
}

func TestSyncUpgradeRegressions(t *testing.T) {
	ctx := context.Background()
	logger := testr.New(t)
	ctx = log.IntoContext(ctx, logger)

	// No auth is easy, only test on a cluster with auth on admin API.
	container, err := redpanda.Run(
		ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.2.4",
	)
	require.NoError(t, err)

	adminAPIAddr, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	adminAPIClient, err := rpadmin.NewAdminAPI([]string{adminAPIAddr}, &rpadmin.NopAuth{}, nil)
	require.NoError(t, err)
	defer adminAPIClient.Close()

	rpkConfigBytes, err := yaml.Marshal(map[string]any{
		"rpk": map[string]any{
			"admin_api": map[string]any{
				"addresses": []string{adminAPIAddr},
				"tls":       nil,
			},
		},
	})
	require.NoError(t, err)

	redpandaYAMLPath := testutils.WriteFile(t, "redpanda-*.yaml", rpkConfigBytes)

	cases := []struct {
		Upgrades []map[string]any
	}{
		{
			Upgrades: []map[string]any{{
				"kafka_nodelete_topics": []any{"audit", "consumer_offsets"},
				"kafka_throughput_control": []map[string]any{
					{"name": "first_group", "client_id": "client1"},
					{"client_id": "consumer-\\d+"},
				},
			}, {
				"kafka_nodelete_topics": []any{"audit"},
				"kafka_throughput_control": []map[string]any{
					{"name": "first_group", "client_id": "client1"},
				},
			}},
		},
	}

	for i, tc := range cases {
		t.Logf("case %d", i)

		for _, upgrade := range tc.Upgrades {
			configBytes, err := yaml.Marshal(upgrade)
			require.NoError(t, err)

			cmd := Command()
			cmd.SetArgs([]string{
				"--redpanda-yaml", redpandaYAMLPath,
				"--bootstrap-yaml", testutils.WriteFile(t, "bootstrap-*.yaml", configBytes),
			})

			require.NotPanics(t, func() {
				require.NoError(t, cmd.ExecuteContext(ctx))

				actual, err := adminAPIClient.Config(ctx, false)
				require.NoError(t, err)

				for key := range upgrade {
					requireJSONEq(t, upgrade[key], actual[key])
				}
			})
		}
	}
}

func requireJSONEq[T any](t *testing.T, expected, actual T) {
	expectedBytes, err := json.Marshal(expected)
	require.NoError(t, err)

	actualBytes, err := json.Marshal(actual)
	require.NoError(t, err)

	require.JSONEq(t, string(expectedBytes), string(actualBytes))
}

func TestSyncSuperusers(t *testing.T) {
	// Check that the syncer works with alternative values for config["superusers"]
	s := Syncer{}

	for _, tc := range []struct {
		name        string
		config      map[string]any
		suTxt       []string
		expectArray bool
	}{
		{
			name:   "no superusers",
			config: map[string]any{},
			suTxt:  []string{"a", "b"},
			// TODO: this should probably insert `a` and `b` as superusers; we don't hit this in v2 at present.
		},
		{
			name:        "[]any superusers",
			config:      map[string]any{superusersEntry: []any{"b", "c"}},
			suTxt:       []string{"a", "b"},
			expectArray: true,
		},
		{
			name:        "[]string superusers",
			config:      map[string]any{superusersEntry: []string{"b", "c"}},
			suTxt:       []string{"a", "b"},
			expectArray: true,
		},
		{
			name:   "bad superusers",
			config: map[string]any{superusersEntry: "broken"},
			suTxt:  []string{"a", "b"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged, _ := s.mergeSuperusers(t.Context(), tc.suTxt, tc.config)
			if tc.expectArray {
				assert.Equal(t, []string{"a", "b", "c"}, merged)
			}
		})
	}
}
