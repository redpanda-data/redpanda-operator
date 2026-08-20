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
	"fmt"
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

	w.SyncAll(ctx)

	return w.watchFilesystem(ctx)
}

// SyncAll synchronizes the SCRAM users of every users file in the users
// directory and then updates the superusers cluster configuration once, with
// the union of all files' users.
//
// The superusers property can only be replaced wholesale, so it must only
// ever be written with the complete set. Patching it per file replaces the
// list with a subset, which transiently revokes superuser status from every
// user in any file not yet processed (K8S-924). For the same reason, the
// patch is skipped entirely whenever any users file can't be read.
func (w *ConfigWatcher) SyncAll(ctx context.Context) {
	entries, err := afero.ReadDir(w.fs, w.usersDirectory)
	if err != nil {
		w.log.Error(err, "unable to get user directory files")
		return
	}

	// sync our internal superuser first
	internalSuperuser, password, mechanism := getInternalUser()
	// the internal user should only ever be created once, so don't
	// update its password ever.
	w.syncUser(ctx, internalSuperuser, password, mechanism, false)

	users := []string{internalSuperuser}
	synced := 0
	complete := true

	for _, entry := range entries {
		filePath := path.Join(w.usersDirectory, entry.Name())

		// entry.IsDir is lstat-based and therefore false for symlinks, but
		// kubelet secret mounts contain a `..data` symlink that points at a
		// directory. Stat follows symlinks, so it skips `..data` while still
		// resolving the symlinked users files themselves.
		info, err := w.fs.Stat(filePath)
		if err != nil {
			w.log.Error(err, "unable to stat users file", "file", filePath)
			complete = false
			continue
		}
		if info.IsDir() {
			continue
		}

		fileUsers, err := w.syncUsersFile(ctx, filePath)
		if err != nil {
			w.log.Error(err, "unable to synchronize users file", "file", filePath)
			complete = false
			continue
		}

		synced++
		for _, user := range fileUsers {
			if !slices.Contains(users, user) {
				users = append(users, user)
			}
		}
	}

	if !complete {
		w.log.Info("not setting superusers: some users files could not be read and a partial list would revoke superuser status from the missing users")
		return
	}

	// Don't reduce superusers down to just the internal user when the
	// directory holds no users files at all.
	if synced == 0 {
		return
	}

	w.setSuperusers(ctx, users)
}

func (w *ConfigWatcher) watchFilesystem(ctx context.Context) error {
	if !w.watch {
		<-ctx.Done()
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(w.usersDirectory); err != nil {
		return err
	}

	for {
		select {
		case err := <-watcher.Errors:
			// here we don't return as that'd crash the broker, instead
			// just log the error and move on after some sleep time.
			w.log.Error(err, "watcher returned an error")
			time.Sleep(5 * time.Second)
		case event := <-watcher.Events:
			// Kubernete updates secrets by swapping a symlink named `..data`.
			// We must watch for the CREATE event on this specific symlink.
			if event.Name == w.defaultK8sUserSymlink && event.Has(fsnotify.Create) {
				w.log.Info("Kubernetes secret update detected, synchronizing users", "event", event.String())
				w.SyncAll(ctx)
				continue
			}

			// The original logic in case there is direct file writes.
			if event.Has(fsnotify.Write) && strings.HasSuffix(event.Name, ".txt") {
				w.log.Info("Direct file write detected, synchronizing users", "event", event.String())
				// Resync the whole directory: superusers must always be
				// computed from the union of every users file, never from a
				// single file.
				w.SyncAll(ctx)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// syncUsersFile creates or updates the SCRAM credentials of every user in the
// given users file and returns their names. Users files hold one
// user:password[:mechanism] entry per line.
func (w *ConfigWatcher) syncUsersFile(ctx context.Context, path string) ([]string, error) {
	file, err := w.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	w.log.Info("synchronizing users in file", "file", path)

	var users []string

	scanner := bufio.NewScanner(file)
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

		if !slices.Contains(users, user) {
			users = append(users, user)
		}

		w.syncUser(ctx, user, password, mechanism, true)
	}

	// A read error on an open file (e.g. EISDIR when the "file" is really a
	// symlink to a directory) surfaces here rather than at Open time.
	return users, scanner.Err()
}

func (w *ConfigWatcher) setSuperusers(ctx context.Context, users []string) {
	if w.noSetSuperusers {
		return
	}

	users = normalizeSuperusers(users)

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
			if len(existingUsers) == len(existing) && slices.Equal(normalizeSuperusers(existingUsers), users) {
				w.log.Info("superusers already up to date", "users", users)
				return
			}
		}
	}

	w.log.Info("setting superusers", "users", users)

	if _, err := w.adminClient.PatchClusterConfig(ctx, map[string]any{
		"superusers": users,
	}, []string{}); err != nil {
		w.log.Error(err, "could not set superusers")
	}
}

// normalizeSuperusers de-duplicates and sorts the given user names, matching
// the post-install/upgrade job's handling of the superusers property.
func normalizeSuperusers(users []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(users)))
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
