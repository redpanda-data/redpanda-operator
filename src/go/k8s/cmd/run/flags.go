// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"fmt"
	"strings"
	"time"

	"github.com/fluxcd/pkg/runtime/client"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/spf13/cobra"
)

type flags struct {
	clusterDomain               string
	metricsAddr                 string
	probeAddr                   string
	pprofAddr                   string
	enableLeaderElection        bool
	webhookEnabled              bool
	configuratorBaseImage       string
	configuratorTag             string
	configuratorImagePullPolicy string
	decommissionWaitInterval    time.Duration
	metricsTimeout              time.Duration
	restrictToRedpandaVersion   string
	namespace                   string
	eventsAddr                  string
	additionalControllers       []string
	operatorMode                bool
	enableHelmControllers       bool
	debug                       bool
	ghostbuster                 bool
	unbindPVCsAfter             time.Duration
	autoDeletePVCs              bool

	// globals for v1
	superUsersPrefix          string
	allowConsoleAnyNamespace  bool
	allowDownscalingInWebhook bool

	// flux stuff
	clientOptions  client.Options
	kubeConfigOpts client.KubeConfigOptions
	logOptions     logger.Options
}

func (f *flags) register(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringVar(&f.eventsAddr, "events-addr", "", "The address of the events receiver.")
	cmd.Flags().StringVar(&f.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&f.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&f.pprofAddr, "pprof-bind-address", ":8082", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&f.clusterDomain, "cluster-domain", "cluster.local", "Set the Kubernetes local domain (Kubelet's --cluster-domain)")
	cmd.Flags().BoolVar(&f.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().BoolVar(&f.webhookEnabled, "webhook-enabled", false, "Enable webhook Manager")
	cmd.Flags().StringVar(&f.configuratorBaseImage, "configurator-base-image", defaultConfiguratorContainerImage, "Set the configurator base image")
	cmd.Flags().StringVar(&f.configuratorTag, "configurator-tag", "latest", "Set the configurator tag")
	cmd.Flags().StringVar(&f.configuratorImagePullPolicy, "configurator-image-pull-policy", "Always", "Set the configurator image pull policy")
	cmd.Flags().DurationVar(&f.decommissionWaitInterval, "decommission-wait-interval", 8*time.Second, "Set the time to wait for a node decommission to happen in the cluster")
	cmd.Flags().DurationVar(&f.metricsTimeout, "metrics-timeout", 8*time.Second, "Set the timeout for a checking metrics Admin API endpoint. If set to 0, then the 2 seconds default will be used")
	cmd.Flags().StringVar(&f.restrictToRedpandaVersion, "restrict-redpanda-version", "", "Restrict management of clusters to those with this version")
	cmd.Flags().BoolVar(&f.debug, "debug", false, "Set to enable debugging")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", "If namespace is set to not empty value, it changes scope of Redpanda operator to work in single namespace")
	cmd.Flags().StringSliceVar(&f.additionalControllers, "additional-controllers", []string{""}, fmt.Sprintf("which controllers to run, available: all, %s", strings.Join(availableControllers, ", ")))
	cmd.Flags().BoolVar(&f.operatorMode, "operator-mode", true, "enables to run as an operator, setting this to false will disable cluster (deprecated), redpanda resources reconciliation.")
	cmd.Flags().BoolVar(&f.enableHelmControllers, "enable-helm-controllers", true, "if a namespace is defined and operator mode is true, this enables the use of helm controllers to manage fluxcd helm resources.")
	cmd.Flags().DurationVar(&f.unbindPVCsAfter, "unbind-pvcs-after", 0, "if not zero, runs the PVCUnbinder controller which attempts to 'unbind' the PVCs' of Pods that are Pending for longer than the given duration")
	cmd.Flags().BoolVar(&f.autoDeletePVCs, "auto-delete-pvcs", false, "Use StatefulSet PersistentVolumeClaimRetentionPolicy to auto delete PVCs on scale down and Cluster resource delete.")

	// v1 globals
	cmd.Flags().StringVar(&f.superUsersPrefix, "superusers-prefix", "", "Prefix to add in username of superusers managed by operator. This will only affect new clusters, enabling this will not add prefix to existing clusters (alpha feature)")
	cmd.Flags().BoolVar(&f.allowConsoleAnyNamespace, "allow-console-any-ns", false, "Allow to create Console in any namespace. Allowing this copies Redpanda SchemaRegistry TLS Secret to namespace (alpha feature)")
	cmd.Flags().BoolVar(&f.allowDownscalingInWebhook, "allow-downscaling", true, "Allow to reduce the number of replicas in existing clusters")

	// flux stuff
	f.logOptions.BindFlags(cmd.Flags())
	f.clientOptions.BindFlags(cmd.Flags())
	f.kubeConfigOpts.BindFlags(cmd.Flags())

	// hidden
	cmd.Flags().BoolVar(&f.ghostbuster, "unsafe-decommission-failed-brokers", false, "Set to enable decommissioning a failed broker that is configured but does not exist in the StatefulSet (ghost broker). This may result in invalidating valid data")
	cmd.Flags().Bool("allow-pvc-deletion", false, "Deprecated: Ignored if specified")
	_ = cmd.Flags().MarkHidden("unsafe-decommission-failed-brokers")
	_ = cmd.Flags().MarkHidden("allow-pvc-deletion")

	return cmd
}
