package sidecar

import (
	"context"
	"errors"
	"net/http"
	"net/http/pprof"
	"time"

	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	vectorizedcontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/health"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	unbindTimeout = 10 * time.Minute
)

var (
	schemes = []func(s *runtime.Scheme) error{
		clientgoscheme.AddToScheme,
	}
)

func Command() *cobra.Command {
	var (
		metricsAddr      string
		probeAddr        string
		pprofAddr        string
		clusterNamespace string
		clusterName      string
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
			)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&pprofAddr, "pprof-bind-address", ":8082", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&clusterNamespace, "cluster-namespace", "", "The namespace of the cluster that this sidecar manages.")
	cmd.Flags().StringVar(&clusterName, "cluster-name", "", "The name of the cluster that this sidecar manages.")

	return cmd
}

//nolint:funlen,gocyclo // length looks good
func Run(
	ctx context.Context,
	metricsAddr string,
	probeAddr string,
	clusterNamespace string,
	clusterName string,
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

	checker := &health.HealthChecker{}
	if err := mgr.AddHealthzCheck("cluster", checker.HandleRequest); err != nil {
		setupLog.Error(err, "unable to create health check")
		return err
	}

	filter := decommissioning.FilterOwner(clusterNamespace, clusterName)

	if err := (&vectorizedcontrollers.PVCUnbinderReconciler{
		Client:  mgr.GetClient(),
		Timeout: unbindTimeout,
		Filter: func(pod *corev1.Pod) bool {
			return filter(pod)
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PVCUnbinder")
		return err
	}

	decommissioner := decommissioning.NewStatefulSetDecommissioner(mgr, decommissioning.NewHelmFetcher(mgr), []decommissioning.Option{
		decommissioning.WithFilter(decommissioning.FilterStatefulSetOwner(clusterNamespace, clusterName)),
	}...)

	if err := (&redpandacontrollers.SidecarDecommissionReconciler{
		Client:         mgr.GetClient(),
		Decommissioner: decommissioner,
	}).SetupWithManager(mgr); err != nil {
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
