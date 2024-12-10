// Copyright 2024 Redpanda Data, Inc.
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
	"errors"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/redpanda-data/redpanda-operator/operator/internal/decommissioning"
)

var schemes = []func(s *runtime.Scheme) error{
	clientgoscheme.AddToScheme,
}

func Command() *cobra.Command {
	var (
		metricsAddr                string
		probeAddr                  string
		pprofAddr                  string
		clusterNamespace           string
		clusterName                string
		decommissionRequeueTimeout time.Duration
		decommissionVoteInterval   time.Duration
		decommissionMaxVoteCount   int
		redpandaYAMLPath           string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the redpanda sidecar",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			return Run(
				ctx,
				metricsAddr,
				probeAddr,
				pprofAddr,
				clusterNamespace,
				clusterName,
				decommissionRequeueTimeout,
				decommissionVoteInterval,
				decommissionMaxVoteCount,
				redpandaYAMLPath,
			)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&pprofAddr, "pprof-bind-address", ":8082", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&clusterNamespace, "redpanda-cluster-namespace", "", "The namespace of the cluster that this sidecar manages.")
	cmd.Flags().StringVar(&clusterName, "redpanda-cluster-name", "", "The name of the cluster that this sidecar manages.")
	cmd.Flags().DurationVar(&decommissionRequeueTimeout, "decommission-requeue-timeout", 10*time.Second, "The time period to wait before rechecking a broker that is being decommissioned.")
	cmd.Flags().DurationVar(&decommissionVoteInterval, "decommission-vote-interval", 30*time.Second, "The time period between incrementing decommission vote counts since the last decommission conditions were met.")
	cmd.Flags().IntVar(&decommissionMaxVoteCount, "decommission-vote-count", 2, "The number of times that a vote must be tallied when a resource meets decommission conditions for it to actually be decommissioned.")
	cmd.Flags().StringVar(&redpandaYAMLPath, "redpanda-yaml", "/etc/redpanda/redpanda.yaml", "Path to redpanda.yaml whose rpk stanza will be used for connecting to a Redpanda cluster.")

	return cmd
}

func Run(
	ctx context.Context,
	metricsAddr string,
	probeAddr string,
	pprofAddr string,
	clusterNamespace string,
	clusterName string,
	decommissionRequeueTimeout time.Duration,
	decommissionVoteInterval time.Duration,
	decommissionMaxVoteCount int,
	redpandaYAMLPath string,
) error {
	setupLog := ctrl.LoggerFrom(ctx).WithName("setup")

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
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize manager")
		return err
	}

	fetcher := decommissioning.NewChainedFetcher(
		// prefer RPK profile first and then move on to fetch from helm values
		decommissioning.NewRPKProfileFetcher(redpandaYAMLPath),
		decommissioning.NewHelmFetcher(mgr),
	)

	if err := decommissioning.NewStatefulSetDecommissioner(mgr, fetcher, []decommissioning.Option{
		decommissioning.WithFilter(decommissioning.FilterStatefulSetOwner(clusterNamespace, clusterName)),
		decommissioning.WithRequeueTimeout(decommissionRequeueTimeout),
		decommissioning.WithDelayedCacheInterval(decommissionVoteInterval),
		decommissioning.WithDelayedCacheMaxCount(decommissionMaxVoteCount),
	}...).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StatefulSetDecommissioner")
		return err
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
