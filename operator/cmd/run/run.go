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
	"k8s.io/apimachinery/pkg/types"
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
	namespace             string
	additionalControllers []string

	// enableVectorizedControllers controls whether or not controllers for
	// resources in the vectorized group (Cluster, Console) AKA the V1 Operator
	// will be enabled or not.
	enableVectorizedControllers bool

	// enableRedpandaControllers controls whether or not to enable the Redpanda
	// controller - this should really only be used in cloud where we leverage
	// a different set of cluster CRDs.
	enableRedpandaControllers bool

	enableV2NodepoolController          bool
	enableConsoleController             bool
	managerOptions                      ctrl.Options
	clusterDomain                       string
	secureMetrics                       bool
	enableHTTP2                         bool
	webhookEnabled                      bool
	configuratorBaseImage               string
	configuratorTag                     string
	configuratorImagePullPolicy         string
	redpandaDefaultTag                  string
	redpandaDefaultRepository           string
	decommissionWaitInterval            time.Duration
	metricsTimeout                      time.Duration
	rpClientTimeout                     time.Duration
	restrictToRedpandaVersion           string
	ghostbuster                         bool
	unbindPVCsAfter                     time.Duration
	unbinderSelector                    pflagutil.LabelSelectorValue
	allowPVRebinding                    bool
	autoDeletePVCs                      bool
	webhookCertPath                     string
	webhookCertName                     string
	webhookCertKey                      string
	metricsCertPath                     string
	metricsCertName                     string
	metricsCertKey                      string
	enableGhostBrokerDecommissioner     bool
	ghostBrokerDecommissionerSyncPeriod time.Duration
	cloudSecretsEnabled                 bool
	cloudSecretsPrefix                  string
	cloudSecretsConfig                  pkgsecrets.ExpanderCloudConfiguration

	apiServer struct {
		enabled            bool
		serviceName        string
		serviceNamespace   string
		secretKeyName      string
		secretKeyNamespace string
	}
}

func (o *RunOptions) validateAPIServer() error {
	if !o.apiServer.enabled {
		return nil
	}

	var err error
	if o.apiServer.serviceName == "" {
		err = errors.Join(err, errors.New("--experimental-api-server-service-name is required when --experimental-api-server-enabled is specified"))
	}
	if o.apiServer.serviceNamespace == "" {
		err = errors.Join(err, errors.New("--experimental-api-server-service-namespace is required when --experimental-api-server-enabled is specified"))
	}
	if o.apiServer.secretKeyName == "" {
		err = errors.Join(err, errors.New("--experimental-api-server-secret-key-name is required when --experimental-api-server-enabled is specified"))
	}
	if o.apiServer.secretKeyNamespace == "" {
		err = errors.Join(err, errors.New("--experimental-api-server-secret-key-namespace is required when --experimental-api-server-enabled is specified"))
	}
	return err
}

func (o *RunOptions) BindFlags(cmd *cobra.Command) {
	// Manager flags.
	cmd.Flags().StringVar(&o.managerOptions.Metrics.BindAddress, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().BoolVar(&o.managerOptions.LeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().StringVar(&o.managerOptions.LeaderElectionID, "leader-election-id", "aa9fc693.vectorized.io", "Sets the ID used for the leader election process.")
	// NB: The default behavior here is in the controller-runtime, pretty deep. It reads the namespace file that's created when mounting a service account token.
	cmd.Flags().StringVar(&o.managerOptions.LeaderElectionNamespace, "leader-election-namespace", "", "Sets the namespace that leader election resources will be created within. If not specified, defaults the value of --namespace or the namespace this Pod is running in.")
	cmd.Flags().StringVar(&o.managerOptions.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&o.managerOptions.PprofBindAddress, "pprof-bind-address", ":8082", "The address the metric endpoint binds to. Set to '' or 0 to disable")
	cmd.Flags().StringVar(&o.namespace, "namespace", "", "If namespace is set to not empty value, it changes scope of Redpanda operator to work in single namespace")
	cmd.Flags().BoolVar(&o.secureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().BoolVar(&o.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	cmd.Flags().StringVar(&o.webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().StringVar(&o.webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().StringVar(&o.webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	cmd.Flags().StringVar(&o.metricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	cmd.Flags().StringVar(&o.metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	cmd.Flags().StringVar(&o.metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	cmd.Flags().BoolVar(&o.webhookEnabled, "webhook-enabled", false, "Enable webhook Manager")

	// Controller flags.
	cmd.Flags().BoolVar(&o.enableConsoleController, "enable-console", true, "Specifies whether or not to enabled the redpanda Console controller")
	cmd.Flags().BoolVar(&o.enableV2NodepoolController, "enable-v2-nodepools", false, "Specifies whether or not to enabled the v2 nodepool controller")
	cmd.Flags().BoolVar(&o.enableVectorizedControllers, "enable-vectorized-controllers", false, "Specifies whether or not to enabled the legacy controllers for resources in the Vectorized Group (Also known as V1 operator mode)")
	cmd.Flags().BoolVar(&o.enableRedpandaControllers, "enable-redpanda-controllers", true, "Specifies whether or not to enabled the Redpanda cluster controllers")
	cmd.Flags().StringVar(&o.clusterDomain, "cluster-domain", "cluster.local", "Set the Kubernetes local domain (Kubelet's --cluster-domain)")
	cmd.Flags().StringVar(&o.configuratorBaseImage, "configurator-base-image", defaultConfiguratorContainerImage, "The repository of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
	cmd.Flags().StringVar(&o.configuratorTag, "configurator-tag", version.Version, "The tag of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
	cmd.Flags().StringVar(&o.redpandaDefaultTag, "redpanda-tag", DefaultRedpandaImageTag, "The default docker image tag for redpanda containers")
	cmd.Flags().StringVar(&o.redpandaDefaultRepository, "redpanda-repository", DefaultRedpandaRepository, "The default docker repository to pull redpanda images from")
	cmd.Flags().StringVar(&o.configuratorImagePullPolicy, "configurator-image-pull-policy", "Always", "Set the configurator image pull policy")
	cmd.Flags().DurationVar(&o.decommissionWaitInterval, "decommission-wait-interval", 8*time.Second, "Set the time to wait for a node decommission to happen in the cluster")
	cmd.Flags().DurationVar(&o.metricsTimeout, "metrics-timeout", 8*time.Second, "Set the timeout for a checking metrics Admin API endpoint. If set to 0, then the 2 seconds default will be used")
	cmd.Flags().DurationVar(&o.rpClientTimeout, "cluster-connection-timeout", 10*time.Second, "Set the timeout for internal clients used to connect to Redpanda clusters")
	cmd.Flags().StringVar(&o.restrictToRedpandaVersion, "restrict-redpanda-version", "", "Restrict management of clusters to those with this version")
	cmd.Flags().BoolVar(&o.ghostbuster, "unsafe-decommission-failed-brokers", false, "Set to enable decommissioning a failed broker that is configured but does not exist in the StatefulSet (ghost broker). This may result in invalidating valid data")
	_ = cmd.Flags().MarkHidden("unsafe-decommission-failed-brokers")
	cmd.Flags().StringSliceVar(&o.additionalControllers, "additional-controllers", []string{""}, fmt.Sprintf("which controllers to run, available: all, %s", strings.Join(availableControllers, ", ")))
	cmd.Flags().DurationVar(&o.unbindPVCsAfter, "unbind-pvcs-after", 0, "if not zero, runs the PVCUnbinder controller which attempts to 'unbind' the PVCs' of Pods that are Pending for longer than the given duration")
	cmd.Flags().BoolVar(&o.allowPVRebinding, "allow-pv-rebinding", false, "controls whether or not PVs unbound by the PVCUnbinder have their .ClaimRef cleared, which allows them to be reused")
	cmd.Flags().Var(&o.unbinderSelector, "unbinder-label-selector", "if provided, a Kubernetes label selector that will filter Pods to be considered by the PVCUnbinder.")
	cmd.Flags().BoolVar(&o.autoDeletePVCs, "auto-delete-pvcs", false, "Use StatefulSet PersistentVolumeClaimRetentionPolicy to auto delete PVCs on scale down and Cluster resource delete.")
	cmd.Flags().BoolVar(&o.enableGhostBrokerDecommissioner, "enable-ghost-broker-decommissioner", false, "Enable ghost broker decommissioner.")
	cmd.Flags().DurationVar(&o.ghostBrokerDecommissionerSyncPeriod, "ghost-broker-decommissioner-sync-period", time.Minute*5, "Ghost broker sync period. The Ghost Broker Decommissioner is guaranteed to be called after this period.")

	// Secret store related flags.
	cmd.Flags().BoolVar(&o.cloudSecretsEnabled, "enable-cloud-secrets", false, "Set to true if config values can reference secrets from cloud secret store")
	cmd.Flags().StringVar(&o.cloudSecretsPrefix, "cloud-secrets-prefix", "", "Prefix for all names of cloud secrets")
	cmd.Flags().StringVar(&o.cloudSecretsConfig.AWSRegion, "cloud-secrets-aws-region", "", "AWS Region in which the secrets are stored")
	cmd.Flags().StringVar(&o.cloudSecretsConfig.AWSRoleARN, "cloud-secrets-aws-role-arn", "", "AWS role ARN to assume when fetching secrets")
	cmd.Flags().StringVar(&o.cloudSecretsConfig.GCPProjectID, "cloud-secrets-gcp-project-id", "", "GCP project ID in which the secrets are stored")
	cmd.Flags().StringVar(&o.cloudSecretsConfig.AzureKeyVaultURI, "cloud-secrets-azure-key-vault-uri", "", "Azure Key Vault URI in which the secrets are stored")

	// Legacy Global flags.
	cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowDownscalingInWebhook, "allow-downscaling", true, "Allow to reduce the number of replicas in existing clusters")
	cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowConsoleAnyNamespace, "allow-console-any-ns", false, "Allow to create Console in any namespace. Allowing this copies Redpanda SchemaRegistry TLS Secret to namespace (alpha feature)")
	cmd.Flags().StringVar(&vectorizedv1alpha1.SuperUsersPrefix, "superusers-prefix", "", "Prefix to add in username of superusers managed by operator. This will only affect new clusters, enabling this will not add prefix to existing clusters (alpha feature)")

	{
		// api server flags
		cmd.Flags().BoolVar(&o.apiServer.enabled, "experimental-api-server-enabled", false, "")
		cmd.Flags().StringVar(&o.apiServer.serviceName, "experimental-api-server-service-name", "", "")
		cmd.Flags().StringVar(&o.apiServer.serviceNamespace, "experimental-api-server-service-namespace", "", "")
		cmd.Flags().StringVar(&o.apiServer.secretKeyName, "experimental-api-server-secret-key-name", "", "")
		cmd.Flags().StringVar(&o.apiServer.secretKeyNamespace, "experimental-api-server-secret-key-namespace", "", "")
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
	for _, c := range o.additionalControllers {
		if Controller(c) == AllNonVectorizedControllers || Controller(c) == controller {
			return true
		}
	}
	return false
}

// APIServices RBAC permissions
// +kubebuilder:rbac:groups=apiregistration.k8s.io,resources=apiservices,verbs=get;list;watch;create;update;patch

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
			if options.cloudSecretsEnabled {
				var err error
				cloudExpander, err = pkgsecrets.NewCloudExpander(ctx, options.cloudSecretsPrefix, options.cloudSecretsConfig)
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
	if err := opts.validateAPIServer(); err != nil {
		return err
	}

	v1Controllers := opts.enableVectorizedControllers
	v2Controllers := opts.enableRedpandaControllers

	if (opts.enableV2NodepoolController || opts.enableConsoleController) && !v2Controllers {
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
	if !opts.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	var webhookServer webhook.Server
	if opts.webhookEnabled {
		// Initial webhook TLS options
		webhookTLSOpts := tlsOpts

		if len(opts.webhookCertPath) > 0 {
			setupLog.Info("Initializing webhook certificate watcher using provided certificates",
				"webhook-cert-path", opts.webhookCertPath, "webhook-cert-name", opts.webhookCertName, "webhook-cert-key", opts.webhookCertKey)

			var err error
			webhookCertWatcher, err = certwatcher.New(
				filepath.Join(opts.webhookCertPath, opts.webhookCertName),
				filepath.Join(opts.webhookCertPath, opts.webhookCertKey),
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
		SecureServing: opts.secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if opts.secureMetrics {
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
	if len(opts.metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", opts.metricsCertPath, "metrics-cert-name", opts.metricsCertName, "metrics-cert-key", opts.metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(opts.metricsCertPath, opts.metricsCertName),
			filepath.Join(opts.metricsCertPath, opts.metricsCertKey),
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
		if opts.namespace == "" {
			opts.managerOptions.LeaderElectionNamespace = "kube-system"
		} else {
			opts.managerOptions.LeaderElectionNamespace = opts.namespace
		}
	}

	if opts.namespace != "" {
		opts.managerOptions.Cache.DefaultNamespaces = map[string]cache.Config{opts.namespace: {}}
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
		CloudSecretsEnabled:          opts.cloudSecretsEnabled,
		CloudSecretsPrefix:           opts.cloudSecretsPrefix,
		CloudSecretsAWSRegion:        opts.cloudSecretsConfig.AWSRegion,
		CloudSecretsAWSRoleARN:       opts.cloudSecretsConfig.AWSRoleARN,
		CloudSecretsGCPProjectID:     opts.cloudSecretsConfig.GCPProjectID,
		CloudSecretsAzureKeyVaultURI: opts.cloudSecretsConfig.AzureKeyVaultURI,
	}

	sidecarImage := lifecycle.Image{
		Repository: opts.configuratorBaseImage,
		Tag:        opts.configuratorTag,
	}

	redpandaImage := lifecycle.Image{
		Repository: opts.redpandaDefaultRepository,
		Tag:        opts.redpandaDefaultTag,
	}

	if v2Controllers {
		// Redpanda Reconciler
		if err := (&redpandacontrollers.RedpandaReconciler{
			Manager:              mcmanager,
			LifecycleClient:      lifecycle.NewResourceClient(mcmanager, lifecycle.V2ResourceManagers(redpandaImage, sidecarImage, cloudSecrets)),
			ClientFactory:        factory,
			CloudSecretsExpander: cloudExpander,
			UseNodePools:         opts.enableV2NodepoolController,
		}).SetupWithManager(ctx, mcmanager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Redpanda")
			return err
		}

		// the following 2 controllers depend on the Redpanda controller being run, so
		// only run them if we run the Redpanda controller

		// NodePool Reconciler
		if opts.enableV2NodepoolController {
			if err := (&redpandacontrollers.NodePoolReconciler{
				Manager: mcmanager,
			}).SetupWithManager(ctx, mcmanager); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "NodePool")
				return err
			}
		}

		// Console Reconciler.
		if opts.enableConsoleController {
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
			DecommissionWaitInterval: opts.decommissionWaitInterval,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DecommissionReconciler")
			return err
		}
	}

	// The unbinder gets to run in any mode, if it's enabled.
	if opts.unbindPVCsAfter <= 0 {
		setupLog.Info("PVCUnbinder controller not active", "unbind-after", opts.unbindPVCsAfter, "selector", opts.unbinderSelector, "allow-pv-rebinding", opts.allowPVRebinding)
	} else {
		setupLog.Info("starting PVCUnbinder controller", "unbind-after", opts.unbindPVCsAfter, "selector", opts.unbinderSelector, "allow-pv-rebinding", opts.allowPVRebinding)

		if err := (&pvcunbinder.Controller{
			Client:         mgr.GetClient(),
			Timeout:        opts.unbindPVCsAfter,
			Selector:       opts.unbinderSelector.Selector,
			AllowRebinding: opts.allowPVRebinding,
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

	if opts.webhookEnabled {
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

	if opts.apiServer.enabled {
		if err := SetupAPIServer(APIServerConfig{
			Manager: mgr,
			ServiceKey: types.NamespacedName{
				Namespace: opts.apiServer.serviceNamespace,
				Name:      opts.apiServer.serviceName,
			},
			SecretKey: types.NamespacedName{
				Namespace: opts.apiServer.secretKeyNamespace,
				Name:      opts.apiServer.secretKeyName,
			},
			Factory: factory,
		}); err != nil {
			setupLog.Error(err, "setting up api server")
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
		ConfiguratorBaseImage:        opts.configuratorBaseImage,
		ConfiguratorTag:              opts.configuratorTag,
		ImagePullPolicy:              corev1.PullPolicy(opts.configuratorImagePullPolicy),
		CloudSecretsEnabled:          opts.cloudSecretsEnabled,
		CloudSecretsPrefix:           opts.cloudSecretsPrefix,
		CloudSecretsAWSRegion:        opts.cloudSecretsConfig.AWSRegion,
		CloudSecretsAWSRoleARN:       opts.cloudSecretsConfig.AWSRoleARN,
		CloudSecretsGCPProjectID:     opts.cloudSecretsConfig.GCPProjectID,
		CloudSecretsAzureKeyVaultURI: opts.cloudSecretsConfig.AzureKeyVaultURI,
	}

	adminAPIClientFactory := adminutils.CachedNodePoolAdminAPIClientFactory(adminutils.NewNodePoolInternalAdminAPI)

	if err := (&vectorizedcontrollers.ClusterReconciler{
		Client:                    mgr.GetClient(),
		Log:                       ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Cluster"),
		Scheme:                    mgr.GetScheme(),
		AdminAPIClientFactory:     adminAPIClientFactory,
		DecommissionWaitInterval:  opts.decommissionWaitInterval,
		MetricsTimeout:            opts.metricsTimeout,
		RestrictToRedpandaVersion: opts.restrictToRedpandaVersion,
		GhostDecommissioning:      opts.ghostbuster,
		AutoDeletePVCs:            opts.autoDeletePVCs,
		CloudSecretsExpander:      cloudExpander,
		Timeout:                   opts.rpClientTimeout,
	}).WithClusterDomain(opts.clusterDomain).WithConfiguratorSettings(configurator).SetupWithManager(mgr); err != nil {
		log.Error(ctx, err, "Unable to create controller", "controller", "Cluster")
		return err
	}

	if err := vectorizedcontrollers.NewClusterMetricsController(mgr.GetClient()).SetupWithManager(mgr); err != nil {
		log.Error(ctx, err, "Unable to create controller", "controller", "ClustersMetrics")
		return err
	}

	// Setup webhooks
	if opts.webhookEnabled {
		log.Info(ctx, "Setup webhook")
		if err := (&vectorizedv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
			log.Error(ctx, err, "Unable to create webhook", "webhook", "RedpandaCluster")
			return err
		}
	}

	if opts.enableGhostBrokerDecommissioner && opts.enableVectorizedControllers {
		adapter := vectorizedDecommissionerAdapter{factory: factory, client: mgr.GetClient()}
		d := decommissioning.NewStatefulSetDecommissioner(
			mgr,
			adapter.getAdminClient,
			decommissioning.WithFilter(adapter.filter),
			// Operator v1 supports multiple NodePools, and therefore multiple STS.
			// This function provides a custom replica count: the desired replicas of all STS, instead of a single STS.
			decommissioning.WithDesiredReplicasFetcher(adapter.desiredReplicas),
			decommissioning.WithSyncPeriod(opts.ghostBrokerDecommissionerSyncPeriod),
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
