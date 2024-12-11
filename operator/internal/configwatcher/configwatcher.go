// Copyright 2024 Redpanda Data, Inc.
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
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
	"k8s.io/client-go/rest"
)

const (
	defaultConfigPath     = "/var/lib/redpanda.yaml"
	defaultUsersDirectory = "/etc/secret/users"
)

type Option func(c *ConfigWatcher)

// ConfigWatcher replaces the old bash scripts we leveraged for waiting
// for a cluster to become stable and then creating superusers
type ConfigWatcher struct {
	adminClient    *rpadmin.AdminAPI
	configPath     string
	usersDirectory string
	watch          bool
	fs             afero.Fs
	log            logr.Logger

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

	return watcher
}

func (w *ConfigWatcher) Start(ctx context.Context) error {
	params := rpkconfig.Params{ConfigFlag: w.configPath}

	config, err := params.Load(w.fs)
	if err != nil {
		return fmt.Errorf("loading rpk config: %w", err)
	}

	factory := internalclient.NewFactory(&rest.Config{}, nil).WithFS(w.fs)
	client, err := factory.RedpandaAdminClient(ctx, config.VirtualProfile())
	if err != nil {
		return fmt.Errorf("initializing Redpanda admin API client: %w", err)
	}

	w.adminClient = client

	close(w.initialized)

	w.syncInitial(ctx)

	return w.watchFilesystem(ctx)
}

func (w *ConfigWatcher) syncInitial(ctx context.Context) {
	files, err := os.ReadDir(w.usersDirectory)
	if err != nil {
		w.log.Error(err, "unable to get user directory files")
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filePath := path.Join(w.usersDirectory, file.Name())
		w.SyncUsers(ctx, filePath)
	}
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
			file := path.Join(w.usersDirectory, event.Name)
			w.SyncUsers(ctx, file)
		case <-ctx.Done():
			return nil
		}
	}
}

func (w *ConfigWatcher) SyncUsers(ctx context.Context, path string) {
	file, err := w.fs.Open(path)
	if err != nil {
		w.log.Error(err, "unable to open superusers file", "file", path)
		return
	}
	defer file.Close()

	// sync our internal superuser first
	internalSuperuser, password, mechanism := getInternalUser()
	// the internal user should only ever be created once, so don't
	// update its password ever.
	w.syncUser(ctx, internalSuperuser, password, mechanism, false)

	users := []string{internalSuperuser}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		tokens := strings.SplitN(line, ":", 3)
		if len(tokens) != 3 && len(tokens) != 2 {
			w.log.Error(err, "malformed line: %s", line)
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

	w.setSuperusers(ctx, users)
}

func (w *ConfigWatcher) setSuperusers(ctx context.Context, users []string) {
	if _, err := w.adminClient.PatchClusterConfig(ctx, map[string]any{
		"superusers": users,
	}, []string{}); err != nil {
		w.log.Error(err, "could not set superusers")
	}
}

func (w *ConfigWatcher) syncUser(ctx context.Context, user, password, mechanism string, recreate bool) {
	if err := w.adminClient.CreateUser(ctx, user, password, mechanism); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if recreate {
				// the original implementation did an update via Delete + Create, so do that here
				if err := w.adminClient.DeleteUser(ctx, user); err != nil {
					w.log.Error(err, "could not delete user for recreation", "user", user)
					return
				}
				if err := w.adminClient.CreateUser(ctx, user, password, mechanism); err != nil {
					w.log.Error(err, "could not recreate user", "user", user)
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
