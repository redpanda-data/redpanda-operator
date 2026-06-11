// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configurator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/internal/rpkprofile"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// The rpk profile inside a v1 broker pod is rendered once by the
// configurator init container and then never refreshed, so it goes stale
// whenever brokers join or leave the cluster (K8S-755). The operator keeps
// the rpk.yaml template in the pod-mounted ConfigMap up to date on every
// reconcile, and the kubelet propagates ConfigMap updates into the running
// pod's mount. This sidecar command closes the gap: it re-renders the
// profile from the mounted template whenever the mount changes, without
// restarting the pod. The watch loop is shared with the v2 sidecar via
// the rpkprofile package.

// WatchRPKProfileCommand returns the `rpk-profile-watcher` subcommand. It is
// run as a lightweight sidecar in v1 (vectorized) broker pods, sharing the
// configurator's CONFIG_SOURCE_DIR / RPK_PROFILE_DESTINATION env contract and
// volume mounts.
func WatchRPKProfileCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rpk-profile-watcher",
		Short: "Keep the pod-local rpk profile in sync with the operator-managed ConfigMap",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sourceDir := os.Getenv(configSourceDirEnvVar)
			destination := os.Getenv(rpkProfileDestinationEnvVar)
			if sourceDir == "" || destination == "" {
				return fmt.Errorf("%s and %s must be set", configSourceDirEnvVar, rpkProfileDestinationEnvVar)
			}
			return watchRPKProfile(cmd.Context(), sourceDir, destination, rpkprofile.DefaultResyncPeriod)
		},
	}
}

// watchRPKProfile runs the shared watch loop with the v1 render: expand the
// ConfigMap-provided rpk.yaml template (with its env-var fixups, e.g. SASL
// credentials) into the pod-local rpk profile — the same render the
// configurator init container performs at pod startup.
func watchRPKProfile(ctx context.Context, sourceDir, destination string, resync time.Duration) error {
	log := ctrl.Log.WithName("rpk-profile-watcher")
	return rpkprofile.WatchAndRender(ctx, log, sourceDir, resync, func(ctx context.Context) error {
		in := filepath.Join(sourceDir, clusterconfiguration.RPKProfileYamlFile)
		fixups := filepath.Join(sourceDir, clusterconfiguration.RPKProfileYamlFixupFile)
		return TemplateRPKProfileYaml(ctx, in, destination, fixups)
	})
}
