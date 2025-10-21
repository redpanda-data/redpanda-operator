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
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
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

			client, err := adminAPIFromRPKConfig(ctx, redpandaYAMLPath)
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
func adminAPIFromRPKConfig(ctx context.Context, configPath string) (*rpadmin.AdminAPI, error) {
	fs := afero.NewOsFs()

	params := rpkconfig.Params{ConfigFlag: configPath}

	rpkCfg, err := params.Load(fs)
	if err != nil {
		return nil, err
	}

	return rpkadminapi.NewClient(ctx, fs, rpkCfg.VirtualProfile())
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
		if !os.IsNotExist(err) {
			return nil, err
		}
		log.FromContext(ctx).Info(fmt.Sprintf("users directory doesn't exist; skipping: %q", path))
		return nil, nil
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
	SyncerModeDisabled

	maxSyncMode // Not a valid value, used for static checks.
)

var ErrSyncerMode = errors.New("unrecognised syncer mode")

func (m SyncerMode) String() string {
	switch m {
	case SyncerModeAdditive:
		return "additive"
	case SyncerModeDeclarative:
		return "declarative"
	case SyncerModeDisabled:
		return "disabled"
	default:
		return "SyncerMode(-1)"
	}
}

func StringToMode(m string) (SyncerMode, error) {
	m = strings.ToLower(m)
	for i := 0; i < int(maxSyncMode); i++ {
		if m == SyncerMode(i).String() {
			return SyncerMode(i), nil
		}
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
	logger := log.FromContext(ctx)

	schema, err := s.Client.ClusterConfigSchema(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// 1. Normalize the desired config.
	// **All operations that go to redpanda APIs will operate on the normalized value.**
	// **All operations that go to Kubernetes will operate on the unmodified desired value.**
	normalized := s.normalizeConfig(ctx, schema, superusers, desired)

	// 2. Compute values to be removed.
	status, err := s.Client.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// NB: toRemove MUST default to an empty array. Otherwise redpanda will reject our request.
	toRemove := []string{}

	// We need to explicitly mark unknown or invalid properties to remove, because
	// they will otherwise linger, since AdminAPI.Config does not return those entries.
	// We always send requests for config status to the leader to avoid inconsistencies
	// due to config propagation delays.
	// This list is generally expected to be empty.
	for i := range status {
		for _, invalid := range status[i].Invalid {
			if _, ok := normalized[invalid]; !ok {
				toRemove = append(toRemove, invalid)
			}
		}
		for _, unknown := range status[i].Unknown {
			if _, ok := normalized[unknown]; !ok {
				toRemove = append(toRemove, unknown)
			}
		}
	}

	// If we're operating in declarative mode, we'll remove the any keys that
	// aren't present in our normalized config.
	if s.Mode == SyncerModeDeclarative {
		current, err := s.Client.Config(ctx, false)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		for key := range current {
			if _, ok := normalized[key]; !ok {
				toRemove = append(toRemove, key)
			}
		}
	}

	// Intermediate variable to hold the config result if syncing is actually
	// enabled.
	configVersion := int64(-1)

	// 3. Actually send the patch to redpanda, if enabled.
	if s.Mode == SyncerModeDisabled {
		logger.Info("cluster config synchronization is disabled")
	} else {
		result, err := s.Client.PatchClusterConfig(ctx, normalized, toRemove)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		configVersion = int64(result.ConfigVersion)
		logger.Info("updated cluster configuration", "config_version", result.ConfigVersion, "removed", toRemove)
	}

	// Compute a hash of configs that might trigger a restart of the cluster.
	// This incorrectly uses the desired config instead of the one reflected by
	// the cluster and could technically trigger a restart prior to the config
	// be set but is historical consistent.
	// Use of this hash is slated for removal.
	hashOfConfigsThatNeedRestart, err := hashConfigsThatNeedRestart(desired, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to hash config")
	}

	status, err = s.Client.ClusterConfigStatus(ctx, true)
	if err != nil {
		return nil, err
	}

	needsRestart := false
	for _, s := range status {
		configVersion = max(s.ConfigVersion, configVersion)
		needsRestart = needsRestart || s.Restart
	}

	return &ClusterConfigStatus{
		Version:                       configVersion,
		NeedsRestart:                  needsRestart,
		PropertiesThatNeedRestartHash: hashOfConfigsThatNeedRestart,
	}, nil
}

func (s *Syncer) normalizeConfig(
	ctx context.Context,
	schema rpadmin.ConfigSchema,
	superusers []string,
	desired map[string]any,
) map[string]any {
	logger := log.FromContext(ctx)
	normalized := maps.Clone(desired)

	if superusers, ok := s.mergeSuperusers(ctx, superusers, desired); ok {
		// Conversely, if ok is false, we don't need to unset superusers as it
		// should already be unset.
		normalized["superusers"] = superusers
	}

	// Redpanda's cluster configs can be aliased. The preferred name is the one
	// that's listed in the schema (as opposed to being present in an alias
	// list). We perform client side processing here to normalize the
	// computation of the "toRemove" slice in Declarative mode, otherwise we
	// might accidentally send payloads to redpanda that both set and unset a
	// key at the same time with different names.
	for key, schema := range schema {
		_, hasKey := normalized[key]

		for _, alias := range schema.Aliases {
			aliased, hasAlias := normalized[alias]

			if !hasKey && hasAlias {
				logger.Info("renaming aliased config key", "from", alias, "to", key)
				normalized[key] = aliased
				delete(normalized, alias)
			} else if hasKey && hasAlias {
				logger.Info("dropping aliased key due to non-alias key being provided", "alias", alias, "non-alias", key)
				delete(normalized, alias)
			}
		}
	}

	return normalized
}

func (s *Syncer) mergeSuperusers(ctx context.Context, superusers []string, desired map[string]any) ([]string, bool) {
	desiredSUs, ok := desired["superusers"]
	superusers = append(superusers, coherceToSliceOf[string](ctx, desiredSUs)...)

	// return union(desired['superusers'], superusers) and an indication of
	// whether or not the superusers key should be set or not.
	//
	// **Setting the key or not has different implications depending on s.Mode**
	//
	// We largely key off the presence, or lack thereof, of the superusers key
	// in the desired cluster config. If any superusers are provided via other
	// means, we consider that as an implicit setting of superusers.
	return NormalizeSuperusers(superusers), ok || len(superusers) > 0
}

func coherceToSliceOf[T any](ctx context.Context, in any) []T {
	logger := log.FromContext(ctx)

	switch in := in.(type) {
	case nil:
		return nil

	case []T:
		return in

	case []any:
		out := make([]T, 0, len(in))
		for i := 0; i < len(in); i++ {
			if asT, ok := in[i].(T); ok {
				out = append(out, asT)
			} else {
				logger.Info("unable to cast values from %T to %T; skipping", in[i], asT)
			}
		}
		return out

	default:
		err := errors.Newf("can't coherce %T to %T", in, make([]T, 1))
		logger.Error(err, "unhandled type in coherceToSliceOf")
		return nil
	}
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
