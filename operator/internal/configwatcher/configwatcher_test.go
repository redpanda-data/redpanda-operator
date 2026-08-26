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
	"runtime"
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

// TestWatchRecoversFromIncompleteSyncPass is a regression test for the
// fail-closed half of the K8S-924 fix: a sync pass that can't read every
// users file withholds the superusers patch — and must then be *retried*,
// because the filesystem events that would re-trigger it may never come.
// kubelet's AtomicWriter fires the handled `..data` CREATE event before it
// removes the symlinks of deleted secret keys and the old payload directory,
// so the pass it triggers can catch a dangling symlink, and the subsequent
// cleanup emits only REMOVE events, which the watch loop ignores. Without the
// watch loop's retry, a secret update that revokes a superuser would leave
// that user privileged until the next update or a pod restart.
//
// The test runs the watcher with watch=true and drives the exact AtomicWriter
// sequence: swap `..data`, then clean up stale symlinks. Recovery must come
// from the watcher itself — the only events the cleanup emits are unhandled —
// so an eventual patch proves the retry path. A dangling symlink present from
// the start makes the initial pass incomplete deterministically.
func TestWatchRecoversFromIncompleteSyncPass(t *testing.T) {
	// The watch loop's platform contract is inotify's: fsnotify's kqueue
	// backend (darwin) opens every directory entry at Add time — a dangling
	// symlink fails the whole Add — and a rename over an existing name emits
	// no CREATE. Production sidecars run on Linux, as does CI.
	if runtime.GOOS != "linux" {
		t.Skipf("the fsnotify watch loop relies on inotify semantics; GOOS=%s uses kqueue", runtime.GOOS)
	}

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

	// kubelet AtomicWriter layout, plus a users file written directly into the
	// directory (local.txt) and a dangling symlink that keeps every pass
	// incomplete until it is removed.
	usersDir := t.TempDir()
	dataDir := filepath.Join(usersDir, "..2026_08_20_11_42_32.0123456789")
	require.NoError(t, os.Mkdir(dataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "users.txt"), []byte(createUserLine("alice", password, "SCRAM-SHA-512")+"\n"+createUserLine("bob", password, "SCRAM-SHA-256")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "more-users.txt"), []byte(createUserLine("carol", password, "SCRAM-SHA-512")), 0o644))
	require.NoError(t, os.Symlink(filepath.Base(dataDir), filepath.Join(usersDir, "..data")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "users.txt"), filepath.Join(usersDir, "users.txt")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "more-users.txt"), filepath.Join(usersDir, "more-users.txt")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "missing.txt"), filepath.Join(usersDir, "dangling.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(usersDir, "local.txt"), []byte(createUserLine("dana", password, "SCRAM-SHA-512")), 0o644))

	configPath := filepath.Join(t.TempDir(), "redpanda.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(createRedpandaYaml(server.URL, bootstrapUser, password, saslMechanism)), 0o644))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	initialized := make(chan struct{})
	watcher := configwatcher.NewConfigWatcher(
		logger,
		true,
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

	// The dangling symlink makes the initial pass — and every retry of it —
	// incomplete: the patch stays withheld.
	time.Sleep(600 * time.Millisecond)
	select {
	case err := <-errCh:
		require.NoError(t, err, "watcher exited instead of watching")
	default:
	}
	require.Empty(t, admin.superuserPatches())

	// Replay AtomicWriter's steps for a secret update that deletes the
	// more-users.txt key (revoking carol): write the new payload, atomically
	// swap `..data` (the only creation event the watch loop handles, and it
	// arrives while stale symlinks still dangle), then clean up. The cleanup
	// emits only REMOVE events, so only the retry can deliver the patch.
	newDataDir := filepath.Join(usersDir, "..2026_08_20_11_45_00.9876543210")
	require.NoError(t, os.Mkdir(newDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newDataDir, "users.txt"), []byte(createUserLine("alice", password, "SCRAM-SHA-512")+"\n"+createUserLine("bob", password, "SCRAM-SHA-256")), 0o644))
	require.NoError(t, os.Symlink(filepath.Base(newDataDir), filepath.Join(usersDir, "..data_tmp")))
	require.NoError(t, os.Rename(filepath.Join(usersDir, "..data_tmp"), filepath.Join(usersDir, "..data")))
	// Hold the AtomicWriter window open long enough that the CREATE-triggered
	// pass deterministically runs against the dangling symlinks (kubelet's
	// window is milliseconds; the ordering is what matters). Without this the
	// cleanup below can race ahead of the event and hand a complete directory
	// to the event-triggered pass, which would let a retry-less watch loop
	// pass the test.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, os.Remove(filepath.Join(usersDir, "more-users.txt")))
	require.NoError(t, os.RemoveAll(dataDir))
	require.NoError(t, os.Remove(filepath.Join(usersDir, "dangling.txt")))

	// The first complete pass patches the post-update union — carol revoked,
	// nobody else touched. Every recorded patch must carry exactly that union:
	// a partial list must never have been written along the way.
	union := []string{bootstrapUser, "alice", "bob", "dana"}
	require.Eventually(t, func() bool { return len(admin.superuserPatches()) > 0 }, 15*time.Second, 25*time.Millisecond,
		"no superusers patch arrived: an incomplete sync pass was never retried")
	for _, patch := range admin.superuserPatches() {
		require.ElementsMatch(t, union, patch)
	}

	// Direct writes are debounced and then synced. Two back-to-back writes
	// still produce exactly one additional patch: either the debounce
	// coalesces them or the second pass hits the equality skip.
	require.NoError(t, os.WriteFile(filepath.Join(usersDir, "local.txt"), []byte(createUserLine("dana", password, "SCRAM-SHA-512")+"\n"+createUserLine("erin", password, "SCRAM-SHA-512")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(usersDir, "local.txt"), []byte(createUserLine("dana", password, "SCRAM-SHA-512")+"\n"+createUserLine("erin", password, "SCRAM-SHA-512")), 0o644))
	require.Eventually(t, func() bool { return len(admin.superuserPatches()) >= 2 }, 15*time.Second, 25*time.Millisecond,
		"direct file write never triggered a sync")
	time.Sleep(700 * time.Millisecond)
	require.Len(t, admin.superuserPatches(), 2)
	require.ElementsMatch(t, append(union, "erin"), admin.superuserPatches()[1])

	cancel()
	<-done
}

// TestSyncAllToleratesNonUsersFileBlobs pins the error classification of a
// sync pass: content that merely fails to parse — including a line too long
// to be a users entry, e.g. a certificate or JSON blob stored under another
// key of the same user-managed secret — must not mark the pass incomplete.
// An incomplete pass withholds the superusers patch, and a blob key would
// stay unparseable on every retry, wedging superusers management forever
// while SCRAM credentials keep syncing.
func TestSyncAllToleratesNonUsersFileBlobs(t *testing.T) {
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

	usersDir := t.TempDir()
	// users.txt carries one valid user plus an oversized-but-parseable
	// malformed line (needs the enlarged scanner buffer to even be read past).
	require.NoError(t, os.WriteFile(filepath.Join(usersDir, "users.txt"), []byte(
		createUserLine("alice", password, "SCRAM-SHA-512")+"\n"+strings.Repeat("b", 100_000)+"\n"+createUserLine("bob", password, "SCRAM-SHA-256"),
	), 0o644))
	// A single line longer than any conceivable users entry: scanning stops
	// with bufio.ErrTooLong, which must count as malformed content, not as a
	// read failure.
	require.NoError(t, os.WriteFile(filepath.Join(usersDir, "cert.blob"), []byte(strings.Repeat("a", 1<<20+1)), 0o644))

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

	require.Eventually(t, func() bool { return len(admin.superuserPatches()) > 0 }, 10*time.Second, 10*time.Millisecond,
		"the blob key wedged the sync pass: superusers were never patched")
	require.ElementsMatch(t, []string{bootstrapUser, "alice", "bob"}, admin.superuserPatches()[0])

	// The pass reports complete — nothing needs retrying — and a rerun over
	// unchanged input hits the equality skip.
	require.True(t, watcher.SyncAll(ctx))
	require.Len(t, admin.superuserPatches(), 1)

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
