// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configwatcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/console/backend/pkg/config"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const (
	defaultConfigPath     = "/var/lib/redpanda.yaml"
	defaultUsersDirectory = "/etc/secret/users"

	// syncRetryBase/syncRetryMax bound the retry of an incomplete sync pass.
	// A pass that can't read every users file withholds the superusers patch
	// (see SyncAll) and must be retried, because the events that would
	// resolve it may never come: kubelet's AtomicWriter fires the `..data`
	// CREATE *before* it removes the symlinks of deleted secret keys, and
	// that cleanup emits only REMOVE events, which nothing handles.
	syncRetryBase = 250 * time.Millisecond
	syncRetryMax  = time.Minute

	// writeDebounce coalesces direct-write bursts to users files. A WRITE
	// event can be observed mid-write, and syncing from a truncated read
	// would transiently revoke the file's remaining users. kubelet secret
	// updates don't take this path: the `..data` swap is atomic and happens
	// only after the payload is fully written, so those sync immediately.
	writeDebounce = 500 * time.Millisecond

	// maxUsersFileLineSize is the scanner's line-length ceiling. Any
	// legitimate user:password[:mechanism] line is orders of magnitude
	// smaller, so a longer line means the key holds something else entirely
	// (a certificate, a JSON blob, ...) and is treated as malformed content
	// rather than a read failure — see syncUsersFile.
	maxUsersFileLineSize = 1 << 20
)

type Option func(c *ConfigWatcher)

// ConfigWatcher replaces the old bash scripts we leveraged for waiting
// for a cluster to become stable and then creating superusers
type ConfigWatcher struct {
	adminClient           *rpadmin.AdminAPI
	configPath            string
	usersDirectory        string
	defaultK8sUserSymlink string
	watch                 bool
	fs                    afero.Fs
	log                   logr.Logger

	// When deployed using operator cluster configuration is synced only
	// in main reconciler. ConfigWatcher sidecar should only synchronize users
	// and their passwords.
	// Only when deployed via helm the ConfigWatcher must synchronize superuser
	// cluster configuration list.
	noSetSuperusers bool

	// for testing mostly
	initialized chan struct{}
}

func WithRedpandaConfigPath(path string) Option {
	return func(c *ConfigWatcher) {
		c.configPath = path
	}
}

func WithUsersDirectory(path string) Option {
	return func(c *ConfigWatcher) {
		c.usersDirectory = path
	}
}

func WithFs(fs afero.Fs) Option {
	return func(c *ConfigWatcher) {
		c.fs = fs
	}
}

func WithInitializedSignal(ch chan struct{}) Option {
	return func(c *ConfigWatcher) {
		c.initialized = ch
	}
}

func WithSkipClusterConfigurationSync(noSetSuperusers bool) Option {
	return func(c *ConfigWatcher) {
		c.noSetSuperusers = noSetSuperusers
	}
}

func NewConfigWatcher(log logr.Logger, watch bool, options ...Option) *ConfigWatcher {
	watcher := &ConfigWatcher{
		log:            log,
		watch:          watch,
		configPath:     defaultConfigPath,
		usersDirectory: defaultUsersDirectory,
		fs:             afero.NewOsFs(),
		initialized:    make(chan struct{}),
	}

	for _, option := range options {
		option(watcher)
	}
	watcher.defaultK8sUserSymlink = path.Join(watcher.usersDirectory, "..data")

	return watcher
}

func (w *ConfigWatcher) Start(ctx context.Context) error {
	params := rpkconfig.Params{ConfigFlag: w.configPath}

	config, err := params.Load(w.fs)
	if err != nil {
		return fmt.Errorf("loading rpk config: %w", err)
	}

	factory := internalclient.NewRPKOnlyFactory().WithFS(w.fs)
	client, err := factory.RedpandaAdminClient(ctx, config.VirtualProfile())
	if err != nil {
		return fmt.Errorf("initializing Redpanda admin API client: %w", err)
	}
	defer client.Close()

	w.adminClient = client

	close(w.initialized)

	if !w.watch {
		w.SyncAll(ctx)
		<-ctx.Done()
		return nil
	}

	return w.watchFilesystem(ctx)
}

// SyncAll synchronizes the SCRAM users of every users file in the users
// directory and then updates the superusers cluster configuration once, with
// the union of all files' users.
//
// The superusers property can only be replaced wholesale, so it must only
// ever be written with the complete set. Patching it per file replaces the
// list with a subset, which transiently revokes superuser status from every
// user in any file not yet processed (K8S-924). For the same reason, a pass
// that can't read a users file gives up without patching.
//
// The returned bool reports whether the pass got far enough to make the
// superusers decision from the directory's full contents. A false return
// means the patch was withheld and the pass MUST be retried
// (watchFilesystem re-arms one with backoff), otherwise the unread file's
// users stay in (or out of) the superusers list indefinitely. Only read
// failures give up; malformed file *content* never does — a users secret can
// carry non-users keys forever, and treating those as failures would wedge
// superusers management just as long.
func (w *ConfigWatcher) SyncAll(ctx context.Context) bool {
	entries, err := afero.ReadDir(w.fs, w.usersDirectory)
	if err != nil {
		w.log.Error(err, "unable to get user directory files")
		return false
	}

	// sync our internal superuser first
	internalSuperuser, password, mechanism := getInternalUser()
	// the internal user should only ever be created once, so don't
	// update its password ever.
	w.syncUser(ctx, internalSuperuser, password, mechanism, false)

	users := map[string]struct{}{internalSuperuser: {}}
	synced := 0

	for _, entry := range entries {
		filePath := path.Join(w.usersDirectory, entry.Name())

		// entry.IsDir is lstat-based and therefore false for symlinks, but
		// kubelet secret mounts contain a `..data` symlink that points at a
		// directory. Stat follows symlinks, so it skips `..data` while still
		// resolving the symlinked users files themselves.
		info, err := w.fs.Stat(filePath)
		if err != nil {
			w.log.Error(err, "unable to stat users file; not setting superusers this pass", "file", filePath)
			return false
		}
		if info.IsDir() {
			continue
		}

		fileUsers, err := w.syncUsersFile(ctx, filePath)
		if err != nil {
			w.log.Error(err, "unable to synchronize users file; not setting superusers this pass", "file", filePath)
			return false
		}

		synced++
		for _, user := range fileUsers {
			users[user] = struct{}{}
		}
	}

	// Don't reduce superusers down to just the internal user when the
	// directory holds no users files at all. Deliberate — deleting the whole
	// users secret shouldn't demote every user — but worth a log line: any
	// superusers the cluster still has are now stale.
	if synced == 0 {
		w.log.Info("not setting superusers: the users directory holds no users files; leaving the cluster's current list untouched")
		return true
	}

	w.setSuperusers(ctx, users)
	return true
}

// watchFilesystem runs the initial sync pass and then re-runs it on
// filesystem events until ctx is done.
func (w *ConfigWatcher) watchFilesystem(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(w.usersDirectory); err != nil {
		return err
	}

	// resync schedules the next pass. It is armed by a pass that came up
	// incomplete (retried with backoff; see syncRetryBase) and by direct
	// writes to users files (debounced; see writeDebounce). A nil channel
	// blocks forever, so it only fires while a pass is actually pending.
	var resync <-chan time.Time
	backoff := syncRetryBase

	sync := func() {
		if w.SyncAll(ctx) {
			resync, backoff = nil, syncRetryBase
			return
		}
		w.log.Info("scheduling retry of incomplete users sync", "after", backoff.String())
		resync, backoff = time.After(backoff), min(2*backoff, syncRetryMax)
	}

	sync()

	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-watcher.Errors:
			// here we don't return as that'd crash the broker, instead
			// just log the error and move on after some sleep time.
			w.log.Error(err, "watcher returned an error")
			time.Sleep(5 * time.Second)

		case <-resync:
			sync()

		case event := <-watcher.Events:
			switch {
			// Kubernetes updates secrets by swapping a symlink named `..data`.
			// We must watch for the CREATE event on this specific symlink. The
			// swap is atomic and happens only after the new payload is fully
			// written, so this syncs immediately, without debouncing.
			case event.Name == w.defaultK8sUserSymlink && event.Has(fsnotify.Create):
				w.log.Info("Kubernetes secret update detected, synchronizing users", "event", event.String())
				sync()

			// The original logic in case there is direct file writes. Each
			// write pushes the pass out, so a writer mid-file doesn't produce
			// a pass computed from a torn read.
			case event.Has(fsnotify.Write) && strings.HasSuffix(event.Name, ".txt"):
				w.log.Info("Direct file write detected, scheduling users synchronization", "event", event.String())
				resync = time.After(writeDebounce)
			}
		}
	}
}

// syncUsersFile creates or updates the SCRAM credentials of every user in the
// given users file and returns their names. Users files hold one
// user:password[:mechanism] entry per line.
//
// A non-nil error means the file could not be *read* and the caller's pass is
// incomplete. Content problems — malformed lines, or a line so long it can't
// be a users entry — are logged and skipped but never returned as errors:
// they'd recur on every retry, and the users secret is user-managed, so a
// single odd key must not wedge superusers management indefinitely.
func (w *ConfigWatcher) syncUsersFile(ctx context.Context, path string) ([]string, error) {
	file, err := w.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	w.log.Info("synchronizing users in file", "file", path)

	var users []string

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxUsersFileLineSize)
	for line := 1; scanner.Scan(); line++ {
		tokens := strings.SplitN(scanner.Text(), ":", 3)
		if len(tokens) != 3 && len(tokens) != 2 {
			// NB: only the line number is logged; the line itself contains a
			// password.
			w.log.Info("skipping malformed users file line", "file", path, "line", line)
			continue
		}

		mechanism := config.SASLMechanismScramSHA256

		user, password := tokens[0], tokens[1]
		if len(tokens) == 3 {
			mechanism = tokens[2]
		}

		// NB: not de-duplicated; SyncAll collects users into a set.
		users = append(users, user)

		w.syncUser(ctx, user, password, mechanism, true)
	}

	// A read error on an open file (e.g. EISDIR when the "file" is really a
	// symlink to a directory) surfaces here rather than at Open time.
	if err := scanner.Err(); err != nil {
		// ErrTooLong is a content-level problem, not a read failure: no
		// legitimate users line approaches maxUsersFileLineSize, so this key
		// holds something else entirely. Stop parsing it (the scanner can't
		// resynchronize past the oversized line anyway) but report the file
		// as processed, like any other malformed content.
		if errors.Is(err, bufio.ErrTooLong) {
			w.log.Info("skipping rest of users file: line too long to be a users entry; not a users file?", "file", path, "limit", maxUsersFileLineSize)
			return users, nil
		}
		return users, err
	}
	return users, nil
}

func (w *ConfigWatcher) setSuperusers(ctx context.Context, users map[string]struct{}) {
	if w.noSetSuperusers {
		return
	}

	// Sorted, like the post-install/upgrade job writes the property.
	desired := slices.Sorted(maps.Keys(users))

	// Skip patching when the cluster already has exactly this list, so
	// sidecar restarts don't needlessly bump the cluster config version.
	// Errors are ignored: failing to read the current value must never
	// prevent setting the desired one.
	if current, err := w.adminClient.Config(ctx, false); err == nil {
		if existing, ok := current["superusers"].([]any); ok {
			existingUsers := make([]string, 0, len(existing))
			for _, user := range existing {
				if name, ok := user.(string); ok {
					existingUsers = append(existingUsers, name)
				}
			}
			if len(existingUsers) == len(existing) && slices.Equal(slices.Compact(slices.Sorted(slices.Values(existingUsers))), desired) {
				w.log.Info("superusers already up to date", "users", desired)
				return
			}
		}
	}

	w.log.Info("setting superusers", "users", desired)

	if _, err := w.adminClient.PatchClusterConfig(ctx, map[string]any{
		"superusers": desired,
	}, []string{}); err != nil {
		w.log.Error(err, "could not set superusers")
	}
}

func (w *ConfigWatcher) syncUser(ctx context.Context, user, password, mechanism string, recreate bool) {
	w.log.Info("synchronizing user", "user", user)

	if err := w.adminClient.CreateUser(ctx, user, password, mechanism); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if recreate {
				if err := w.adminClient.UpdateUser(ctx, user, password, mechanism); err != nil {
					w.log.Error(err, "could not update user, falling back to delete/recreate", "user", user)
					if err := w.adminClient.DeleteUser(ctx, user); err != nil {
						w.log.Error(err, "could not delete user for recreation", "user", user)
						return
					}
					if err := w.adminClient.CreateUser(ctx, user, password, mechanism); err != nil {
						w.log.Error(err, "could not recreate user", "user", user)
					}
				}
			}
			return
		}
		w.log.Error(err, "could not create user", "user", user)
	}
}

func getInternalUser() (string, string, string) {
	mechanism := os.Getenv("RPK_SASL_MECHANISM")
	if mechanism == "" {
		mechanism = config.SASLMechanismScramSHA256
	}

	return os.Getenv("RPK_USER"), os.Getenv("RPK_PASS"), mechanism
}
