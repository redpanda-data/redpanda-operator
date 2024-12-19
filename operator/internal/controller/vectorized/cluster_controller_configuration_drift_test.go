// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestDiffIntegration(t *testing.T) {
	const user = "syncer"
	const password = "password"
	const saslMechanism = "SCRAM-SHA-256"

	ctx := context.Background()
	logger := testr.New(t)
	ctx = log.IntoContext(ctx, logger)

	// No auth is easy, only test on a cluster with auth on admin API.
	container, err := redpanda.Run(
		ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.2.4",
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
	defer func() {
		_ = container.Stop(ctx, nil)
	}()

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
		"kafka_rpc_server_tcp_send_buf": 102400,
	}, []string{})
	require.NoError(t, err)

	schema, err := adminAPIClient.ClusterConfigSchema(ctx)
	require.NoError(t, err)

	config, err := adminAPIClient.Config(ctx, true)
	require.NoError(t, err)
	require.Equal(t, config["kafka_rpc_server_tcp_send_buf"].(float64), float64(102400))

	_, drift := hasDrift(logr.Discard(), map[string]any{
		"kafka_rpc_server_tcp_send_buf": "null",
	}, config, schema)
	require.True(t, drift, `expecting to see a drift between integer in redpanda and "null" in desired configuration`)

	_, err = adminAPIClient.PatchClusterConfig(ctx, map[string]any{
		"kafka_rpc_server_tcp_send_buf": nil,
	}, []string{})
	require.NoError(t, err)

	config, err = adminAPIClient.Config(ctx, true)
	require.NoError(t, err)

	_, drift = hasDrift(logr.Discard(), map[string]any{
		"kafka_rpc_server_tcp_send_buf": "null",
	}, config, schema)
	require.False(t, drift, `shall have no drift if we compare null against "null"`)
}

func TestDiffWithNull(t *testing.T) {
	assert := require.New(t)

	schema := map[string]rpadmin.ConfigPropertyMetadata{
		"kafka_rpc_server_tcp_send_buf": {
			Type:         "integer",
			Description:  "Size of the Kafka server TCP receive buffer. If `null`, the property is disabled.",
			Nullable:     true,
			NeedsRestart: true,
			IsSecret:     false,
			Visibility:   "user",
		},
	}

	_, ok := hasDrift(logr.Discard(), map[string]any{
		"kafka_rpc_server_tcp_send_buf": "null",
	}, map[string]any{
		"kafka_rpc_server_tcp_send_buf": nil,
	}, schema)
	assert.False(ok)
}

func TestHasDrift(t *testing.T) {
	assert := require.New(t)

	schema := map[string]rpadmin.ConfigPropertyMetadata{
		"write_caching_default": {
			Type:         "integer",
			Description:  "wait_for_leader_timeout_ms",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "tunable",
			Units:        "ms",
		},
		"cloud_storage_secret_key": {
			Type:         "string",
			Description:  "AWS secret key",
			Nullable:     true,
			NeedsRestart: true,
			IsSecret:     true,
			Visibility:   "user",
		},
	}

	tests := []struct {
		name          string
		desired       map[string]any
		actual        map[string]any
		driftExpected bool
	}{
		{
			name: "cloud_storage_secret_key is omitted",
			desired: map[string]any{
				"cloud_storage_secret_key": "my-key",
				"write_caching_default":    10,
			},
			actual: map[string]any{
				"cloud_storage_secret_key": "[secret]", // Redpanda replaces secret values with [secret]
				"write_caching_default":    10,
			},
			driftExpected: false,
		},
		{
			name: "diff is detected",
			desired: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_background_jobs_quota": 123,
			},
			driftExpected: true,
		},
		{
			name: "diff is detected along other diffs",
			desired: map[string]any{
				"cloud_storage_secret_key":            "my-key",
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_secret_key":            "[secret]",
				"cloud_storage_background_jobs_quota": 123,
			},
			driftExpected: true,
		},
		{
			name: "no diff is detected if all is equal",
			desired: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			actual: map[string]any{
				"cloud_storage_background_jobs_quota": 1337,
			},
			driftExpected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := hasDrift(logr.Discard(), tt.desired, tt.actual, schema)
			assert.Equal(tt.driftExpected, ok)
		})
	}
}
