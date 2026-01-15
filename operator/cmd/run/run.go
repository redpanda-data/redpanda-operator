// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package run contains the boilerplate code to configure and run the redpanda
// operator.
package run

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	consolecontroller "github.com/redpanda-data/redpanda-operator/operator/internal/controller/console"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/nodewatcher"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/olddecommission"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	vectorizedcontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/pflagutil"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

type Controller string

const (
	defaultConfiguratorContainerImage = "docker.redpanda.com/redpandadata/redpanda-operator"
	DefaultRedpandaImageTag           = "v25.3.1"
	DefaultRedpandaRepository         = "docker.redpanda.com/redpandadata/redpanda"

	AllNonVectorizedControllers = Controller("all")
	NodeWatcherController       = Controller("nodeWatcher")
	OldDecommissionController   = Controller("decommission")
)

var availableControllers = []string{
	string(NodeWatcherController),
	string(OldDecommissionController),
}

type RunOptions struct {
	managerOptions  ctrl.Options
	rpClientTimeout time.Duration

	images struct {
		configurator struct {
			image string
			tag   string
		}
		redpanda struct {
			image string
			tag   string
		}
	}

	v1Flags struct {
		enabled                     bool
		ghostbuster                 bool
		clusterDomain               string
		decommissionWaitInterval    time.Duration
		metricsTimeout              time.Duration
		autoDeletePVCs              bool
		restrictToRedpandaVersion   string
		configuratorImagePullPolicy string
	}

	v2Flags struct {
		enabled          bool
		nodepoolsEnabled bool
		consoleEnabled   bool
	}

	unbinder struct {
		selector       pflagutil.LabelSelectorValue
		unbindAfter    time.Duration
		allowRebinding bool
	}

	webhooks struct {
		enabled  bool
		certPath string
		certName string
		certKey  string
	}

	metrics struct {
		secure   bool
		certPath string
		certName string
		certKey  string
	}

	cloudSecrets struct {
		enabled bool
		prefix  string
		config  pkgsecrets.ExpanderCloudConfiguration
	}

	maybeDeprecate struct {
		// all of our docs basically say 1 operator per cluster
		// we should deprecate this mode of running
		namespace             string
		additionalControllers []string
		enableHTTP2           bool
		decommissionerFlags   struct {
			enabled    bool
			syncPeriod time.Duration
		}
	}
}

func (o *RunOptions) BindFlags(cmd *cobra.Command) {
	{
		// Manager flags.
		cmd.Flags().StringVar(&o.managerOptions.Metrics.BindAddress, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
		cmd.Flags().BoolVar(&o.managerOptions.LeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
		cmd.Flags().StringVar(&o.managerOptions.LeaderElectionID, "leader-election-id", "aa9fc693.vectorized.io", "Sets the ID used for the leader election process.")
		// NB: The default behavior here is in the controller-runtime, pretty deep. It reads the namespace file that's created when mounting a service account token.
		cmd.Flags().StringVar(&o.managerOptions.LeaderElectionNamespace, "leader-election-namespace", "", "Sets the namespace that leader election resources will be created within. If not specified, defaults the value of --namespace or the namespace this Pod is running in.")
		cmd.Flags().StringVar(&o.managerOptions.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
		cmd.Flags().StringVar(&o.managerOptions.PprofBindAddress, "pprof-bind-address", ":8082", "The address the metric endpoint binds to. Set to '' or 0 to disable")
	}

	cmd.Flags().DurationVar(&o.rpClientTimeout, "cluster-connection-timeout", 10*time.Second, "Set the timeout for internal clients used to connect to Redpanda clusters")

	{
		// image flags
		cmd.Flags().StringVar(&o.images.configurator.image, "configurator-base-image", defaultConfiguratorContainerImage, "The repository of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
		cmd.Flags().StringVar(&o.images.configurator.tag, "configurator-tag", version.Version, "The tag of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
		cmd.Flags().StringVar(&o.images.redpanda.image, "redpanda-tag", DefaultRedpandaImageTag, "The default docker image tag for redpanda containers")
		cmd.Flags().StringVar(&o.images.redpanda.tag, "redpanda-repository", DefaultRedpandaRepository, "The default docker repository to pull redpanda images from")
	}

	{
		// unbinder flags
		cmd.Flags().DurationVar(&o.unbinder.unbindAfter, "unbind-pvcs-after", 0, "if not zero, runs the PVCUnbinder controller which attempts to 'unbind' the PVCs' of Pods that are Pending for longer than the given duration")
		cmd.Flags().BoolVar(&o.unbinder.allowRebinding, "allow-pv-rebinding", false, "controls whether or not PVs unbound by the PVCUnbinder have their .ClaimRef cleared, which allows them to be reused")
		cmd.Flags().Var(&o.unbinder.selector, "unbinder-label-selector", "if provided, a Kubernetes label selector that will filter Pods to be considered by the PVCUnbinder.")
	}

	{
		// webhooks flags
		cmd.Flags().BoolVar(&o.webhooks.enabled, "webhook-enabled", false, "Enable webhook Manager")
		cmd.Flags().StringVar(&o.webhooks.certPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
		cmd.Flags().StringVar(&o.webhooks.certName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
		cmd.Flags().StringVar(&o.webhooks.certKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	}

	{
		// metrics flags
		cmd.Flags().BoolVar(&o.metrics.secure, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
		cmd.Flags().StringVar(&o.metrics.certPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
		cmd.Flags().StringVar(&o.metrics.certName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
		cmd.Flags().StringVar(&o.metrics.certKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	}

	{
		// cloud secrets flags
		cmd.Flags().BoolVar(&o.cloudSecrets.enabled, "enable-cloud-secrets", false, "Set to true if config values can reference secrets from cloud secret store")
		cmd.Flags().StringVar(&o.cloudSecrets.prefix, "cloud-secrets-prefix", "", "Prefix for all names of cloud secrets")
		cmd.Flags().StringVar(&o.cloudSecrets.config.AWSRegion, "cloud-secrets-aws-region", "", "AWS Region in which the secrets are stored")
		cmd.Flags().StringVar(&o.cloudSecrets.config.AWSRoleARN, "cloud-secrets-aws-role-arn", "", "AWS role ARN to assume when fetching secrets")
		cmd.Flags().StringVar(&o.cloudSecrets.config.GCPProjectID, "cloud-secrets-gcp-project-id", "", "GCP project ID in which the secrets are stored")
		cmd.Flags().StringVar(&o.cloudSecrets.config.AzureKeyVaultURI, "cloud-secrets-azure-key-vault-uri", "", "Azure Key Vault URI in which the secrets are stored")
	}

	{
		// v2-only flags
		cmd.Flags().BoolVar(&o.v2Flags.enabled, "enable-redpanda-controllers", true, "Specifies whether or not to enabled the Redpanda cluster controllers")
		cmd.Flags().BoolVar(&o.v2Flags.consoleEnabled, "enable-console", true, "Specifies whether or not to enabled the redpanda Console controller")
		cmd.Flags().BoolVar(&o.v2Flags.nodepoolsEnabled, "enable-v2-nodepools", false, "Specifies whether or not to enabled the v2 nodepool controller")
	}

	{
		// v1-only flags
		cmd.Flags().BoolVar(&o.v1Flags.enabled, "enable-vectorized-controllers", false, "Specifies whether or not to enabled the legacy controllers for resources in the Vectorized Group (Also known as V1 operator mode)")
		cmd.Flags().DurationVar(&o.v1Flags.decommissionWaitInterval, "decommission-wait-interval", 8*time.Second, "Set the time to wait for a node decommission to happen in the cluster")
		cmd.Flags().DurationVar(&o.v1Flags.metricsTimeout, "metrics-timeout", 8*time.Second, "Set the timeout for a checking metrics Admin API endpoint. If set to 0, then the 2 seconds default will be used")
		cmd.Flags().BoolVar(&o.v1Flags.autoDeletePVCs, "auto-delete-pvcs", false, "Use StatefulSet PersistentVolumeClaimRetentionPolicy to auto delete PVCs on scale down and Cluster resource delete.")
		cmd.Flags().StringVar(&o.v1Flags.restrictToRedpandaVersion, "restrict-redpanda-version", "", "Restrict management of clusters to those with this version")
		cmd.Flags().StringVar(&o.v1Flags.configuratorImagePullPolicy, "configurator-image-pull-policy", "Always", "Set the configurator image pull policy")
		cmd.Flags().StringVar(&o.v1Flags.clusterDomain, "cluster-domain", "cluster.local", "Set the Kubernetes local domain (Kubelet's --cluster-domain)")

		cmd.Flags().BoolVar(&o.v1Flags.ghostbuster, "unsafe-decommission-failed-brokers", false, "Set to enable decommissioning a failed broker that is configured but does not exist in the StatefulSet (ghost broker). This may result in invalidating valid data")
		_ = cmd.Flags().MarkHidden("unsafe-decommission-failed-brokers")

		// Legacy Global flags.
		cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowDownscalingInWebhook, "allow-downscaling", true, "Allow to reduce the number of replicas in existing clusters")
		cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowConsoleAnyNamespace, "allow-console-any-ns", false, "Allow to create Console in any namespace. Allowing this copies Redpanda SchemaRegistry TLS Secret to namespace (alpha feature)")
		cmd.Flags().StringVar(&vectorizedv1alpha1.SuperUsersPrefix, "superusers-prefix", "", "Prefix to add in username of superusers managed by operator. This will only affect new clusters, enabling this will not add prefix to existing clusters (alpha feature)")
	}

	{
		// to deprecate
		cmd.Flags().StringVar(&o.maybeDeprecate.namespace, "namespace", "", "If namespace is set to not empty value, it changes scope of Redpanda operator to work in single namespace")
		cmd.Flags().BoolVar(&o.maybeDeprecate.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
		cmd.Flags().StringSliceVar(&o.maybeDeprecate.additionalControllers, "additional-controllers", []string{""}, fmt.Sprintf("which controllers to run, available: all, %s", strings.Join(availableControllers, ", ")))
		{
			// decommissioner flags
			cmd.Flags().BoolVar(&o.maybeDeprecate.decommissionerFlags.enabled, "enable-ghost-broker-decommissioner", false, "Enable ghost broker decommissioner.")
			cmd.Flags().DurationVar(&o.maybeDeprecate.decommissionerFlags.syncPeriod, "ghost-broker-decommissioner-sync-period", time.Minute*5, "Ghost broker sync period. The Ghost Broker Decommissioner is guaranteed to be called after this period.")
		}
	}

	// Deprecated flags.
	cmd.Flags().Bool("debug", false, "A deprecated and unused flag")
	cmd.Flags().String("events-addr", "", "A deprecated and unused flag")
	cmd.Flags().Bool("enable-helm-controllers", false, "A deprecated and unused flag")
	cmd.Flags().String("helm-repository-url", "https://charts.redpanda.com/", "A deprecated and unused flag")
	cmd.Flags().Bool("force-defluxed-mode", false, "A deprecated and unused flag")
	cmd.Flags().Bool("allow-pvc-deletion", false, "Deprecated: Ignored if specified")
	cmd.Flags().Bool("operator-mode", true, "A deprecated and unused flag")
	cmd.Flags().Bool("enable-shadowlinks", false, "Specifies whether or not to enabled the shadow links controller")
}

func (o *RunOptions) ControllerEnabled(controller Controller) bool {
	for _, c := range o.maybeDeprecate.additionalControllers {
		if Controller(c) == AllNonVectorizedControllers || Controller(c) == controller {
			return true
		}
	}
	return false
}

// Metrics RBAC permissions
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create;
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create;

// Leader election permissions
// +kubebuilder:rbac:groups=coordination.k8s.io,namespace=default,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,namespace=default,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=events,verbs=create;patch

func Command() *cobra.Command {
	var options RunOptions

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the redpanda operator",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			var cloudExpander *pkgsecrets.CloudExpander
			if options.cloudSecrets.enabled {
				var err error
				cloudExpander, err = pkgsecrets.NewCloudExpander(ctx, options.cloudSecrets.prefix, options.cloudSecrets.config)
				if err != nil {
					return err
				}
			}

			return Run(ctx, cloudExpander, &options)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

//nolint:funlen,gocyclo // length looks good
func Run(
	ctx context.Context,
	cloudExpander *pkgsecrets.CloudExpander,
	opts *RunOptions,
) error {
	v1Controllers := opts.v1Flags.enabled
	v2Controllers := opts.v2Flags.enabled

	if (opts.v2Flags.nodepoolsEnabled || opts.v2Flags.consoleEnabled) && !v2Controllers {
		return errors.New("running NodePool or Console controllers requires running the Redpanda controller")
	}

	setupLog := ctrl.LoggerFrom(ctx).WithName("setup")

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	var tlsOpts []func(*tls.Config)
	if !opts.maybeDeprecate.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	var webhookServer webhook.Server
	if opts.webhooks.enabled {
		// Initial webhook TLS options
		webhookTLSOpts := tlsOpts

		if len(opts.webhooks.certPath) > 0 {
			setupLog.Info("Initializing webhook certificate watcher using provided certificates",
				"webhook-cert-path", opts.webhooks.certPath, "webhook-cert-name", opts.webhooks.certName, "webhook-cert-key", opts.webhooks.certKey)

			var err error
			webhookCertWatcher, err = certwatcher.New(
				filepath.Join(opts.webhooks.certPath, opts.webhooks.certName),
				filepath.Join(opts.webhooks.certPath, opts.webhooks.certKey),
			)
			if err != nil {
				setupLog.Error(err, "Failed to initialize webhook certificate watcher")
				return err
			}
			go func() {
				setupLog.Error(webhookCertWatcher.Start(ctx), "webhook cert watcher exits")
			}()

			webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
				config.GetCertificate = webhookCertWatcher.GetCertificate
			})
		}

		webhookServer = webhook.NewServer(webhook.Options{
			TLSOpts: webhookTLSOpts,
		})
	}

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   opts.managerOptions.Metrics.BindAddress,
		SecureServing: opts.metrics.secure,
		TLSOpts:       tlsOpts,
	}

	if opts.metrics.secure {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(opts.metrics.certPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", opts.metrics.certPath, "metrics-cert-name", opts.metrics.certName, "metrics-cert-key", opts.metrics.certKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(opts.metrics.certPath, opts.metrics.certName),
			filepath.Join(opts.metrics.certPath, opts.metrics.certKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			return err
		}
		go func() {
			setupLog.Error(metricsCertWatcher.Start(ctx), "metrics cert watcher exits")
		}()

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// Set options that are not or cannot be bound to CLI flags.
	opts.managerOptions.WebhookServer = webhookServer
	opts.managerOptions.Metrics = metricsServerOptions
	opts.managerOptions.Scheme = controller.UnifiedScheme

	if opts.managerOptions.LeaderElectionNamespace == "" {
		if opts.maybeDeprecate.namespace == "" {
			opts.managerOptions.LeaderElectionNamespace = "kube-system"
		} else {
			opts.managerOptions.LeaderElectionNamespace = opts.maybeDeprecate.namespace
		}
	}

	if opts.maybeDeprecate.namespace != "" {
		opts.managerOptions.Cache.DefaultNamespaces = map[string]cache.Config{opts.maybeDeprecate.namespace: {}}
	}

	mcmanager, err := multicluster.NewSingleClusterManager(ctrl.GetConfigOrDie(), opts.managerOptions)
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		return err
	}
	mgr := mcmanager.GetLocalManager()

	// Configure controllers that are always enabled (Redpanda, Topic, User, Schema).

	factory := internalclient.NewFactory(mcmanager, cloudExpander).WithAdminClientTimeout(opts.rpClientTimeout)

	cloudSecrets := lifecycle.CloudSecretsFlags{
		CloudSecretsEnabled:          opts.cloudSecrets.enabled,
		CloudSecretsPrefix:           opts.cloudSecrets.prefix,
		CloudSecretsAWSRegion:        opts.cloudSecrets.config.AWSRegion,
		CloudSecretsAWSRoleARN:       opts.cloudSecrets.config.AWSRoleARN,
		CloudSecretsGCPProjectID:     opts.cloudSecrets.config.GCPProjectID,
		CloudSecretsAzureKeyVaultURI: opts.cloudSecrets.config.AzureKeyVaultURI,
	}

	sidecarImage := lifecycle.Image{
		Repository: opts.images.configurator.image,
		Tag:        opts.images.configurator.tag,
	}

	redpandaImage := lifecycle.Image{
		Repository: opts.images.redpanda.image,
		Tag:        opts.images.redpanda.tag,
	}

	if v2Controllers {
		// Redpanda Reconciler
		if err := (&redpandacontrollers.RedpandaReconciler{
			Manager:              mcmanager,
			LifecycleClient:      lifecycle.NewResourceClient(mcmanager, lifecycle.V2ResourceManagers(redpandaImage, sidecarImage, cloudSecrets)),
			ClientFactory:        factory,
			CloudSecretsExpander: cloudExpander,
			UseNodePools:         opts.v2Flags.consoleEnabled,
		}).SetupWithManager(ctx, mcmanager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Redpanda")
			return err
		}

		// the following 2 controllers depend on the Redpanda controller being run, so
		// only run them if we run the Redpanda controller

		// NodePool Reconciler
		if opts.v2Flags.nodepoolsEnabled {
			if err := (&redpandacontrollers.NodePoolReconciler{
				Manager: mcmanager,
			}).SetupWithManager(ctx, mcmanager); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "NodePool")
				return err
			}
		}

		// Console Reconciler.
		if opts.v2Flags.consoleEnabled {
			ctl, err := kube.FromRESTConfig(mgr.GetConfig(), kube.Options{
				Options: client.Options{
					Scheme: mgr.GetScheme(),
					// mgr's GetClient sets the cache, to have a fully compatible ctl, we
					// need to set the cache as well.
					Cache: &client.CacheOptions{
						Reader: mgr.GetCache(),
					},
				},
			})
			if err != nil {
				return err
			}

			if err := (&consolecontroller.Controller{Ctl: ctl}).SetupWithManager(ctx, mcmanager); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "Console")
				return err
			}
		}
	}

	if err := redpandacontrollers.SetupShadowLinkController(ctx, mcmanager, cloudExpander, v1Controllers, v2Controllers); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ShadowLink")
		return err
	}

	if err := redpandacontrollers.SetupTopicController(ctx, mcmanager, cloudExpander, v1Controllers, v2Controllers); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Topic")
		return err
	}

	if err := redpandacontrollers.SetupUserController(ctx, mcmanager, cloudExpander, v1Controllers, v2Controllers); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "User")
		return err
	}

	if err := redpandacontrollers.SetupRoleController(ctx, mcmanager, cloudExpander, v1Controllers, v2Controllers); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RedpandaRole")
		return err
	}

	if err := redpandacontrollers.SetupSchemaController(ctx, mcmanager, cloudExpander, v1Controllers, v2Controllers); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Schema")
		return err
	}

	// Next configure and setup optional controllers.

	if v1Controllers {
		setupLog.Info("setting up vectorized controllers")
		if err := setupVectorizedControllers(ctx, mgr, factory, cloudExpander, opts); err != nil {
			return err
		}
	}

	if opts.ControllerEnabled(NodeWatcherController) {
		if err = (&nodewatcher.RedpandaNodePVCReconciler{
			Client:       mgr.GetClient(),
			OperatorMode: true,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "RedpandaNodePVCReconciler")
			return err
		}
	}

	if opts.ControllerEnabled(OldDecommissionController) {
		if err = (&olddecommission.DecommissionReconciler{
			Client:                   mgr.GetClient(),
			OperatorMode:             true,
			DecommissionWaitInterval: opts.v1Flags.decommissionWaitInterval,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DecommissionReconciler")
			return err
		}
	}

	// The unbinder gets to run in any mode, if it's enabled.
	if opts.unbinder.unbindAfter <= 0 {
		setupLog.Info("PVCUnbinder controller not active", "unbind-after", opts.unbinder.unbindAfter, "selector", opts.unbinder.selector, "allow-pv-rebinding", opts.unbinder.allowRebinding)
	} else {
		setupLog.Info("starting PVCUnbinder controller", "unbind-after", opts.unbinder.unbindAfter, "selector", opts.unbinder.selector, "allow-pv-rebinding", opts.unbinder.allowRebinding)

		if err := (&pvcunbinder.Controller{
			Client:         mgr.GetClient(),
			Timeout:        opts.unbinder.unbindAfter,
			Selector:       opts.unbinder.selector.Selector,
			AllowRebinding: opts.unbinder.allowRebinding,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PVCUnbinder")
			return err
		}
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up health check")
		return err
	}

	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up ready check")
		return err
	}

	if opts.webhooks.enabled {
		hookServer := mgr.GetWebhookServer()
		if err := mgr.AddReadyzCheck("webhook", hookServer.StartedChecker()); err != nil {
			setupLog.Error(err, "unable to create ready check")
			return err
		}

		if err := mgr.AddHealthzCheck("webhook", hookServer.StartedChecker()); err != nil {
			setupLog.Error(err, "unable to create health check")
			return err
		}
	}

	setupLog.Info("Starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		return err
	}

	return nil
}

// setupVectorizedControllers configures and registers controllers and
// runnables for the custom resources in the vectorized group, AKA the V1
// operator.
func setupVectorizedControllers(ctx context.Context, mgr ctrl.Manager, factory internalclient.ClientFactory, cloudExpander *pkgsecrets.CloudExpander, opts *RunOptions) error {
	log.Info(ctx, "Starting Vectorized (V1) Controllers")

	configurator := resources.ConfiguratorSettings{
		ConfiguratorBaseImage:        opts.images.configurator.image,
		ConfiguratorTag:              opts.images.configurator.tag,
		ImagePullPolicy:              corev1.PullPolicy(opts.v1Flags.configuratorImagePullPolicy),
		CloudSecretsEnabled:          opts.cloudSecrets.enabled,
		CloudSecretsPrefix:           opts.cloudSecrets.prefix,
		CloudSecretsAWSRegion:        opts.cloudSecrets.config.AWSRegion,
		CloudSecretsAWSRoleARN:       opts.cloudSecrets.config.AWSRoleARN,
		CloudSecretsGCPProjectID:     opts.cloudSecrets.config.GCPProjectID,
		CloudSecretsAzureKeyVaultURI: opts.cloudSecrets.config.AzureKeyVaultURI,
	}

	adminAPIClientFactory := adminutils.CachedNodePoolAdminAPIClientFactory(adminutils.NewNodePoolInternalAdminAPI)

	if err := (&vectorizedcontrollers.ClusterReconciler{
		Client:                    mgr.GetClient(),
		Log:                       ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Cluster"),
		Scheme:                    mgr.GetScheme(),
		AdminAPIClientFactory:     adminAPIClientFactory,
		DecommissionWaitInterval:  opts.v1Flags.decommissionWaitInterval,
		MetricsTimeout:            opts.v1Flags.metricsTimeout,
		RestrictToRedpandaVersion: opts.v1Flags.restrictToRedpandaVersion,
		GhostDecommissioning:      opts.v1Flags.ghostbuster,
		AutoDeletePVCs:            opts.v1Flags.autoDeletePVCs,
		CloudSecretsExpander:      cloudExpander,
		Timeout:                   opts.rpClientTimeout,
	}).WithClusterDomain(opts.v1Flags.clusterDomain).WithConfiguratorSettings(configurator).SetupWithManager(mgr); err != nil {
		log.Error(ctx, err, "Unable to create controller", "controller", "Cluster")
		return err
	}

	if err := vectorizedcontrollers.NewClusterMetricsController(mgr.GetClient()).SetupWithManager(mgr); err != nil {
		log.Error(ctx, err, "Unable to create controller", "controller", "ClustersMetrics")
		return err
	}

	// Setup webhooks
	if opts.webhooks.enabled {
		log.Info(ctx, "Setup webhook")
		if err := (&vectorizedv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
			log.Error(ctx, err, "Unable to create webhook", "webhook", "RedpandaCluster")
			return err
		}
	}

	if opts.maybeDeprecate.decommissionerFlags.enabled && opts.v1Flags.enabled {
		adapter := vectorizedDecommissionerAdapter{factory: factory, client: mgr.GetClient()}
		d := decommissioning.NewStatefulSetDecommissioner(
			mgr,
			adapter.getAdminClient,
			decommissioning.WithFilter(adapter.filter),
			// Operator v1 supports multiple NodePools, and therefore multiple STS.
			// This function provides a custom replica count: the desired replicas of all STS, instead of a single STS.
			decommissioning.WithDesiredReplicasFetcher(adapter.desiredReplicas),
			decommissioning.WithSyncPeriod(opts.maybeDeprecate.decommissionerFlags.syncPeriod),
			decommissioning.WithCleanupPVCs(false),
			// In Operator v1, decommissioning based on pod ordinal is not correct because
			// it has controller code that manages decommissioning. If something else decommissions the node, it can not deal with this under all circumstances because of various reasons, eg. bercause of a protection against stale status reads of status.currentReplicas
			//   (http://github.com/redpanda-data/redpanda-operator/blob/main/operator/pkg/resources/statefulset_scale.go#L139)
			// In addition to this situation where it can not (always) recover, it is just not desired that it interferes with graceful, "standard" decommissions (at least, in Operator v1 mode)
			decommissioning.WithDecommisionOnTooHighOrdinal(false),
		)

		if err := d.SetupWithManager(mgr); err != nil {
			log.Error(ctx, err, "unable to create controller", "controller", "StatefulSetDecommissioner")
			return err
		}
	}

	return nil
}
