// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/license"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/watcher"
	"github.com/redpanda-data/redpanda-operator/pkg/pflagutil"
)

// NB: these annotations are necessary because we want the ability to manager service accounts
// and secrets for generating long-lived kubeconfigs in our peer clusters

//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;patch

type RaftCluster struct {
	Name    string
	Address string
}

type MulticlusterOptions struct {
	Name                   string
	Address                string
	Peers                  []RaftCluster
	ElectionTimeout        time.Duration
	HeartbeatInterval      time.Duration
	CAFile                 string
	PrivateKeyFile         string
	CertificateFile        string
	KubernetesAPIServer    string
	KubeconfigNamespace    string
	KubeconfigName         string
	BaseImage              string
	BaseTag                string
	HealthProbeBindAddress string
	MetricsBindAddress     string
	MetricsCertPath        string
	MetricsKeyPath         string
	WebhookCertPath        string
	WebhookKeyPath         string

	LeaderElectionID            string
	LeaderElectionNamespace     string
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration

	peersStrings []string

	LicenseFilePath string

	UnbindPVCsAfter  time.Duration
	AllowPVRebinding bool
	UnbinderSelector pflagutil.LabelSelectorValue

	ClusterConnectionTimeout time.Duration
	ReconcileTimeout         time.Duration
}

func (o *MulticlusterOptions) validate() error {
	if o.Name == "" {
		return errors.New("name must be specified")
	}
	if o.Address == "" {
		return errors.New("address must be specified")
	}
	if len(o.CAFile) == 0 {
		return errors.New("ca must be specified")
	}
	if len(o.PrivateKeyFile) == 0 {
		return errors.New("private key must be specified")
	}
	if len(o.CertificateFile) == 0 {
		return errors.New("certificate must be specified")
	}
	//if len(o.peersStrings) < 3 {
	//	return errors.New("peers must be set and contain 3 or more nodes")
	//}
	if o.BaseImage == "" {
		return errors.New("base image must be specified")
	}
	if o.BaseTag == "" {
		return errors.New("base tag must be specified")
	}
	if o.MetricsCertPath != "" || o.MetricsKeyPath != "" {
		// if one is set, both must be
		if o.MetricsCertPath == "" || o.MetricsKeyPath == "" {
			return errors.New("when one of metrics-cert-path or metrics-key-path is specified, both must be set")
		}
	}
	if o.WebhookCertPath != "" || o.WebhookKeyPath != "" {
		// if one is set, both must be
		if o.WebhookCertPath == "" || o.WebhookKeyPath == "" {
			return errors.New("when one of webhook-cert-path or webhook-key-path is specified, both must be set")
		}
	}

	for _, peer := range o.peersStrings {
		cluster, err := peerFromFlag(peer)
		if err != nil {
			return fmt.Errorf("parsing peer flag %q: %v", peer, err)
		}
		o.Peers = append(o.Peers, cluster)
	}

	return nil
}

func peerFromFlag(value string) (RaftCluster, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return RaftCluster{}, errors.New("format of peer flag is name://address")
	}
	return RaftCluster{
		Name:    parsed.Scheme,
		Address: parsed.Host,
	}, nil
}

func (o *MulticlusterOptions) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Name, "name", "", "raft node name")
	cmd.Flags().StringVar(&o.Address, "raft-address", "", "raft node address")
	cmd.Flags().StringVar(&o.CAFile, "ca-file", "", "raft ca file")
	cmd.Flags().StringVar(&o.PrivateKeyFile, "private-key-file", "", "raft private key file")
	cmd.Flags().StringVar(&o.CertificateFile, "certificate-file", "", "raft certificate file")
	cmd.Flags().DurationVar(&o.ElectionTimeout, "election-timeout", 10*time.Second, "raft election timeout")
	cmd.Flags().DurationVar(&o.HeartbeatInterval, "heartbeat-interval", 1*time.Second, "raft heartbeat interval")
	cmd.Flags().StringSliceVar(&o.peersStrings, "peer", []string{}, "raft peers")
	cmd.Flags().StringVar(&o.KubernetesAPIServer, "kubernetes-api-address", "", "raft kubernetes api server address")
	cmd.Flags().StringVar(&o.KubeconfigNamespace, "kubeconfig-namespace", "default", "raft kubeconfig namespace")
	cmd.Flags().StringVar(&o.KubeconfigName, "kubeconfig-name", "multicluster-kubeconfig", "raft kubeconfig name")
	cmd.Flags().StringVar(&o.WebhookCertPath, "webhook-cert-path", "", "path on disk to the webhook certificate, implies enabling webhooks")
	cmd.Flags().StringVar(&o.WebhookKeyPath, "webhook-key-path", "", "path on disk to the webhook certificate key, implies enabling webhooks")
	cmd.Flags().StringVar(&o.MetricsBindAddress, "metrics-bind-address", "", "address for binding metrics server")
	cmd.Flags().StringVar(&o.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "address for binding health check endpoint")
	cmd.Flags().StringVar(&o.BaseImage, "base-image", "", "base image for sidecars and init containers")
	cmd.Flags().StringVar(&o.BaseTag, "base-tag", "", "base image tag for sidecars and init containers")
	cmd.Flags().StringVar(&o.MetricsCertPath, "metrics-cert-path", "", "The path to the metrics server certificate file, implies secure serving of metrics")
	cmd.Flags().StringVar(&o.MetricsKeyPath, "metrics-key-path", "", "The path to the metrics server key file, implies secure serving of metrics.")
	cmd.Flags().StringVar(&o.LicenseFilePath, "license-file-path", "", "The path to the Redpanda License.")
	cmd.Flags().StringVar(&o.LeaderElectionID, "local-leader-election-id", "redpanda-multicluster-raft-leader", "Name of the K8s Lease resource for local leader election")
	cmd.Flags().StringVar(&o.LeaderElectionNamespace, "local-leader-election-namespace", "", "Namespace for the local leader election Lease (defaults to pod namespace)")
	cmd.Flags().DurationVar(&o.LeaderElectionLeaseDuration, "local-leader-election-lease-duration", 0, "Duration of the local leader election lease (0 uses controller-runtime default of 15s)")
	cmd.Flags().DurationVar(&o.LeaderElectionRenewDeadline, "local-leader-election-renew-deadline", 0, "Renew deadline for the local leader election lease (0 uses controller-runtime default of 10s)")
	cmd.Flags().DurationVar(&o.LeaderElectionRetryPeriod, "local-leader-election-retry-period", 0, "Retry period for the local leader election lease (0 uses controller-runtime default of 2s)")
	cmd.Flags().DurationVar(&o.UnbindPVCsAfter, "unbind-pvcs-after", 0, "if not zero, runs the PVCUnbinder controller which attempts to 'unbind' the PVCs' of Pods that are Pending for longer than the given duration")
	cmd.Flags().BoolVar(&o.AllowPVRebinding, "allow-pv-rebinding", false, "controls whether or not PVs unbound by the PVCUnbinder have their .ClaimRef cleared, which allows them to be reused")
	cmd.Flags().Var(&o.UnbinderSelector, "unbinder-label-selector", "if provided, a Kubernetes label selector that will filter Pods to be considered by the PVCUnbinder.")
	cmd.Flags().DurationVar(&o.ClusterConnectionTimeout, "cluster-connection-timeout", 10*time.Second, "Timeout for internal clients used to connect to Redpanda clusters (admin API in particular)")
	cmd.Flags().DurationVar(&o.ReconcileTimeout, "reconcile-timeout", 2*time.Minute, "Defense-in-depth ceiling on a single reconcile pass; on deadline the reconcile aborts with context.DeadlineExceeded and is requeued with backoff. Primary bounding should still come from per-call timeouts on downstream clients")
}

func Command() *cobra.Command {
	var options MulticlusterOptions

	cmd := &cobra.Command{
		Use:   "multicluster",
		Short: "Run the redpanda operator in multicluster mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(cmd.Context(), &options)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

func Run(
	ctx context.Context,
	opts *MulticlusterOptions,
) error {
	setupLog := log.FromContext(ctx).WithName("setup")

	if err := opts.validate(); err != nil {
		return err
	}

	l, err := license.ReadLicense(opts.LicenseFilePath)
	if err != nil {
		return errors.WithStack(err)
	}

	if err = license.CheckExpiration(l.Expires()); err != nil {
		return errors.WithStack(err)
	}

	// as AllowsEnterpriseFeatures function checks expiration and license type in the above
	// if statement the expiration is checked and here we validate the type of the license
	if !l.AllowsEnterpriseFeatures() {
		return errors.New("Operator requires enterprise license")
	}

	k8sConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	config := &multicluster.RaftConfiguration{
		Name:                opts.Name,
		Address:             opts.Address,
		ElectionTimeout:     opts.ElectionTimeout,
		HeartbeatInterval:   opts.HeartbeatInterval,
		Logger:              log.FromContext(ctx).WithName("raft-runtime-manager"),
		RestConfig:          k8sConfig,
		Meta:                []byte("node-name=" + opts.Name),
		Scheme:              controller.MulticlusterScheme,
		Bootstrap:           true,
		KubernetesAPIServer: opts.KubernetesAPIServer,
		KubeconfigNamespace: opts.KubeconfigNamespace,
		KubeconfigName:      opts.KubeconfigName,
		HealthProbeAddress:  opts.HealthProbeBindAddress,
		LocalLeaderElection: &multicluster.LocalLeaderElectionConfig{
			ID:            opts.LeaderElectionID,
			Namespace:     opts.LeaderElectionNamespace,
			LeaseDuration: opts.LeaderElectionLeaseDuration,
			RenewDeadline: opts.LeaderElectionRenewDeadline,
			RetryPeriod:   opts.LeaderElectionRetryPeriod,
		},
	}

	// Disabling http/2 is to
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}

	if opts.MetricsBindAddress != "" {
		config.Metrics = &metricsserver.Options{
			BindAddress: opts.MetricsBindAddress,
			// FilterProvider is used to protect the metrics endpoint with authn/authz.
			// These configurations ensure that only authorized users and service accounts
			// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
			// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
			FilterProvider: filters.WithAuthenticationAndAuthorization,
		}

		if opts.MetricsCertPath != "" || opts.MetricsKeyPath != "" {
			// Set up all of the certificate watching code
			metricsCertWatcher, err := certwatcher.New(opts.MetricsCertPath, opts.MetricsKeyPath)
			if err != nil {
				setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
				return err
			}
			go func() {
				setupLog.Error(metricsCertWatcher.Start(ctx), "metrics cert watcher exited")
			}()
			fetchCertificates := func(config *tls.Config) {
				config.GetCertificate = metricsCertWatcher.GetCertificate
			}

			config.Metrics.SecureServing = true
			config.Metrics.TLSOpts = []func(*tls.Config){disableHTTP2, fetchCertificates}
		}
	}

	if opts.WebhookCertPath != "" || opts.WebhookKeyPath != "" {
		// Set up all of the certificate watching code
		webhookCertWatcher, err := certwatcher.New(opts.WebhookCertPath, opts.WebhookKeyPath)
		if err != nil {
			setupLog.Error(err, "to initialize webhook certificate watcher", "error", err)
			return err
		}
		go func() {
			setupLog.Error(webhookCertWatcher.Start(ctx), "webhook cert watcher exited")
		}()
		fetchCertificates := func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		}
		config.Webhooks = webhook.NewServer(webhook.Options{
			TLSOpts: []func(*tls.Config){disableHTTP2, fetchCertificates},
		})
	}

	// Set up the certificate watcher
	certWatcher, err := watcher.New(opts.CAFile, opts.CertificateFile, opts.PrivateKeyFile)
	if err != nil {
		setupLog.Error(err, "to initialize raft certificate watcher", "error", err)
		return err
	}
	certWatcher.SetLogger(setupLog)

	// NB: we dynamically swap out the cached CA read from disk based on
	// the basic CA rotation code found here:
	// https://github.com/golang/go/issues/64796#issuecomment-2897933746
	config.ClientTLSOptions = []func(c *tls.Config){certWatcher.ClientTLSOptions}
	config.ServerTLSOptions = []func(c *tls.Config){certWatcher.ServerTLSOptions}

	for _, peer := range opts.Peers {
		config.Peers = append(config.Peers, multicluster.RaftCluster{
			Name:    peer.Name,
			Address: peer.Address,
		})
	}

	manager, err := multicluster.NewRaftRuntimeManager(config)
	if err != nil {
		return fmt.Errorf("initializing cluster: %w", err)
	}

	cloudSecrets := lifecycle.CloudSecretsFlags{
		CloudSecretsEnabled: false,
	}

	sidecarImage := lifecycle.Image{
		Repository: opts.BaseImage,
		Tag:        opts.BaseTag,
	}

	redpandaImage := lifecycle.Image{
		Repository: opts.BaseImage,
		Tag:        opts.BaseTag,
	}

	factory := internalclient.NewFactory(manager, nil).WithAdminClientTimeout(opts.ClusterConnectionTimeout)

	if err := redpandacontrollers.SetupMulticlusterController(ctx, manager, redpandaImage, sidecarImage, cloudSecrets, factory, opts.ReconcileTimeout); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Multicluster")
		return err
	}

	if err := manager.GetLocalManager().Add(certWatcher); err != nil {
		return err
	}
	if config.HealthProbeAddress != "" {
		if err := manager.GetLocalManager().AddHealthzCheck("health", healthz.Ping); err != nil {
			return err
		}

		if err := manager.GetLocalManager().AddReadyzCheck("check", healthz.Ping); err != nil {
			return err
		}

		if err := manager.GetLocalManager().AddReadyzCheck("raft", manager.Health); err != nil {
			return err
		}
	}

	if err := redpandacontrollers.SetupWithMultiClusterManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NodePool")
		return err
	}

	if opts.UnbindPVCsAfter <= 0 {
		setupLog.Info("PVCUnbinder controller not active", "unbind-after", opts.UnbindPVCsAfter, "selector", opts.UnbinderSelector, "allow-pv-rebinding", opts.AllowPVRebinding)
	} else {
		setupLog.Info("starting PVCUnbinder controller", "unbind-after", opts.UnbindPVCsAfter, "selector", opts.UnbinderSelector, "allow-pv-rebinding", opts.AllowPVRebinding)
		if err := (&pvcunbinder.MulticlusterController{
			Manager:        manager,
			Timeout:        opts.UnbindPVCsAfter,
			Selector:       opts.UnbinderSelector.Selector,
			AllowRebinding: opts.AllowPVRebinding,
		}).SetupWithMultiClusterManager(); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PVCUnbinder")
			return err
		}
	}

	return manager.Start(ctrl.SetupSignalHandler())
}
