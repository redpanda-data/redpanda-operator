// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package sidecar

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	rpkadminapi "github.com/redpanda-data/redpanda/src/go/rpk/pkg/adminapi"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/redpanda-data/redpanda-operator/operator/internal/configwatcher"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/pflagutil"
)

// +kubebuilder:rbac:groups=coordination.k8s.io,namespace=default,resources=leases,verbs=get;list;watch;create;update;patch;delete

var schemes = []func(s *runtime.Scheme) error{
	clientgoscheme.AddToScheme,
}

func Command() *cobra.Command {
	var (
		metricsAddr                string
		probeAddr                  string
		brokerProbeAddr            string
		pprofAddr                  string
		clusterNamespace           string
		clusterName                string
		decommissionRequeueTimeout time.Duration
		decommissionVoteInterval   time.Duration
		decommissionMaxVoteCount   int
		redpandaYAMLPath           string
		usersDirectoryPath         string
		watchUsers                 bool
		noSetSuperusers            bool
		runDecommissioner          bool
		runBrokerProbe             bool
		brokerProbeShutdownTimeout time.Duration
		brokerProbeBrokerURL       string
		runUnbinder                bool
		unbinderTimeout            time.Duration
		selector                   pflagutil.LabelSelectorValue
		panicAfter                 time.Duration
	)

	cmd := &cobra.Command{
		Use:   "sidecar",
		Short: "Run the redpanda sidecar",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			return Run(
				ctx,
				metricsAddr,
				probeAddr,
				brokerProbeAddr,
				pprofAddr,
				clusterNamespace,
				clusterName,
				decommissionRequeueTimeout,
				decommissionVoteInterval,
				decommissionMaxVoteCount,
				redpandaYAMLPath,
				usersDirectoryPath,
				watchUsers,
				noSetSuperusers,
				runDecommissioner,
				runBrokerProbe,
				brokerProbeShutdownTimeout,
				brokerProbeBrokerURL,
				runUnbinder,
				unbinderTimeout,
				selector.Selector,
				panicAfter,
			)
		},
	}

	// runtime flags
	cmd.Flags().StringVar(&metricsAddr, "runtime-metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "runtime-health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&pprofAddr, "runtime-pprof-bind-address", ":8092", "The address the metric endpoint binds to.")

	// rpk flags
	cmd.Flags().StringVar(&redpandaYAMLPath, "redpanda-yaml", "/etc/redpanda/redpanda.yaml", "Path to redpanda.yaml whose rpk stanza will be used for connecting to a Redpanda cluster.")

	// cluster flags
	cmd.Flags().StringVar(&clusterNamespace, "redpanda-cluster-namespace", "", "The namespace of the cluster that this sidecar manages.")
	cmd.Flags().StringVar(&clusterName, "redpanda-cluster-name", "", "The name of the cluster that this sidecar manages.")
	cmd.Flags().Var(&selector, "selector", "Kubernetes label selector that will filter objects to be considered by the all controllers run by the sidecar.")

	// decommission flags
	cmd.Flags().BoolVar(&runDecommissioner, "run-decommissioner", false, "Specifies if the sidecar should run the broker decommissioner.")
	cmd.Flags().DurationVar(&decommissionRequeueTimeout, "decommission-requeue-timeout", 10*time.Second, "The time period to wait before rechecking a broker that is being decommissioned.")
	cmd.Flags().DurationVar(&decommissionVoteInterval, "decommission-vote-interval", 30*time.Second, "The time period between incrementing decommission vote counts since the last decommission conditions were met.")
	cmd.Flags().IntVar(&decommissionMaxVoteCount, "decommission-vote-count", 2, "The number of times that a vote must be tallied when a resource meets decommission conditions for it to actually be decommissioned.")

	// users flags
	cmd.Flags().BoolVar(&watchUsers, "watch-users", false, "Specifies if the sidecar should watch and configure superusers based on a mounted users file.")
	cmd.Flags().BoolVar(&noSetSuperusers, "no-set-superusers", false, "Specifies if the sidecar should sync the superuser cluster configuration with watched users or not")
	cmd.Flags().StringVar(&usersDirectoryPath, "users-directory", "/etc/secrets/users/", "Path to users directory where secrets are mounted.")
	cmd.Flags().MarkHidden("no-set-superusers") // nolint:gosec

	// broker probe flags
	cmd.Flags().BoolVar(&runBrokerProbe, "run-broker-probe", false, "Specifies if the sidecar should run the health probe.")
	cmd.Flags().StringVar(&brokerProbeAddr, "broker-probe-bind-address", ":8093", "The address the broker probe endpoint binds to.")
	cmd.Flags().DurationVar(&brokerProbeShutdownTimeout, "broker-probe-shutdown-timeout", 10*time.Second, "The time period to wait to gracefully shutdown the broker probe before terminating.")
	cmd.Flags().StringVar(&brokerProbeBrokerURL, "broker-probe-broker-url", "", "The URL of the broker instance this sidecar is for.")

	// unbinder flags
	cmd.Flags().BoolVar(&runUnbinder, "run-pvc-unbinder", false, "Specifies if the PVC unbinder should be run.")
	cmd.Flags().DurationVar(&unbinderTimeout, "pvc-unbinder-timeout", 60*time.Second, "The time period to wait before removing any unbound PVCs.")

	// Internal use flags.
	cmd.Flags().DurationVar(&panicAfter, "panic-after", 0, "If non-zero, will trigger an unhandled panic after the specified time resulting in a process crash.")
	_ = cmd.Flags().MarkHidden("panic-after")

	return cmd
}

func Run(
	ctx context.Context,
	metricsAddr string,
	probeAddr string,
	brokerProbeAddr string,
	pprofAddr string,
	clusterNamespace string,
	clusterName string,
	decommissionRequeueTimeout time.Duration,
	decommissionVoteInterval time.Duration,
	decommissionMaxVoteCount int,
	redpandaYAMLPath string,
	usersDirectoryPath string,
	watchUsers bool,
	noSetSuperusers bool,
	runDecommissioner bool,
	runBrokerProbe bool,
	brokerProbeShutdownTimeout time.Duration,
	brokerProbeBrokerURL string,
	runUnbinder bool,
	unbinderTimeout time.Duration,
	selector labels.Selector,
	panicAfter time.Duration,
) error {
	setupLog := ctrl.LoggerFrom(ctx).WithName("setup")

	// Required arguments check, in sidecar mode these MUST be specified to
	// ensure the sidecar only affects the helm deployment that's deployed it.

	if clusterNamespace == "" {
		err := errors.New("must specify a cluster-namespace parameter")
		setupLog.Error(err, "no cluster namespace provided")
		return err
	}

	if clusterName == "" {
		err := errors.New("must specify a cluster-name parameter")
		setupLog.Error(err, "no cluster name provided")
		return err
	}

	if selector == nil || selector.Empty() {
		// Use a sensible default that's about as correct than the previous
		// hard coded values. Hardcoding of name=redpanda is incorrect when
		// nameoverride is used.
		var err error
		selector, err = labels.Parse(fmt.Sprintf(
			"apps.kubernetes.io/component,app.kubernetes.io/name=redpanda,app.kubernetes.io/instance=%s",
			clusterName,
		))
		if err != nil {
			panic(err)
		}
	}

	scheme := runtime.NewScheme()

	for _, fn := range schemes {
		utilruntime.Must(fn(scheme))
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics:                 metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:  probeAddr,
		PprofBindAddress:        pprofAddr,
		LeaderElection:          true,
		LeaderElectionID:        clusterName + "." + clusterNamespace + ".redpanda",
		Scheme:                  scheme,
		LeaderElectionNamespace: clusterNamespace,
		Cache: cache.Options{
			// Only watch the specified namespace, we don't have permissions for watch at the ClusterScope.
			DefaultNamespaces: map[string]cache.Config{
				clusterNamespace: {},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize manager")
		return err
	}

	if runDecommissioner {
		setupLog.Info("broker decommissioner enabled", "namespace", clusterNamespace, "cluster", clusterName, "selector", selector)

		fs := afero.NewOsFs()

		params := rpkconfig.Params{ConfigFlag: redpandaYAMLPath}

		config, err := params.Load(afero.NewOsFs())
		if err != nil {
			return err
		}

		if err := decommissioning.NewStatefulSetDecommissioner(
			mgr,
			func(ctx context.Context, _ *appsv1.StatefulSet) (*rpadmin.AdminAPI, error) {
				// Always use the config that's loaded from redpanda.yaml, in
				// sidecar mode no other STS's should be watched.
				return rpkadminapi.NewClient(ctx, fs, config.VirtualProfile())
			},
			decommissioning.WithSelector(selector),
			decommissioning.WithRequeueTimeout(decommissionRequeueTimeout),
			decommissioning.WithDelayedCacheInterval(decommissionVoteInterval),
			decommissioning.WithDelayedCacheMaxCount(decommissionMaxVoteCount),
		).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "StatefulSetDecommissioner")
			return err
		}
	}

	if runUnbinder {
		setupLog.Info("PVC unbinder enabled", "namespace", clusterNamespace, "selector", selector)

		if err := (&pvcunbinder.Controller{
			Client:   mgr.GetClient(),
			Timeout:  unbinderTimeout,
			Selector: selector,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PVCUnbinder")
			return err
		}
	}

	if runBrokerProbe {
		if brokerProbeBrokerURL == "" {
			err := errors.New("must specify -broker-probe-broker-url to run the broker probe")
			setupLog.Error(err, "no broker URL provided")
			return err
		}

		server, err := probes.NewServer(probes.Config{
			Prober: probes.NewProber(
				internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()),
				redpandaYAMLPath,
				probes.WithLogger(mgr.GetLogger().WithName("Prober")),
			),
			ShutdownTimeout: brokerProbeShutdownTimeout,
			URL:             brokerProbeBrokerURL,
			Address:         brokerProbeAddr,
			Logger:          mgr.GetLogger().WithName("ProbeServer"),
		})
		if err != nil {
			setupLog.Error(err, "unable to create health probe server")
			return err
		}
		if err := mgr.Add(server); err != nil {
			setupLog.Error(err, "unable to run health probe server")
			return err
		}
	}

	if watchUsers {
		watcher := configwatcher.NewConfigWatcher(mgr.GetLogger(), true,
			configwatcher.WithRedpandaConfigPath(redpandaYAMLPath),
			configwatcher.WithUsersDirectory(usersDirectoryPath),
			configwatcher.WithSkipClusterConfigurationSync(noSetSuperusers),
		)
		if err := mgr.Add(watcher); err != nil {
			setupLog.Error(err, "unable to run config watcher")
			return err
		}
	}

	if panicAfter > 0 {
		go func() {
			time.Sleep(panicAfter)

			panic(fmt.Sprintf("unhandled panic triggered by --panic-after=%s", panicAfter))
		}()
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
