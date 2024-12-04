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
	"net/http"
	"net/http/pprof"
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
		metricsAddr         string
		probeAddr           string
		pprofAddr           string
		clusterNamespace    string
		clusterName         string
		decommissionTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the redpanda sidecar",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Always run a pprof server to facilitate debugging.
			go runPProfServer(ctx, pprofAddr)

			return Run(
				ctx,
				metricsAddr,
				probeAddr,
				clusterNamespace,
				clusterName,
				decommissionTimeout,
			)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&pprofAddr, "pprof-bind-address", ":8082", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&clusterNamespace, "cluster-namespace", "", "The namespace of the cluster that this sidecar manages.")
	cmd.Flags().StringVar(&clusterName, "cluster-name", "", "The name of the cluster that this sidecar manages.")
	cmd.Flags().DurationVar(&decommissionTimeout, "decommission-timeout", 10*time.Second, "The time period to wait before recheck a broker that is being decommissioned.")

	return cmd
}

func Run(
	ctx context.Context,
	metricsAddr string,
	probeAddr string,
	clusterNamespace string,
	clusterName string,
	decommissionTimeout time.Duration,
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
		LeaderElection:          true,
		LeaderElectionID:        clusterName + "." + clusterNamespace + ".redpanda",
		Scheme:                  scheme,
		LeaderElectionNamespace: clusterNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to initialize manager")
		return err
	}

	if err := decommissioning.NewStatefulSetDecommissioner(mgr, decommissioning.NewHelmFetcher(mgr), []decommissioning.Option{
		decommissioning.WithFilter(decommissioning.FilterStatefulSetOwner(clusterNamespace, clusterName)),
		decommissioning.WithRequeueTimeout(decommissionTimeout),
	}...).Setup(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DecommissionReconciler")
		return err
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}

func runPProfServer(ctx context.Context, listenAddr string) {
	logger := ctrl.LoggerFrom(ctx)

	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
	pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	pprofServer := &http.Server{
		Addr:              listenAddr,
		Handler:           pprofMux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	logger.Info("starting pprof server...", "addr", listenAddr)
	if err := pprofServer.ListenAndServe(); err != nil {
		logger.Error(err, "failed to run pprof server")
	}
}
