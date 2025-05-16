// Copyright 2025 Redpanda Data, Inc.
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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	helmkube "helm.sh/helm/v3/pkg/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	kubeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/decommissioning"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/nodewatcher"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/olddecommission"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	vectorizedcontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	consolepkg "github.com/redpanda-data/redpanda-operator/operator/pkg/console"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
	redpandawebhooks "github.com/redpanda-data/redpanda-operator/operator/webhooks/redpanda"
)

type RedpandaController string

type OperatorState string

func (r RedpandaController) toString() string {
	return string(r)
}

const (
	defaultConfiguratorContainerImage = "docker.redpanda.com/redpandadata/redpanda-operator"

	AllControllers         = RedpandaController("all")
	NodeController         = RedpandaController("nodeWatcher")
	DecommissionController = RedpandaController("decommission")

	OperatorV1Mode          = OperatorState("Clustered-v1")
	OperatorV2Mode          = OperatorState("Namespaced-v2")
	NamespaceControllerMode = OperatorState("Namespaced-Controllers")

	controllerName = "redpanda-controller"
)

var availableControllers = []string{
	NodeController.toString(),
	DecommissionController.toString(),
}

type LabelSelectorValue struct {
	Selector labels.Selector
}

var _ pflag.Value = ((*LabelSelectorValue)(nil))

func (s *LabelSelectorValue) Set(value string) error {
	if value == "" {
		return nil
	}
	var err error
	s.Selector, err = labels.Parse(value)
	return err
}

func (s *LabelSelectorValue) String() string {
	if s.Selector == nil {
		return ""
	}
	return s.Selector.String()
}

func (s *LabelSelectorValue) Type() string {
	return "label selector"
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
	var (
		clusterDomain                       string
		metricsAddr                         string
		secureMetrics                       bool
		enableHTTP2                         bool
		probeAddr                           string
		pprofAddr                           string
		enableLeaderElection                bool
		webhookEnabled                      bool
		configuratorBaseImage               string
		configuratorTag                     string
		configuratorImagePullPolicy         string
		decommissionWaitInterval            time.Duration
		metricsTimeout                      time.Duration
		rpClientTimeout                     time.Duration
		restrictToRedpandaVersion           string
		namespace                           string
		additionalControllers               []string
		operatorMode                        bool
		ghostbuster                         bool
		unbindPVCsAfter                     time.Duration
		unbinderSelector                    LabelSelectorValue
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
		cloudSecretsAWSRegion               string
		cloudSecretsAWSRoleARN              string
		cloudSecretsGCPProjectID            string
		cloudSecretsAzureKeyVaultURI        string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the redpanda operator",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var cloudExpander *pkgsecrets.CloudExpander
			if cloudSecretsEnabled {
				cloudConfig := pkgsecrets.ExpanderCloudConfiguration{}
				if cloudSecretsAWSRegion != "" {
					cloudConfig.AWSRegion = cloudSecretsAWSRegion
					// if AWSRoleARN is empty, it uses the assumed role of the pod
					cloudConfig.AWSRoleARN = cloudSecretsAWSRoleARN
				} else if cloudSecretsGCPProjectID != "" {
					cloudConfig.GCPProjectID = cloudSecretsGCPProjectID
				} else if cloudSecretsAzureKeyVaultURI != "" {
					cloudConfig.AzureKeyVaultURI = cloudSecretsAzureKeyVaultURI
				} else {
					return errors.New("Cloud secrets are enabled but configuration for cloud provider is missing or invalid")
				}
				var err error
				cloudExpander, err = pkgsecrets.NewCloudExpander(ctx, cloudSecretsPrefix, cloudConfig)
				if err != nil {
					return err
				}
			}

			return Run(
				ctx,
				clusterDomain,
				metricsAddr,
				secureMetrics,
				enableHTTP2,
				probeAddr,
				enableLeaderElection,
				webhookEnabled,
				configuratorBaseImage,
				configuratorTag,
				configuratorImagePullPolicy,
				decommissionWaitInterval,
				metricsTimeout,
				restrictToRedpandaVersion,
				namespace,
				additionalControllers,
				operatorMode,
				ghostbuster,
				unbindPVCsAfter,
				unbinderSelector.Selector,
				autoDeletePVCs,
				pprofAddr,
				webhookCertPath,
				webhookCertName,
				webhookCertKey,
				metricsCertPath,
				metricsCertName,
				metricsCertKey,
				enableGhostBrokerDecommissioner,
				ghostBrokerDecommissionerSyncPeriod,
				cloudExpander,
				cloudSecretsEnabled,
				cloudSecretsPrefix,
				cloudSecretsAWSRegion,
				cloudSecretsAWSRoleARN,
				cloudSecretsGCPProjectID,
				cloudSecretsAzureKeyVaultURI,
				rpClientTimeout,
			)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().BoolVar(&secureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().BoolVar(&enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&pprofAddr, "pprof-bind-address", ":8082", "The address the metric endpoint binds to. Set to '' or 0 to disable")
	cmd.Flags().StringVar(&clusterDomain, "cluster-domain", "cluster.local", "Set the Kubernetes local domain (Kubelet's --cluster-domain)")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().BoolVar(&webhookEnabled, "webhook-enabled", false, "Enable webhook Manager")
	cmd.Flags().StringVar(&configuratorBaseImage, "configurator-base-image", defaultConfiguratorContainerImage, "The repository of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
	cmd.Flags().StringVar(&configuratorTag, "configurator-tag", version.Version, "The tag of the operator container image for use in self-referential deployments, such as the configurator and sidecar")
	cmd.Flags().StringVar(&configuratorImagePullPolicy, "configurator-image-pull-policy", "Always", "Set the configurator image pull policy")
	cmd.Flags().DurationVar(&decommissionWaitInterval, "decommission-wait-interval", 8*time.Second, "Set the time to wait for a node decommission to happen in the cluster")
	cmd.Flags().DurationVar(&metricsTimeout, "metrics-timeout", 8*time.Second, "Set the timeout for a checking metrics Admin API endpoint. If set to 0, then the 2 seconds default will be used")
	cmd.Flags().DurationVar(&rpClientTimeout, "cluster-connection-timeout", 10*time.Second, "Set the timeout for internal clients used to connect to Redpanda clusters")
	cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowDownscalingInWebhook, "allow-downscaling", true, "Allow to reduce the number of replicas in existing clusters")
	cmd.Flags().Bool("allow-pvc-deletion", false, "Deprecated: Ignored if specified")
	cmd.Flags().BoolVar(&vectorizedv1alpha1.AllowConsoleAnyNamespace, "allow-console-any-ns", false, "Allow to create Console in any namespace. Allowing this copies Redpanda SchemaRegistry TLS Secret to namespace (alpha feature)")
	cmd.Flags().StringVar(&restrictToRedpandaVersion, "restrict-redpanda-version", "", "Restrict management of clusters to those with this version")
	cmd.Flags().StringVar(&vectorizedv1alpha1.SuperUsersPrefix, "superusers-prefix", "", "Prefix to add in username of superusers managed by operator. This will only affect new clusters, enabling this will not add prefix to existing clusters (alpha feature)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "If namespace is set to not empty value, it changes scope of Redpanda operator to work in single namespace")
	cmd.Flags().BoolVar(&ghostbuster, "unsafe-decommission-failed-brokers", false, "Set to enable decommissioning a failed broker that is configured but does not exist in the StatefulSet (ghost broker). This may result in invalidating valid data")
	_ = cmd.Flags().MarkHidden("unsafe-decommission-failed-brokers")
	cmd.Flags().StringSliceVar(&additionalControllers, "additional-controllers", []string{""}, fmt.Sprintf("which controllers to run, available: all, %s", strings.Join(availableControllers, ", ")))
	cmd.Flags().BoolVar(&operatorMode, "operator-mode", true, "enables to run as an operator, setting this to false will disable cluster (deprecated), redpanda resources reconciliation.")
	cmd.Flags().DurationVar(&unbindPVCsAfter, "unbind-pvcs-after", 0, "if not zero, runs the PVCUnbinder controller which attempts to 'unbind' the PVCs' of Pods that are Pending for longer than the given duration")
	cmd.Flags().Var(&unbinderSelector, "unbinder-label-selector", "if provided, a Kubernetes label selector that will filter Pods to be considered by the PVCUnbinder.")
	cmd.Flags().BoolVar(&autoDeletePVCs, "auto-delete-pvcs", false, "Use StatefulSet PersistentVolumeClaimRetentionPolicy to auto delete PVCs on scale down and Cluster resource delete.")
	cmd.Flags().StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	cmd.Flags().StringVar(&metricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	cmd.Flags().StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	cmd.Flags().StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	cmd.Flags().BoolVar(&enableGhostBrokerDecommissioner, "enable-ghost-broker-decommissioner", false, "Enable ghost broker decommissioner.")
	cmd.Flags().DurationVar(&ghostBrokerDecommissionerSyncPeriod, "ghost-broker-decommissioner-sync-period", time.Minute*5, "Ghost broker sync period. The Ghost Broker Decommissioner is guaranteed to be called after this period.")

	// secret store related flags
	cmd.Flags().BoolVar(&cloudSecretsEnabled, "enable-cloud-secrets", false, "Set to true if config values can reference secrets from cloud secret store")
	cmd.Flags().StringVar(&cloudSecretsPrefix, "cloud-secrets-prefix", "", "Prefix for all names of cloud secrets")
	cmd.Flags().StringVar(&cloudSecretsAWSRegion, "cloud-secrets-aws-region", "", "AWS Region in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAWSRoleARN, "cloud-secrets-aws-role-arn", "", "AWS role ARN to assume when fetching secrets")
	cmd.Flags().StringVar(&cloudSecretsGCPProjectID, "cloud-secrets-gcp-project-id", "", "GCP project ID in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAzureKeyVaultURI, "cloud-secrets-azure-key-vault-uri", "", "Azure Key Vault URI in which the secrets are stored")

	// Deprecated flags.
	cmd.Flags().Bool("debug", false, "A deprecated and unused flag")
	cmd.Flags().String("events-addr", "", "A deprecated and unused flag")
	cmd.Flags().Bool("enable-helm-controllers", false, "A deprecated and unused flag")
	cmd.Flags().String("helm-repository-url", "https://charts.redpanda.com/", "A deprecated and unused flag")
	cmd.Flags().Bool("force-defluxed-mode", false, "A deprecated and unused flag")

	return cmd
}

type v1Fetcher struct {
	client kubeClient.Client
}

func (f *v1Fetcher) FetchLatest(ctx context.Context, name, namespace string) (any, error) {
	var vectorizedCluster vectorizedv1alpha1.Cluster
	if err := f.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, &vectorizedCluster); err != nil {
		return nil, err
	}
	return &vectorizedCluster, nil
}

//nolint:funlen,gocyclo // length looks good
func Run(
	ctx context.Context,
	clusterDomain string,
	metricsAddr string,
	secureMetrics bool,
	enableHTTP2 bool,
	probeAddr string,
	enableLeaderElection bool,
	webhookEnabled bool,
	configuratorBaseImage string,
	configuratorTag string,
	configuratorImagePullPolicy string,
	decommissionWaitInterval time.Duration,
	metricsTimeout time.Duration,
	restrictToRedpandaVersion string,
	namespace string,
	additionalControllers []string,
	operatorMode bool,
	ghostbuster bool,
	unbindPVCsAfter time.Duration,
	unbinderSelector labels.Selector,
	autoDeletePVCs bool,
	pprofAddr string,
	webhookCertPath string,
	webhookCertName string,
	webhookCertKey string,
	metricsCertPath string,
	metricsCertName string,
	metricsCertKey string,
	enableGhostBrokerDecommissioner bool,
	ghostBrokerDecommissionerSyncPeriod time.Duration,
	cloudExpander *pkgsecrets.CloudExpander,
	cloudSecretsEnabled bool,
	cloudSecretsPrefix string,
	cloudSecretsAWSRegion string,
	cloudSecretsAWSRoleARN string,
	cloudSecretsGCPProjectID string,
	cloudSecretsAzureKeyVaultURI string,
	rpClientTimeout time.Duration,
) error {
	setupLog := ctrl.LoggerFrom(ctx).WithName("setup")

	// set the managedFields owner for resources reconciled from Helm charts
	helmkube.ManagedFieldsManager = controllerName

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
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	var webhookServer webhook.Server
	if webhookEnabled {
		// Initial webhook TLS options
		webhookTLSOpts := tlsOpts

		if len(webhookCertPath) > 0 {
			setupLog.Info("Initializing webhook certificate watcher using provided certificates",
				"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

			var err error
			webhookCertWatcher, err = certwatcher.New(
				filepath.Join(webhookCertPath, webhookCertName),
				filepath.Join(webhookCertPath, webhookCertKey),
			)
			if err != nil {
				setupLog.Error(err, "Failed to initialize webhook certificate watcher")
				os.Exit(1)
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
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
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
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}
		go func() {
			setupLog.Error(metricsCertWatcher.Start(ctx), "metrics cert watcher exits")
		}()

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgrOptions := ctrl.Options{
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "aa9fc693.vectorized.io",
		LeaderElectionNamespace: namespace,
		Metrics:                 metricsServerOptions,
		PprofBindAddress:        pprofAddr,
		WebhookServer:           webhookServer,
	}
	if namespace != "" {
		mgrOptions.Cache.DefaultNamespaces = map[string]cache.Config{namespace: {}}
	}

	configurator := resources.ConfiguratorSettings{
		ConfiguratorBaseImage:        configuratorBaseImage,
		ConfiguratorTag:              configuratorTag,
		ImagePullPolicy:              corev1.PullPolicy(configuratorImagePullPolicy),
		CloudSecretsEnabled:          cloudSecretsEnabled,
		CloudSecretsPrefix:           cloudSecretsPrefix,
		CloudSecretsAWSRegion:        cloudSecretsAWSRegion,
		CloudSecretsAWSRoleARN:       cloudSecretsAWSRoleARN,
		CloudSecretsGCPProjectID:     cloudSecretsGCPProjectID,
		CloudSecretsAzureKeyVaultURI: cloudSecretsAzureKeyVaultURI,
	}

	// init running state values if we are not in operator mode
	var operatorRunningState OperatorState
	if namespace != "" {
		operatorRunningState = NamespaceControllerMode
	}

	// but if we are in operator mode, then the run state is different
	if operatorMode {
		operatorRunningState = OperatorV1Mode
		if namespace != "" {
			operatorRunningState = OperatorV2Mode
		}
	}

	scheme := controller.UnifiedScheme
	mgrOptions.Scheme = scheme

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		return err
	}

	// Now we start different processes depending on state
	switch operatorRunningState {
	case OperatorV1Mode:
		ctrl.Log.Info("running in v1", "mode", OperatorV1Mode)

		adminAPIClientFactory := adminutils.CachedNodePoolAdminAPIClientFactory(adminutils.NewNodePoolInternalAdminAPI)

		if err = (&vectorizedcontrollers.ClusterReconciler{
			Client:                    mgr.GetClient(),
			Log:                       ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Cluster"),
			Scheme:                    mgr.GetScheme(),
			AdminAPIClientFactory:     adminAPIClientFactory,
			DecommissionWaitInterval:  decommissionWaitInterval,
			MetricsTimeout:            metricsTimeout,
			RestrictToRedpandaVersion: restrictToRedpandaVersion,
			GhostDecommissioning:      ghostbuster,
			AutoDeletePVCs:            autoDeletePVCs,
			CloudSecretsExpander:      cloudExpander,
			Timeout:                   rpClientTimeout,
		}).WithClusterDomain(clusterDomain).WithConfiguratorSettings(configurator).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Cluster")
			return err
		}

		if err = vectorizedcontrollers.NewClusterMetricsController(mgr.GetClient()).
			SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "ClustersMetrics")
			return err
		}

		if err = (&vectorizedcontrollers.ConsoleReconciler{
			Client:                  mgr.GetClient(),
			Scheme:                  mgr.GetScheme(),
			Log:                     ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Console"),
			AdminAPIClientFactory:   adminAPIClientFactory,
			Store:                   consolepkg.NewStore(mgr.GetClient(), mgr.GetScheme()),
			EventRecorder:           mgr.GetEventRecorderFor("Console"),
			KafkaAdminClientFactory: consolepkg.NewKafkaAdmin,
		}).WithClusterDomain(clusterDomain).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Console")
			return err
		}

		if err = (&redpandacontrollers.TopicReconciler{
			Client:        mgr.GetClient(),
			Factory:       internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithAdminClientTimeout(rpClientTimeout),
			Scheme:        mgr.GetScheme(),
			EventRecorder: mgr.GetEventRecorderFor("TopicReconciler"),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Topic")
			return err
		}

		// Setup webhooks
		if webhookEnabled {
			setupLog.Info("Setup webhook")
			if err = (&vectorizedv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "Unable to create webhook", "webhook", "RedpandaCluster")
				return err
			}
			hookServer := mgr.GetWebhookServer()
			hookServer.Register("/mutate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
				Handler: &redpandawebhooks.ConsoleDefaulter{
					Client:  mgr.GetClient(),
					Decoder: admission.NewDecoder(scheme),
				},
			})
			hookServer.Register("/validate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
				Handler: &redpandawebhooks.ConsoleValidator{
					Client:  mgr.GetClient(),
					Decoder: admission.NewDecoder(scheme),
				},
			})
		}
	case OperatorV2Mode:
		ctrl.Log.Info("running in v2", "mode", OperatorV2Mode, "namespace", namespace)

		factory := internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithAdminClientTimeout(rpClientTimeout)

		cloudSecrets := lifecycle.CloudSecretsFlags{
			CloudSecretsEnabled:          cloudSecretsEnabled,
			CloudSecretsPrefix:           cloudSecretsPrefix,
			CloudSecretsAWSRegion:        cloudSecretsAWSRegion,
			CloudSecretsAWSRoleARN:       cloudSecretsAWSRoleARN,
			CloudSecretsGCPProjectID:     cloudSecretsGCPProjectID,
			CloudSecretsAzureKeyVaultURI: cloudSecretsAzureKeyVaultURI,
		}
		redpandaImage := lifecycle.Image{
			Repository: configuratorBaseImage,
			Tag:        configuratorTag,
		}

		// Redpanda Reconciler
		if err = (&redpandacontrollers.RedpandaReconciler{
			KubeConfig:           mgr.GetConfig(),
			Client:               mgr.GetClient(),
			EventRecorder:        mgr.GetEventRecorderFor("RedpandaReconciler"),
			LifecycleClient:      lifecycle.NewResourceClient(mgr, lifecycle.V2ResourceManagers(redpandaImage, cloudSecrets)),
			ClientFactory:        factory,
			CloudSecretsExpander: cloudExpander,
		}).SetupWithManager(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Redpanda")
			return err
		}

		if err = (&redpandacontrollers.TopicReconciler{
			Client:        mgr.GetClient(),
			Factory:       factory,
			Scheme:        mgr.GetScheme(),
			EventRecorder: mgr.GetEventRecorderFor("TopicReconciler"),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Topic")
			return err
		}

		if err = redpandacontrollers.SetupUserController(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "User")
			return err
		}

		if err = redpandacontrollers.SetupSchemaController(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Schema")
			return err
		}

		if runThisController(NodeController, additionalControllers) {
			if err = (&nodewatcher.RedpandaNodePVCReconciler{
				Client:       mgr.GetClient(),
				OperatorMode: operatorMode,
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "RedpandaNodePVCReconciler")
				return err
			}
		}

		if runThisController(DecommissionController, additionalControllers) {
			if err = (&olddecommission.DecommissionReconciler{
				Client:                   mgr.GetClient(),
				OperatorMode:             operatorMode,
				DecommissionWaitInterval: decommissionWaitInterval,
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "DecommissionReconciler")
				return err
			}
		}

		if webhookEnabled {
			setupLog.Info("Setup Redpanda conversion webhook")
			if err = (&redpandav1alpha2.Redpanda{}).SetupWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "Unable to create webhook", "webhook", "RedpandaConversion")
				return err
			}
		}

	case NamespaceControllerMode:
		ctrl.Log.Info("running as a namespace controller", "mode", NamespaceControllerMode, "namespace", namespace)
		if runThisController(NodeController, additionalControllers) {
			if err = (&nodewatcher.RedpandaNodePVCReconciler{
				Client:       mgr.GetClient(),
				OperatorMode: operatorMode,
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "RedpandaNodePVCReconciler")
				return err
			}
		}

		if runThisController(DecommissionController, additionalControllers) {
			if err = (&olddecommission.DecommissionReconciler{
				Client:                   mgr.GetClient(),
				OperatorMode:             operatorMode,
				DecommissionWaitInterval: decommissionWaitInterval,
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "DecommissionReconciler")
				return err
			}
		}
	default:
		err := errors.New("unable to run operator without specifying an operator state")
		setupLog.Error(err, "shutting down")
		return err
	}

	// The unbinder gets to run in any mode, if it's enabled.
	if unbindPVCsAfter <= 0 {
		setupLog.Info("PVCUnbinder controller not active", "unbind-after", unbindPVCsAfter, "selector", unbinderSelector)
	} else {
		setupLog.Info("starting PVCUnbinder controller", "unbind-after", unbindPVCsAfter, "selector", unbinderSelector)

		if err := (&pvcunbinder.Controller{
			Client:   mgr.GetClient(),
			Timeout:  unbindPVCsAfter,
			Selector: unbinderSelector,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PVCUnbinder")
			return err
		}
	}

	if enableGhostBrokerDecommissioner {
		d := decommissioning.NewStatefulSetDecommissioner(mgr, &v1Fetcher{client: mgr.GetClient()},
			decommissioning.WithSyncPeriod(ghostBrokerDecommissionerSyncPeriod),
			decommissioning.WithCleanupPVCs(false),
			decommissioning.WithFactory(internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient())),
			decommissioning.WithFilter(func(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
				idx := slices.IndexFunc(
					sts.OwnerReferences,
					func(ownerRef metav1.OwnerReference) bool {
						return ownerRef.APIVersion == vectorizedv1alpha1.GroupVersion.String() && ownerRef.Kind == "Cluster"
					})
				if idx == -1 {
					return false, nil
				}

				var vectorizedCluster vectorizedv1alpha1.Cluster
				if err := mgr.GetClient().Get(ctx, types.NamespacedName{
					Name:      sts.OwnerReferences[idx].Name,
					Namespace: sts.Namespace,
				}, &vectorizedCluster); err != nil {
					return false, fmt.Errorf("could not get Cluster: %w", err)
				}

				managedAnnotationKey := vectorizedv1alpha1.GroupVersion.Group + "/managed"
				if managed, exists := vectorizedCluster.Annotations[managedAnnotationKey]; exists && managed == "false" {
					ctrl.Log.V(1).Info("ignoring StatefulSet of unmanaged V1 Cluster", "sts", sts.Name, "namespace", sts.Namespace)
					return false, nil
				}

				return true, nil
			}),
		)
		if err := d.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "StatefulSetDecommissioner")
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

	if webhookEnabled {
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

func runThisController(rc RedpandaController, controllers []string) bool {
	if len(controllers) == 0 {
		return false
	}

	for _, c := range controllers {
		if RedpandaController(c) == AllControllers || RedpandaController(c) == rc {
			return true
		}
	}
	return false
}
