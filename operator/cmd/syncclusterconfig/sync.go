// Copyright 2024 Redpanda Data, Inc.
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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	rpkadminapi "github.com/redpanda-data/redpanda/src/go/rpk/pkg/adminapi"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	licenseEnvvar   = "REDPANDA_LICENSE"
	superusersEntry = "superusers"
)

func Command() *cobra.Command {
	var usersDirectoryPath string
	var redpandaYAMLPath string
	var bootstrapYAMLPath string

	cmd := &cobra.Command{
		Use: "sync-cluster-config [--bootstrap-yaml file] [--redpanda-yaml file] [--users-directory file]",
		Long: fmt.Sprintf(`sync-cluster-config patches a cluster's configuration with values from the provided bootstrap.yaml.
If present and not empty, the $%s environment variable will be set as the cluster's license.
`, licenseEnvvar),
		SilenceUsage: true, // Don't show --help when errors are returned from RunE
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := log.FromContext(ctx)

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

			usersTXTs, err := loadUsersFiles(ctx, usersDirectoryPath)
			if err != nil {
				return err
			}

			syncer := Syncer{Client: client}

			if err := syncer.Sync(ctx, clusterConfig, usersTXTs); err != nil {
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

func loadUsersFiles(ctx context.Context, path string) (map[string][]byte, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.FromContext(ctx).Info(fmt.Sprintf("users directory doesn't exist; skipping: %q", path))
			return nil, nil
		}
		return nil, err
	}

	users := map[string][]byte{}

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

		users[file.Name()] = usersFile
	}

	return users, nil
}

func loadUsersFile(ctx context.Context, filename string, usersFile []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(usersFile))

	users := []string{}

	i := 0
	for scanner.Scan() {
		i++

		line := scanner.Text()
		tokens := strings.Split(line, ":")
		if len(tokens) != 2 && len(tokens) != 3 {
			log.FromContext(ctx).Info(fmt.Sprintf("Skipping malformatted line number %d in file %q", i, filename))
			continue
		}
		users = append(users, tokens[0])
	}

	return users
}

// normalizeSuperusers de-duplicates and sorts the superusers
func normalizeSuperusers(entries []string) []string {
	var sorted sort.StringSlice

	unique := make(map[string]struct{})
	for _, value := range entries {
		if _, ok := unique[value]; !ok {
			sorted = append(sorted, value)
		}
		unique[value] = struct{}{}
	}

	sorted.Sort()

	return sorted
}

func mapConvertibleTo[T any](logger logr.Logger, array []any) []T {
	var v T

	converted := []T{}
	for _, value := range array {
		if cast, ok := value.(T); ok {
			converted = append(converted, cast)
		} else {
			logger.Info("Unable to cast value from %T to %T, skipping.", value, v)
		}
	}

	return converted
}

type Syncer struct {
	Client *rpadmin.AdminAPI
}

func (s *Syncer) Sync(ctx context.Context, desired map[string]any, usersTXT map[string][]byte) error {
	logger := log.FromContext(ctx)

	s.maybeMergeSuperusers(ctx, desired, usersTXT)

	current, err := s.Client.Config(ctx, false)
	if err != nil {
		return err
	}

	var added []string
	var changed []string
	// NB: toRemove MUST default to an empty array. Otherwise redpanda will reject our request.
	removed := []string{}

	// TODO: Uncomment this block to make cluster config syncing fully
	// declarative. The historical behavior has not been, so this is
	// technically a breaking change.
	// for key := range current {
	// 	if _, ok := desired[key]; !ok {
	// 		removed = append(removed, key)
	// 	}
	// }

	for key, value := range desired {
		if currentValue, ok := current[key]; !ok {
			added = append(added, key)
		} else if !reflect.DeepEqual(value, currentValue) {
			changed = append(changed, key)
		}
	}

	logger.Info("updating cluster config", "added", added, "removed", removed, "changed", changed, "config", desired)

	result, err := s.Client.PatchClusterConfig(ctx, desired, removed)
	if err != nil {
		return err
	}

	logger.Info("updated cluster configuration", "config_version", result.ConfigVersion)

	return nil
}

func (s *Syncer) maybeMergeSuperusers(ctx context.Context, config map[string]any, usersTXT map[string][]byte) {
	logger := log.FromContext(ctx)

	if len(usersTXT) == 0 {
		logger.Info("usersTXT not specified or empty. Skipping superusers merge.")
	}

	superusers := []string{}
	for name, content := range usersTXT {
		superusers = append(superusers, loadUsersFile(ctx, name, content)...)
	}

	superusersConfig, ok := config[superusersEntry]
	if !ok {
		// we have no superusers configuration, so don't do anything
		logger.Info("Configuration does not contain a 'superusers' entry. Skipping superusers merge.")
		return
	}

	superusersAny, ok := superusersConfig.([]any)
	if !ok {
		logger.Info(fmt.Sprintf("Unable to cast superusers entry to array. Skipping superusers merge. Type is: %T", superusersConfig))
		return
	}

	config[superusersEntry] = normalizeSuperusers(append(superusers, mapConvertibleTo[string](logger, superusersAny)...))
}
