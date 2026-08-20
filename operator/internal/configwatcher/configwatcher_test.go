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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
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

	watcher.SyncAll(ctx)
	clusterUsers, err := adminClient.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, clusterUsers, 3)

	superuserConfig, err := adminClient.SingleKeyConfig(ctx, "superusers")
	require.NoError(t, err)

	superusers := superuserConfig["superusers"]
	require.Len(t, superusers, 3)

	require.ElementsMatch(t, superusers, clusterUsers)

	cancel()

	select {
	case <-done:
	case err := <-errCh:
		require.NoError(t, err)
	}
}

// TestSyncAllPatchesSuperusersOncePerPass is a regression test for K8S-924:
// the watcher used to patch the superusers cluster config once per file with
// only that file's users — including kubelet's `..data` symlink, which parsed
// as an empty users file — so every sync pass transiently replaced the
// superusers list with a subset, revoking superuser status from live clients.
func TestSyncAllPatchesSuperusersOncePerPass(t *testing.T) {
	const bootstrapUser = "admin-bootstrap"
	const password = "password"
	const saslMechanism = "SCRAM-SHA-512"

	logger := testr.New(t)
	ctx := log.IntoContext(context.Background(), logger)

	admin := newFakeAdminAPI()
	server := httptest.NewServer(admin)
	t.Cleanup(server.Close)

	t.Setenv("RPK_USER", bootstrapUser)
	t.Setenv("RPK_PASS", password)
	t.Setenv("RPK_SASL_MECHANISM", saslMechanism)

	// Lay the users directory out exactly like a kubelet secret mount: the
	// data lives in a timestamped hidden directory that a `..data` symlink
	// points at, and every secret key is a symlink through `..data`.
	usersDir := t.TempDir()
	dataDir := filepath.Join(usersDir, "..2026_08_20_11_42_32.0123456789")
	require.NoError(t, os.Mkdir(dataDir, 0o755))
	writeUsersFile := func(name string, lines ...string) {
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, name), []byte(strings.Join(lines, "\n")), 0o644))
	}
	writeUsersFile("users.txt", createUserLine("alice", password, "SCRAM-SHA-512"), createUserLine("bob", password, "SCRAM-SHA-256"))
	writeUsersFile("more-users.txt", createUserLine("carol", password, "SCRAM-SHA-512"))
	require.NoError(t, os.Symlink(filepath.Base(dataDir), filepath.Join(usersDir, "..data")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "users.txt"), filepath.Join(usersDir, "users.txt")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "more-users.txt"), filepath.Join(usersDir, "more-users.txt")))

	configPath := filepath.Join(t.TempDir(), "redpanda.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(createRedpandaYaml(server.URL, bootstrapUser, password, saslMechanism)), 0o644))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	initialized := make(chan struct{})
	watcher := configwatcher.NewConfigWatcher(
		logger,
		false,
		configwatcher.WithFs(afero.NewOsFs()),
		configwatcher.WithRedpandaConfigPath(configPath),
		configwatcher.WithUsersDirectory(usersDir),
		configwatcher.WithInitializedSignal(initialized),
	)

	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := watcher.Start(ctx); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-initialized:
	case err := <-errCh:
		require.NoError(t, err)
	}

	// Start runs the first sync pass asynchronously.
	require.Eventually(t, func() bool { return len(admin.superuserPatches()) > 0 }, 10*time.Second, 10*time.Millisecond)

	// The critical assertion: every superusers patch carries the full union
	// of the internal user and every users file — never a subset, no matter
	// how the pass interleaves with per-file work.
	union := []string{bootstrapUser, "alice", "bob", "carol"}
	for _, patch := range admin.superuserPatches() {
		require.ElementsMatch(t, union, patch)
	}
	require.Len(t, admin.superuserPatches(), 1)

	// A pass over unchanged input doesn't patch again: sidecar restarts no
	// longer bump the cluster config version.
	watcher.SyncAll(ctx)
	require.Len(t, admin.superuserPatches(), 1)

	// Adding a user patches exactly once more, again with the full union.
	writeUsersFile("more-users.txt", createUserLine("carol", password, "SCRAM-SHA-512"), createUserLine("dave", password, "SCRAM-SHA-512"))
	watcher.SyncAll(ctx)
	require.Len(t, admin.superuserPatches(), 2)
	require.ElementsMatch(t, append(union, "dave"), admin.superuserPatches()[1])

	// An unreadable users file aborts the superusers update entirely rather
	// than patching a partial list.
	writeUsersFile("more-users.txt", createUserLine("carol", password, "SCRAM-SHA-512"), createUserLine("dave", password, "SCRAM-SHA-512"), createUserLine("erin", password, "SCRAM-SHA-512"))
	require.NoError(t, os.Symlink(filepath.Join("..data", "missing.txt"), filepath.Join(usersDir, "dangling.txt")))
	watcher.SyncAll(ctx)
	require.Len(t, admin.superuserPatches(), 2)

	require.NoError(t, os.Remove(filepath.Join(usersDir, "dangling.txt")))
	watcher.SyncAll(ctx)
	require.Len(t, admin.superuserPatches(), 3)
	require.ElementsMatch(t, append(union, "dave", "erin"), admin.superuserPatches()[2])

	cancel()
	<-done
}

// fakeAdminAPI implements just enough of the Redpanda admin API for the
// watcher: user creation and cluster config reads/patches, recording every
// value the superusers property is patched with.
type fakeAdminAPI struct {
	mu      sync.Mutex
	config  map[string]any
	patches [][]string
}

func newFakeAdminAPI() *fakeAdminAPI {
	return &fakeAdminAPI{config: map[string]any{}}
}

func (f *fakeAdminAPI) superuserPatches() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.patches)
}

func (f *fakeAdminAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/cluster_config":
		_ = json.NewEncoder(w).Encode(f.config)

	case r.Method == http.MethodPut && r.URL.Path == "/v1/cluster_config":
		var body struct {
			Upsert map[string]any `json:"upsert"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Upsert {
			f.config[key] = value
		}
		if superusers, ok := body.Upsert["superusers"].([]any); ok {
			users := make([]string, 0, len(superusers))
			for _, user := range superusers {
				users = append(users, user.(string))
			}
			f.patches = append(f.patches, users)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"config_version": len(f.patches)})

	case strings.HasPrefix(r.URL.Path, "/v1/security/users"):
		_, _ = w.Write([]byte("{}"))

	default:
		http.NotFound(w, r)
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
