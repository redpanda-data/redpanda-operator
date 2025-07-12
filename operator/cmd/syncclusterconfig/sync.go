// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package syncclusterconfig contains a re-implementation of the original
// redpanda helm chart's post-upgrade and post-install jobs in go via direct
// connections to the AdminAPI rather than templating out rpk commands.
//
// The original scripts can be found here:
// - https://github.com/redpanda-data/helm-charts/blob/2bf9dd6f09966189cb00a9b5beab49d3ae57b0e1/charts/redpanda/post_install_upgrade_job.go#L109-L164
// - https://github.com/redpanda-data/helm-charts/blob/2bf9dd6f09966189cb00a9b5beab49d3ae57b0e1/charts/redpanda/post_upgrade_job.go#L82-L141
package syncclusterconfig

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	rpkadminapi "github.com/redpanda-data/redpanda/src/go/rpk/pkg/adminapi"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
)

const (
	licenseEnvvar   = "REDPANDA_LICENSE"
	superusersEntry = "superusers"
)

func Command() *cobra.Command {
	var usersDirectoryPath string
	var redpandaYAMLPath string
	var bootstrapYAMLPath string
	mergeMode := "additive"

	cmd := &cobra.Command{
		Use: "sync-cluster-config [--bootstrap-yaml file] [--redpanda-yaml file] [--users-directory file] [--mode mode]",
		Long: fmt.Sprintf(`sync-cluster-config patches a cluster's configuration with values from the provided bootstrap.yaml.
If present and not empty, the $%s environment variable will be set as the cluster's license.
`, licenseEnvvar),
		SilenceUsage: true, // Don't show --help when errors are returned from RunE
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := log.FromContext(ctx)

			syncerMode, err := StringToMode(mergeMode)
			if err != nil {
				return err
			}

			client, err := adminAPIFromRPKConfig(redpandaYAMLPath)
			if err != nil {
				return err
			}

			if license := os.Getenv(licenseEnvvar); license == "" {
				logger.Info(fmt.Sprintf("$%s not set. Skipping license setting...", licenseEnvvar))
			} else {
				logger.Info(fmt.Sprintf("$%s set. Setting license...", licenseEnvvar))
				if err := client.SetLicense(cmd.Context(), strings.NewReader(license)); err != nil {
					return err
				}
			}

			clusterConfig, err := loadBoostrapYAML(bootstrapYAMLPath)
			if err != nil {
				return err
			}

			superusers, err := loadUsersFiles(ctx, usersDirectoryPath)
			if err != nil {
				return err
			}

			syncer := Syncer{Client: client, Mode: syncerMode}

			if _, err := syncer.Sync(ctx, clusterConfig, superusers); err != nil {
				return err
			}

			// NB: remove must be an empty slice NOT nil.
			result, err := client.PatchClusterConfig(ctx, clusterConfig, []string{})
			if err != nil {
				return err
			}

			logger.Info("Updated cluster configuration", "config_version", result.ConfigVersion)

			// NB: It's unclear why this endpoint is being hit. It's preserved
			// as a historical artifact.
			// See also: https://github.com/redpanda-data/redpanda-operator/issues/232
			if err := client.RestartService(ctx, "schema-registry"); err != nil {
				return err
			}

			logger.Info("Successfully restarted schema-registry")

			return nil
		},
	}

	cmd.Flags().StringVar(&usersDirectoryPath, "users-directory", "/etc/secrets/users/", "Path to users directory where secrets are mounted")
	cmd.Flags().StringVar(&redpandaYAMLPath, "redpanda-yaml", "/etc/redpanda/redpanda.yaml", "Path to redpanda.yaml")
	cmd.Flags().StringVar(&bootstrapYAMLPath, "bootstrap-yaml", "/etc/redpanda/.bootstrap.yaml", "Path to .bootstrap.yaml")
	cmd.Flags().StringVar(&mergeMode, "mode", "additive", "Specify mode: additive | declarative")

	return cmd
}

// adminAPIFromRPKConfig utilizes rpk's internal configuration loading to
// connect to the admin API.
// We've opted to re-use rpk's config format as it's already being computed in
// the helm chart and it handles loading authn envvars such as: RPK_USER and
// RPK_PASSWORD.
func adminAPIFromRPKConfig(configPath string) (*rpadmin.AdminAPI, error) {
	fs := afero.NewOsFs()

	params := rpkconfig.Params{ConfigFlag: configPath}

	rpkCfg, err := params.Load(fs)
	if err != nil {
		return nil, err
	}

	return rpkadminapi.NewClient(fs, rpkCfg.VirtualProfile())
}

func loadBoostrapYAML(path string) (map[string]any, error) {
	bootstrapYAML, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config map[string]any
	if err := yaml.Unmarshal(bootstrapYAML, &config); err != nil {
		return nil, err
	}

	return config, nil
}

func loadUsersFiles(ctx context.Context, path string) ([]string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.FromContext(ctx).Info(fmt.Sprintf("users directory doesn't exist; skipping: %q", path))
			return nil, nil
		}
		return nil, err
	}

	users := []string{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := filepath.Join(path, file.Name())

		usersFile, err := os.ReadFile(filename)
		if err != nil {
			log.FromContext(ctx).Info(fmt.Sprintf("Cannot read user file %q: %v. Skipping.", filename, err))
			continue
		}

		users = append(users, LoadUsersFile(ctx, filename, usersFile)...)
	}

	return NormalizeSuperusers(users), nil
}

type SyncerMode int

const (
	SyncerModeAdditive = SyncerMode(iota)
	SyncerModeDeclarative
)

var ErrSyncerMode = errors.New("unrecognised syncer mode")

func StringToMode(m string) (SyncerMode, error) {
	switch m {
	case "additive":
		return SyncerModeAdditive, nil
	case "declarative":
		return SyncerModeDeclarative, nil
	}
	// Let Unwrap identify the concrete error
	return SyncerModeAdditive, fmt.Errorf("cannot parse %s: %w", m, ErrSyncerMode)
}

type Syncer struct {
	Client admin.AdminAPIClient
	// SyncerMode - set to SyncerModeDeclarative to make cluster config syncing fully declarative.
	// The historical behavior has not been, so this is technically a breaking change.
	Mode SyncerMode
	// Function that tests whether two values are identical (for the purposes of requiring a resync).
	// If unset, this will default to reflect.DeepEqual
	EqualityCheck func(key string, desired any, current any) bool
}

type ClusterConfigStatus struct {
	Version      int64
	NeedsRestart bool
	// Hash of the properties that need restart
	PropertiesThatNeedRestartHash string
}

// Sync will compare the current cluster configuration with the desired
// configuration and apply any changes.
//
// If no changes are needed, it will return the highest config version from
// reported by the brokers. If there's a change, it will return the new version
// of the cluster config.
func (s *Syncer) Sync(ctx context.Context, desired map[string]any, superusers []string) (*ClusterConfigStatus, error) {
	equal := s.EqualityCheck
	if equal == nil {
		equal = func(_ string, desired, current any) bool {
			return reflect.DeepEqual(desired, current)
		}
	}
	logger := log.FromContext(ctx)

	s.maybeMergeSuperusers(ctx, desired, superusers)

	current, err := s.Client.Config(ctx, false)
	if err != nil {
		return nil, err
	}

	var added []string
	var changed []string
	// NB: toRemove MUST default to an empty array. Otherwise redpanda will reject our request.
	removed := []string{}
	upsert := maps.Clone(desired)
	status, err := s.Client.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, err
	}
	schema, err := s.Client.ClusterConfigSchema(ctx)
	if err != nil {
		return nil, err
	}
	hashOfConfigsThatNeedRestart, err := hashConfigsThatNeedRestart(desired, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to hash config: %w", err)
	}
	if s.Mode == SyncerModeDeclarative {
		for key, value := range current {
			if currentValue, ok := desired[key]; !ok {
				removed = append(removed, key)
			} else if equal(key, value, currentValue) {
				// No change, leave it be
				delete(upsert, key)
			}
		}

		// We need to explicitly mark unknown or invalid properties to remove, because
		// they will otherwise linger, since AdminAPI.Config does not return those entries.
		// We always send requests for config status to the leader to avoid inconsistencies
		// due to config propagation delays.
		for i := range status {
			for _, invalid := range status[i].Invalid {
				if _, ok := desired[invalid]; !ok {
					removed = append(removed, invalid)
				}
			}
			for _, unknown := range status[i].Unknown {
				if _, ok := desired[unknown]; !ok {
					removed = append(removed, unknown)
				}
			}
		}
	}
	for key, value := range upsert {
		if currentValue, ok := current[key]; !ok {
			added = append(added, key)
		} else if !equal(key, value, currentValue) {
			changed = append(changed, key)
		}
	}
	if len(added) == 0 && len(changed) == 0 && len(removed) == 0 {
		logger.Info("no cluster config changes to apply")
		// find the highest config version
		return &ClusterConfigStatus{
			Version: slices.MaxFunc(status, func(a, b rpadmin.ConfigStatus) int {
				return int(a.ConfigVersion - b.ConfigVersion)
			}).ConfigVersion,
			NeedsRestart:                  slices.ContainsFunc(status, func(s rpadmin.ConfigStatus) bool { return s.Restart }),
			PropertiesThatNeedRestartHash: hashOfConfigsThatNeedRestart,
		}, nil
	}

	{
		var keys []string
		for k := range maps.Keys(desired) {
			keys = append(keys, k)
		}
		sort.Strings(added)
		sort.Strings(changed)
		sort.Strings(removed)
		sort.Strings(keys)
		logger.Info("updating cluster config", "added", added, "removed", removed, "changed", changed, "config", keys)
	}

	result, err := s.Client.PatchClusterConfig(ctx, upsert, removed)
	if err != nil {
		return nil, err
	}

	logger.Info("updated cluster configuration", "config_version", result.ConfigVersion)
	status, err = s.Client.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, err
	}
	return &ClusterConfigStatus{
		Version:                       int64(result.ConfigVersion),
		NeedsRestart:                  slices.ContainsFunc(status, func(s rpadmin.ConfigStatus) bool { return s.Restart }),
		PropertiesThatNeedRestartHash: hashOfConfigsThatNeedRestart,
	}, nil
}

func (s *Syncer) maybeMergeSuperusers(ctx context.Context, config map[string]any, superusers []string) {
	logger := log.FromContext(ctx)

	superusersConfig, ok := config[superusersEntry]
	if !ok {
		// we have no superusers configuration, so don't do anything
		logger.Info("Configuration does not contain a 'superusers' entry. Skipping superusers merge.")
		return
	}

	switch su := superusersConfig.(type) {
	case []string:
		superusers = append(superusers, su...)
	case []any:
		for _, s := range su {
			if cast, ok := s.(string); ok {
				superusers = append(superusers, cast)
			} else {
				logger.Info("Unable to cast value from %T to string, skipping.", s)
			}
		}
	default:
		err := fmt.Errorf("expected superusers entry to be an array of strings, got %T", superusersConfig)
		logger.Error(err, fmt.Sprintf("Unable to cast superusers entry to array. Skipping superusers merge. Type is: %T", superusersConfig))
		return
	}

	config[superusersEntry] = NormalizeSuperusers(superusers)
}

// hashConfigsThatNeedRestart returns a hash of the config that needs the
// cluster to restart when changed.
func hashConfigsThatNeedRestart(m map[string]any, schema rpadmin.ConfigSchema) (string, error) {
	configNeedRestart := make(map[string]any)
	for k, v := range m {
		if meta, ok := schema[k]; ok && meta.NeedsRestart {
			configNeedRestart[k] = v
		}
	}
	hasher := fnv.New64()
	if err := json.NewEncoder(hasher).Encode(configNeedRestart); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
