// Copyright 2025 Redpanda Data, Inc.
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
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/watcher"
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
	LogLevel               string
	BaseImage              string
	BaseTag                string
	HealthProbeBindAddress string
	MetricsBindAddress     string
	MetricsCertPath        string
	MetricsKeyPath         string
	WebhookCertPath        string
	WebhookKeyPath         string

	peersStrings []string
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
	if len(o.peersStrings) < 3 {
		return errors.New("peers must be set and contain 3 or more nodes")
	}
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
	cmd.Flags().StringVar(&o.LogLevel, "log-level", "info", "log level")
	cmd.Flags().StringVar(&o.WebhookCertPath, "webhook-cert-path", "", "path on disk to the webhook certificate, implies enabling webhooks")
	cmd.Flags().StringVar(&o.WebhookKeyPath, "webhook-key-path", "", "path on disk to the webhook certificate key, implies enabling webhooks")
	cmd.Flags().StringVar(&o.MetricsBindAddress, "metrics-bind-address", "", "address for binding metrics server")
	cmd.Flags().StringVar(&o.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "address for binding health check endpoint")
	cmd.Flags().StringVar(&o.BaseImage, "base-image", "", "base image for sidecars and init containers")
	cmd.Flags().StringVar(&o.BaseTag, "base-tag", "", "base image tag for sidecars and init containers")
	cmd.Flags().StringVar(&o.MetricsCertPath, "metrics-cert-path", "", "The path to the metrics server certificate file, implies secure serving of metrics")
	cmd.Flags().StringVar(&o.MetricsKeyPath, "metrics-key-path", "", "The path to the metrics server key file, implies secure serving of metrics.")
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
	setupLog := ctrl.LoggerFrom(ctx).WithName("setup")

	if err := opts.validate(); err != nil {
		return err
	}
	k8sConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	config := multicluster.RaftConfiguration{
		Name:                opts.Name,
		Address:             opts.Address,
		ElectionTimeout:     opts.ElectionTimeout,
		HeartbeatInterval:   opts.HeartbeatInterval,
		Logger:              ctrl.LoggerFrom(ctx).WithName("raft"),
		RestConfig:          k8sConfig,
		Meta:                []byte("node-name=" + opts.Name),
		Scheme:              controller.MulticlusterScheme,
		Bootstrap:           true,
		KubernetesAPIServer: opts.KubernetesAPIServer,
		KubeconfigNamespace: opts.KubeconfigNamespace,
		KubeconfigName:      opts.KubeconfigName,
		HealthProbeAddress:  opts.HealthProbeBindAddress,
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

	// Set up all of the certificate watching code
	raftCertWatcher, err := certwatcher.New(opts.CertificateFile, opts.PrivateKeyFile)
	if err != nil {
		setupLog.Error(err, "to initialize raft certificate watcher", "error", err)
		return err
	}
	raftCAWatcher, err := watcher.New(opts.CAFile)
	if err != nil {
		setupLog.Error(err, "to initialize raft certificate watcher", "error", err)
		return err
	}
	go func() {
		setupLog.Error(raftCertWatcher.Start(ctx), "raft cert watcher exited")
	}()
	go func() {
		setupLog.Error(raftCAWatcher.Start(ctx), "raft ca watcher exited")
	}()

	// NB: we dynamically swap out the cached CA read from disk based on
	// the basic CA rotation code found here:
	// https://github.com/golang/go/issues/64796#issuecomment-2897933746
	config.ClientTLSOptions = []func(c *tls.Config){func(c *tls.Config) {
		c.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return raftCertWatcher.GetCertificate(nil)
		}
		c.InsecureSkipVerify = true // nolint:gosec // verification below
		c.VerifyConnection = func(cs tls.ConnectionState) error {
			roots, err := raftCAWatcher.GetCA()
			if err != nil {
				return err
			}
			opts := x509.VerifyOptions{
				Roots:         roots,
				Intermediates: x509.NewCertPool(),
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err = cs.PeerCertificates[0].Verify(opts)
			return err
		}
	}}
	config.ServerTLSOptions = []func(c *tls.Config){func(c *tls.Config) {
		c.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			roots, err := raftCAWatcher.GetCA()
			if err != nil {
				return nil, err
			}
			cert, err := raftCertWatcher.GetCertificate(hello)
			if err != nil {
				return nil, err
			}
			if cert == nil {
				return nil, errors.New("certificate not loaded")
			}
			return &tls.Config{
				Certificates: []tls.Certificate{*cert},
				RootCAs:      roots,
				ClientAuth:   tls.RequireAndVerifyClientCert,
			}, nil
		}
	}}

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

	if err := redpandacontrollers.SetupMulticlusterController(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Multicluster")
		return err
	}

	if config.HealthProbeAddress != "" {
		if err := manager.GetLocalManager().AddHealthzCheck("health", healthz.Ping); err != nil {
			return err
		}

		if err := manager.GetLocalManager().AddReadyzCheck("check", healthz.Ping); err != nil {
			return err
		}
	}

	return manager.Start(ctrl.SetupSignalHandler())
}
