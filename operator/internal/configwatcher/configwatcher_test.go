// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configwatcher_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/operator/internal/configwatcher"
)

func TestConfigWatcher(t *testing.T) {
	const user = "user"
	const password = "password"
	const saslMechanism = "SCRAM-SHA-512"

	ctx := context.Background()
	logger := testr.New(t)
	ctx = log.IntoContext(ctx, logger)

	// No auth is easy, only test on a cluster with auth on admin API.
	container, err := redpanda.Run(
		ctx,
		"redpandadata/redpanda:v24.2.4",
		redpanda.WithSuperusers("user"),
		redpanda.WithEnableSASL(),
		redpanda.WithEnableKafkaAuthorization(),
		testcontainers.WithEnv(map[string]string{
			"RP_BOOTSTRAP_USER": fmt.Sprintf("%s:%s:%s", user, password, saslMechanism),
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	adminAPI, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)
	adminClient, err := rpadmin.NewAdminAPI([]string{adminAPI}, &rpadmin.BasicAuth{Username: user, Password: password}, nil)
	require.NoError(t, err)
	defer adminClient.Close()

	t.Setenv("RPK_USER", user)
	t.Setenv("RPK_PASS", password)
	t.Setenv("RPK_SASL_MECHANISM", saslMechanism)

	redpandaYaml := createRedpandaYaml(adminAPI, user, password, saslMechanism)

	users := []string{
		createUserLine("foo", "bar", "SCRAM-SHA-512"),
		createUserLine("baz", "zoiks", "SCRAM-SHA-256"),
		// repeat, make sure it merges and updates to the last
		createUserLine("baz", "bar", "SCRAM-SHA-512"),
		// invalid mechanism, shouldn't fail regardless
		createUserLine("baz", "bar", "INVALID"),
	}

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/var/lib", 0o755))
	require.NoError(t, fs.MkdirAll("/etc/secret/users", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/var/lib/redpanda.yaml", []byte(redpandaYaml), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/etc/secret/users/users.txt", []byte(strings.Join(users, "\n")), 0o644))

	ctx, cancel := context.WithCancel(ctx)

	initialized := make(chan struct{})
	watcher := configwatcher.NewConfigWatcher(logger, false, configwatcher.WithFs(fs), configwatcher.WithInitializedSignal(initialized))

	errCh := make(chan error, 1)
	done := make(chan struct{}, 1)
	go func() {
		if err := watcher.Start(ctx); err != nil {
			select {
			case <-ctx.Done():
				close(done)
				return
			default:
				errCh <- err
			}
		}
		close(done)
	}()

	select {
	case <-initialized:
	case err := <-errCh:
		require.NoError(t, err)
	}

	watcher.SyncUsers(ctx, "/etc/secret/users/users.txt")
	clusterUsers, err := adminClient.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, clusterUsers, 3)

	superuserConfig, err := adminClient.SingleKeyConfig(ctx, "superusers")
	require.NoError(t, err)

	superusers := superuserConfig["superusers"]
	require.Len(t, superusers, 3)

	require.ElementsMatch(t, superusers, clusterUsers)

	// Simulate the bootstrap user Secret being rotated (the operator
	// regenerating it after it was deleted). getInternalUser() reads the
	// password out of RPK_PASS on every sync, so flipping the env var and
	// re-running SyncUsers is enough to exercise the rotation path.
	//
	// Without the fix, SyncUsers would call CreateUser, see "already
	// exists", and return — leaving Redpanda's SCRAM DB pointed at the
	// original password forever. With the fix, it follows up with
	// UpdateUser so the rotated password actually takes effect. We
	// validate via a Kafka SASL handshake because admin-API basic auth
	// is derivable from rpk-config (the only way the configwatcher
	// authenticates) and therefore can't prove the SCRAM DB changed.
	const rotatedPassword = "rotated-password-after-secret-regen"
	t.Setenv("RPK_PASS", rotatedPassword)

	watcher.SyncUsers(ctx, "/etc/secret/users/users.txt")

	kafkaBroker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	require.Error(t, kafkaSASLHandshake(ctx, kafkaBroker, user, password),
		"original password must no longer authenticate after rotation")
	require.NoError(t, kafkaSASLHandshake(ctx, kafkaBroker, user, rotatedPassword),
		"rotated password must authenticate after SyncUsers propagates it")

	cancel()

	select {
	case <-done:
	case err := <-errCh:
		require.NoError(t, err)
	}
}

func createRedpandaYaml(host, user, password, mechanism string) string {
	return fmt.Sprintf(`
rpk:
    admin_api:
        addresses:
            - %q
    kafka_api:
        sasl:
            user: %q
            password: %q
            mechanism: %q
`, host, user, password, mechanism)
}

func createUserLine(user, password, mechanism string) string {
	return user + ":" + password + ":" + mechanism
}

// kafkaSASLHandshake opens a short-lived kgo client against the Kafka listener
// with SCRAM-SHA-512 credentials and issues a Metadata request. The SASL
// handshake runs as part of broker connection setup, so a non-nil error means
// the credentials were rejected (or the broker was unreachable).
func kafkaSASLHandshake(ctx context.Context, broker, user, password string) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker),
		kgo.SASL((&scram.Auth{User: user, Pass: password}).AsSha512Mechanism()),
	)
	if err != nil {
		return err
	}
	defer client.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return client.Ping(pingCtx)
}
