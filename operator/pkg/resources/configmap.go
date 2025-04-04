// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/nodepools"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/configuration"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

const (
	baseSuffix                  = "base"
	dataDirectory               = "/var/lib/redpanda/data"
	archivalCacheIndexDirectory = "/var/lib/shadow-index-cache"

	superusersConfigurationKey = "superusers"

	oneMB          = 1024 * 1024
	logSegmentSize = 512 * oneMB

	saslMechanism = "SCRAM-SHA-256"

	configKey                  = "redpanda.yaml"
	bootstrapConfigFile        = ".bootstrap.yaml"
	bootstrapTemplateEnvVar    = "BOOTSTRAP_TEMPLATE"
	bootstrapDestinationEnvVar = "BOOTSTRAP_DESTINATION"
	bootstrapTemplateFile      = ".bootstrap.json.in"
)

var (
	errKeyDoesNotExistInSecretData        = errors.New("cannot find key in secret data")
	errCloudStorageSecretKeyCannotBeEmpty = errors.New("cloud storage SecretKey string cannot be empty")

	// LastAppliedCriticalConfigurationAnnotationKey is used to store the hash of the most-recently-applied configuration,
	// selecting only those values which are marked in the schema as requiring a cluster restart.
	LastAppliedCriticalConfigurationAnnotationKey = vectorizedv1alpha1.GroupVersion.Group + "/last-applied-critical-configuration"
)

var _ Resource = &ConfigMapResource{}

// ConfigMapResource contains definition and reconciliation logic for operator's ConfigMap.
// The ConfigMap contains the configuration as well as init script.
type ConfigMapResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster

	serviceFQDN            string
	pandaproxySASLUser     types.NamespacedName
	schemaRegistrySASLUser types.NamespacedName
	tlsConfigProvider      resourcetypes.BrokerTLSConfigProvider
	logger                 logr.Logger
}

// NewConfigMap creates ConfigMapResource
func NewConfigMap(
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	serviceFQDN string,
	pandaproxySASLUser types.NamespacedName,
	schemaRegistrySASLUser types.NamespacedName,
	tlsConfigProvider resourcetypes.BrokerTLSConfigProvider,
	logger logr.Logger,
) *ConfigMapResource {
	return &ConfigMapResource{
		client,
		scheme,
		pandaCluster,
		serviceFQDN,
		pandaproxySASLUser,
		schemaRegistrySASLUser,
		tlsConfigProvider,
		logger,
	}
}

// Ensure will manage kubernetes v1.ConfigMap for redpanda.vectorized.io CR
func (r *ConfigMapResource) Ensure(ctx context.Context) error {
	obj, err := r.obj(ctx)
	if err != nil {
		return fmt.Errorf("unable to construct object: %w", err)
	}
	created, err := CreateIfNotExists(ctx, r, obj, r.logger)
	if err != nil || created {
		return err
	}
	var cm corev1.ConfigMap
	err = r.Get(ctx, r.Key(), &cm)
	if err != nil {
		return fmt.Errorf("error while fetching ConfigMap resource: %w", err)
	}

	return r.update(ctx, &cm, obj.(*corev1.ConfigMap), r.Client, r.logger)
}

func (r *ConfigMapResource) update(
	ctx context.Context,
	current *corev1.ConfigMap,
	modified *corev1.ConfigMap,
	c k8sclient.Client,
	logger logr.Logger,
) error {
	// Do not touch existing last-applied-configuration (it's not reconciled in the main loop)
	if val, ok := current.Annotations[LastAppliedCriticalConfigurationAnnotationKey]; ok {
		if modified.Annotations == nil {
			modified.Annotations = make(map[string]string)
		}
		modified.Annotations[LastAppliedCriticalConfigurationAnnotationKey] = val
	}

	if err := r.markConfigurationConditionChanged(ctx, current, modified); err != nil {
		return err
	}

	_, err := Update(ctx, current, modified, c, logger)
	return err
}

// markConfigurationConditionChanged verifies and marks the cluster as needing synchronization (using the ClusterConfigured condition).
// The condition is changed so that the configuration controller can later restore it back to normal after interacting with the cluster.
func (r *ConfigMapResource) markConfigurationConditionChanged(
	ctx context.Context, current *corev1.ConfigMap, modified *corev1.ConfigMap,
) error {
	status := r.pandaCluster.Status.GetConditionStatus(vectorizedv1alpha1.ClusterConfiguredConditionType)
	if status == corev1.ConditionFalse {
		// Condition already indicates a change
		return nil
	}

	// If the condition is not present, or it does not currently indicate a change, we check it again
	if !r.globalConfigurationChanged(current, modified) {
		return nil
	}

	r.logger.Info("Detected configuration change in the cluster")

	// We need to mark the cluster as changed to trigger the configuration workflow
	r.pandaCluster.Status.SetCondition(
		vectorizedv1alpha1.ClusterConfiguredConditionType,
		corev1.ConditionFalse,
		vectorizedv1alpha1.ClusterConfiguredReasonUpdating,
		"Detected cluster configuration change that needs to be applied to the cluster",
	)
	return r.Status().Update(ctx, r.pandaCluster)
}

// obj returns resource managed client.Object
func (r *ConfigMapResource) obj(ctx context.Context) (k8sclient.Object, error) {
	conf, err := r.CreateConfiguration(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating configuration: %w", err)
	}

	// TODO: the serialised template needs to turn k8s object references into env vars
	cfgSerialized, err := conf.Serialize()
	if err != nil {
		return nil, fmt.Errorf("serializing: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Key().Namespace,
			Name:      r.Key().Name,
			Labels:    labels.ForCluster(r.pandaCluster),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		Data: map[string]string{},
	}

	if cfgSerialized.RedpandaFile != nil {
		cm.Data[configKey] = string(cfgSerialized.RedpandaFile)
	}
	if cfgSerialized.BootstrapFile != nil {
		cm.Data[bootstrapConfigFile] = string(cfgSerialized.BootstrapFile)
	}
	if cfgSerialized.BootstrapTemplate != nil {
		cm.Data[bootstrapTemplateFile] = string(cfgSerialized.BootstrapTemplate)
	}

	err = controllerutil.SetControllerReference(r.pandaCluster, cm, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return cm, nil
}

// CreateConfiguration creates a global configuration for the current cluster
//
//nolint:funlen // let's keep the configuration in one function for now and refactor later
func (r *ConfigMapResource) CreateConfiguration(
	ctx context.Context,
) (*configuration.GlobalConfiguration, error) {
	cfg := configuration.For(r.pandaCluster.Spec.Version)
	cfg.NodeConfiguration = config.ProdDefault()
	cfg.BootstrapConfiguration = make(map[string]vectorizedv1alpha1.ClusterConfigValue)
	mountPoints := resourcetypes.GetTLSMountPoints()

	c := r.pandaCluster.Spec.Configuration
	cr := &cfg.NodeConfiguration.Redpanda

	internalListener := r.pandaCluster.InternalListener()
	internalAuthN := &internalListener.AuthenticationMethod
	if *internalAuthN == "" {
		internalAuthN = nil
	}
	cr.KafkaAPI = []config.NamedAuthNSocketAddress{} // we don't want to inherit default kafka port
	cr.KafkaAPI = append(cr.KafkaAPI, config.NamedAuthNSocketAddress{
		Address: "0.0.0.0",
		Port:    internalListener.Port,
		Name:    InternalListenerName,
		AuthN:   internalAuthN,
	})

	externalListeners := r.pandaCluster.KafkaAPIExternalListeners()
	for _, externalListener := range externalListeners {
		externalAuthN := &externalListener.AuthenticationMethod
		if *externalAuthN == "" {
			externalAuthN = nil
		}
		cr.KafkaAPI = append(cr.KafkaAPI, config.NamedAuthNSocketAddress{
			Address: "0.0.0.0",
			Port:    calculateExternalPort(internalListener.Port, externalListener.Port),
			Name:    externalListener.Name,
			AuthN:   externalAuthN,
		})
	}

	cr.RPCServer.Port = clusterCRPortOrRPKDefault(c.RPCServer.Port, cr.RPCServer.Port)
	cr.AdvertisedRPCAPI = &config.SocketAddress{
		Address: "0.0.0.0",
		Port:    clusterCRPortOrRPKDefault(c.RPCServer.Port, cr.RPCServer.Port),
	}

	cr.AdminAPI[0].Port = clusterCRPortOrRPKDefault(r.pandaCluster.AdminAPIInternal().Port, cr.AdminAPI[0].Port)
	cr.AdminAPI[0].Name = AdminPortName
	if r.pandaCluster.AdminAPIExternal() != nil {
		externalAdminAPI := config.NamedSocketAddress{
			Address: cr.AdminAPI[0].Address,
			Port:    calculateExternalPort(cr.AdminAPI[0].Port, r.pandaCluster.AdminAPIExternal().Port),
			Name:    AdminPortExternalName,
		}
		cr.AdminAPI = append(cr.AdminAPI, externalAdminAPI)
	}

	cr.DeveloperMode = c.DeveloperMode
	cr.Directory = dataDirectory
	kl := r.pandaCluster.KafkaTLSListeners()
	for i := range kl {
		tls := config.ServerTLS{
			Name:              kl[i].Name,
			KeyFile:           tlsKeyPath(mountPoints.KafkaAPI.NodeCertMountDir),  // tls.key
			CertFile:          tlsCertPath(mountPoints.KafkaAPI.NodeCertMountDir), // tls.crt
			Enabled:           true,
			RequireClientAuth: kl[i].TLS.RequireClientAuth,
		}
		if kl[i].TLS.RequireClientAuth {
			tls.TruststoreFile = tlsCAPath(mountPoints.KafkaAPI.ClientCAMountDir)
		}
		cr.KafkaAPITLS = append(cr.KafkaAPITLS, tls)
	}
	adminAPITLSListener := r.pandaCluster.AdminAPITLS()
	if adminAPITLSListener != nil {
		// Only one TLS listener is supported (restricted by the webhook).
		// Determine the listener name based on being internal or external.
		name := AdminPortName
		if adminAPITLSListener.External.Enabled {
			name = AdminPortExternalName
		}
		adminTLS := config.ServerTLS{
			Name:              name,
			KeyFile:           tlsKeyPath(mountPoints.AdminAPI.NodeCertMountDir),
			CertFile:          tlsCertPath(mountPoints.AdminAPI.NodeCertMountDir),
			Enabled:           true,
			RequireClientAuth: adminAPITLSListener.TLS.RequireClientAuth,
		}
		if adminAPITLSListener.TLS.RequireClientAuth {
			adminTLS.TruststoreFile = tlsCAPath(mountPoints.AdminAPI.ClientCAMountDir)
		}
		cr.AdminAPITLS = append(cr.AdminAPITLS, adminTLS)
	}

	if r.pandaCluster.Spec.CloudStorage.Enabled {
		if err := r.prepareCloudStorage(ctx, cfg); err != nil {
			return nil, fmt.Errorf("preparing cloud storage: %w", err)
		}
	}

	for _, user := range r.pandaCluster.Spec.Superusers {
		if err := cfg.AppendToAdditionalRedpandaProperty(superusersConfigurationKey, user.Username); err != nil {
			return nil, fmt.Errorf("appending superusers property: %w", err)
		}
	}

	if r.pandaCluster.Spec.EnableSASL {
		cfg.SetAdditionalRedpandaProperty("enable_sasl", true)
	}
	if r.pandaCluster.Spec.KafkaEnableAuthorization != nil && *r.pandaCluster.Spec.KafkaEnableAuthorization {
		cfg.SetAdditionalRedpandaProperty("kafka_enable_authorization", true)
	}

	partitions := r.pandaCluster.Spec.Configuration.GroupTopicPartitions
	if partitions != 0 {
		cfg.SetAdditionalRedpandaProperty("group_topic_partitions", partitions)
	}

	cfg.SetAdditionalRedpandaProperty("auto_create_topics_enabled", r.pandaCluster.Spec.Configuration.AutoCreateTopics)

	intervalSec := 60 * 30 // 60s * 30 = 30 minutes
	cfg.SetAdditionalRedpandaProperty("cloud_storage_segment_max_upload_interval_sec", intervalSec)

	cfg.SetAdditionalRedpandaProperty("log_segment_size", logSegmentSize)

	if err := r.PrepareSeedServerList(cr); err != nil {
		return nil, fmt.Errorf("preparing seed server list: %w", err)
	}
	r.preparePandaproxy(cfg.NodeConfiguration)
	r.preparePandaproxyTLS(cfg.NodeConfiguration, mountPoints)
	err := r.preparePandaproxyClient(ctx, cfg, mountPoints)
	if err != nil {
		return nil, fmt.Errorf("preparing proxy client: %w", err)
	}

	for _, sr := range r.pandaCluster.SchemaRegistryListeners() {
		var authN *string
		if sr.AuthenticationMethod != "" {
			authN = &sr.AuthenticationMethod
		}
		cfg.NodeConfiguration.SchemaRegistry.SchemaRegistryAPI = append(cfg.NodeConfiguration.SchemaRegistry.SchemaRegistryAPI,
			config.NamedAuthNSocketAddress{
				Address: "0.0.0.0",
				Port:    sr.Port,
				Name:    sr.Name,
				AuthN:   authN,
			})
	}
	r.prepareSchemaRegistryTLS(cfg.NodeConfiguration, mountPoints)
	err = r.prepareSchemaRegistryClient(ctx, cfg, mountPoints)
	if err != nil {
		return nil, fmt.Errorf("preparing schemaRegistry client: %w", err)
	}

	cfg.SetAdditionalRedpandaProperty("enable_rack_awareness", true)

	if err := cfg.SetAdditionalFlatProperties(r.pandaCluster.Spec.AdditionalConfiguration); err != nil {
		return nil, fmt.Errorf("adding additional flat properties: %w", err)
	}

	if err := cfg.FinalizeToTemplate(); err != nil {
		return nil, err
	}

	// Fold in any last overriding attributes. Prefer the new ClusterConfiguration attribute.
	for k, v := range r.pandaCluster.Spec.ClusterConfiguration {
		cfg.BootstrapConfiguration[k] = *(v.DeepCopy())
	}

	return cfg, nil
}

// calculateExternalPort can calculate external port based on the internal port
// for any listener
func calculateExternalPort(internalPort, specifiedExternalPort int) int {
	if internalPort < 0 || internalPort > 65535 {
		return 0
	}
	if specifiedExternalPort != 0 {
		return specifiedExternalPort
	}
	return internalPort + 1
}

func (r *ConfigMapResource) prepareCloudStorage(
	ctx context.Context, cfg *configuration.GlobalConfiguration,
) error {
	if r.pandaCluster.Spec.CloudStorage.AccessKey != "" {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_access_key", r.pandaCluster.Spec.CloudStorage.AccessKey)
	}
	if r.pandaCluster.Spec.CloudStorage.SecretKeyRef.Name != "" {
		secretName := types.NamespacedName{
			Name:      r.pandaCluster.Spec.CloudStorage.SecretKeyRef.Name,
			Namespace: r.pandaCluster.Spec.CloudStorage.SecretKeyRef.Namespace,
		}
		// We need to retrieve the Secret containing the provided cloud storage secret key and extract the key itself.
		secretKeyStr, err := r.getSecretValue(ctx, secretName, r.pandaCluster.Spec.CloudStorage.SecretKeyRef.Name)
		if err != nil {
			return fmt.Errorf("cannot retrieve cloud storage secret for data archival: %w", err)
		}
		if secretKeyStr == "" {
			return fmt.Errorf("secret name %s, ns %s: %w", secretName.Name, secretName.Namespace, errCloudStorageSecretKeyCannotBeEmpty)
		}

		cfg.SetAdditionalRedpandaProperty("cloud_storage_secret_key", secretKeyStr)
	}

	if r.pandaCluster.Spec.CloudStorage.CredentialsSource != "" {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_credentials_source", string(r.pandaCluster.Spec.CloudStorage.CredentialsSource))
	}

	cfg.SetAdditionalRedpandaProperty("cloud_storage_enabled", r.pandaCluster.Spec.CloudStorage.Enabled)
	cfg.SetAdditionalRedpandaProperty("cloud_storage_region", r.pandaCluster.Spec.CloudStorage.Region)
	cfg.SetAdditionalRedpandaProperty("cloud_storage_bucket", r.pandaCluster.Spec.CloudStorage.Bucket)
	cfg.SetAdditionalRedpandaProperty("cloud_storage_disable_tls", r.pandaCluster.Spec.CloudStorage.DisableTLS)

	interval := r.pandaCluster.Spec.CloudStorage.ReconcilicationIntervalMs
	if interval != 0 {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_reconciliation_interval_ms", interval)
	}
	maxCon := r.pandaCluster.Spec.CloudStorage.MaxConnections
	if maxCon != 0 {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_max_connections", maxCon)
	}
	apiEndpoint := r.pandaCluster.Spec.CloudStorage.APIEndpoint
	if apiEndpoint != "" {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_api_endpoint", apiEndpoint)
	}
	endpointPort := r.pandaCluster.Spec.CloudStorage.APIEndpointPort
	if endpointPort != 0 {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_api_endpoint_port", endpointPort)
	}
	trustfile := r.pandaCluster.Spec.CloudStorage.Trustfile
	if trustfile != "" {
		cfg.SetAdditionalRedpandaProperty("cloud_storage_trust_file", trustfile)
	}

	cfg.NodeConfiguration.Redpanda.CloudStorageCacheDirectory = archivalCacheIndexDirectory

	// Only set cache_size (in bytes) if no percentage based cluster property is set.
	// This avoids problems with multiple node pools: bytes-based is very different if disk size changes (move between different tiers in cloud, for example).
	// Percentage based is kept stable mostly (even exactly the same most of the time, 15% typically), and will not cause immediate churn on the old nodePool if disk size is going down.
	_, cloudStoragePercentageConfigured := r.pandaCluster.Spec.AdditionalConfiguration["redpanda.cloud_storage_cache_size_percent"]

	if r.pandaCluster.Spec.CloudStorage.CacheStorage != nil && r.pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value() > 0 && !cloudStoragePercentageConfigured {
		size := strconv.FormatInt(r.pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value(), 10)
		cfg.SetAdditionalRedpandaProperty("cloud_storage_cache_size", size)
	}

	return nil
}

func (r *ConfigMapResource) preparePandaproxy(cfgRpk *config.RedpandaYaml) {
	internal := r.pandaCluster.PandaproxyAPIInternal()
	if internal == nil {
		return
	}

	var internalAuthN *string
	if internal.AuthenticationMethod != "" {
		internalAuthN = &internal.AuthenticationMethod
	}
	cfgRpk.Pandaproxy.PandaproxyAPI = []config.NamedAuthNSocketAddress{
		{
			Address: "0.0.0.0",
			Port:    internal.Port,
			Name:    PandaproxyPortInternalName,
			AuthN:   internalAuthN,
		},
	}

	for _, external := range r.pandaCluster.PandaproxyAPIExternalListeners() {
		var externalAuthN *string
		if external.AuthenticationMethod != "" {
			externalAuthN = &external.AuthenticationMethod
		}
		cfgRpk.Pandaproxy.PandaproxyAPI = append(cfgRpk.Pandaproxy.PandaproxyAPI,
			config.NamedAuthNSocketAddress{
				Address: "0.0.0.0",
				Port:    calculateExternalPort(internal.Port, external.Port),
				Name:    external.Name,
				AuthN:   externalAuthN,
			})
	}
}

func (r *ConfigMapResource) preparePandaproxyClient(
	ctx context.Context, cfg *configuration.GlobalConfiguration, mountPoints *resourcetypes.TLSMountPoints,
) error {
	if internal := r.pandaCluster.PandaproxyAPIInternal(); internal == nil {
		return nil
	}
	kafkaInternal := r.pandaCluster.InternalListener()
	if kafkaInternal == nil {
		r.logger.Error(errors.New("pandaproxy is missing internal kafka listener. This state is forbidden by the webhook"), "") //nolint:goerr113 // no need for static error
		return nil
	}

	// Point towards redpanda's headless service to utilize K8s native service
	// discovery features. This mitigates the need to update this section of
	// the config when the number of brokers change.
	// Alternatively, localhost could be used to the same end but using DNS
	// might save us in some cases where the local broker is having some
	// trouble and it makes the config more generally reusable.
	cfg.NodeConfiguration.PandaproxyClient = &config.KafkaClient{
		Brokers: []config.SocketAddress{
			{Address: r.serviceFQDN, Port: r.pandaCluster.InternalListener().Port},
		},
	}

	clientBrokerTLS := r.tlsConfigProvider.KafkaClientBrokerTLS(mountPoints)
	if clientBrokerTLS != nil {
		cfg.NodeConfiguration.PandaproxyClient.BrokerTLS = *clientBrokerTLS
	}

	if !r.pandaCluster.IsSASLOnInternalEnabled() {
		return nil
	}

	// Retrieve SCRAM credentials
	var secret corev1.Secret
	err := r.Get(ctx, r.pandaproxySASLUser, &secret)
	if err != nil {
		return err
	}

	// Populate configuration with SCRAM credentials
	username := string(secret.Data[corev1.BasicAuthUsernameKey])
	password := string(secret.Data[corev1.BasicAuthPasswordKey])
	mechanism := saslMechanism
	cfg.NodeConfiguration.PandaproxyClient.SCRAMUsername = &username
	cfg.NodeConfiguration.PandaproxyClient.SCRAMPassword = &password
	cfg.NodeConfiguration.PandaproxyClient.SASLMechanism = &mechanism
	// Add username as superuser
	return cfg.AppendToAdditionalRedpandaProperty(superusersConfigurationKey, username)
}

func (r *ConfigMapResource) prepareSchemaRegistryClient(
	ctx context.Context, cfg *configuration.GlobalConfiguration, mountPoints *resourcetypes.TLSMountPoints,
) error {
	if r.pandaCluster.Spec.Configuration.SchemaRegistry == nil {
		return nil
	}
	kafkaInternal := r.pandaCluster.InternalListener()
	if kafkaInternal == nil {
		r.logger.Error(errors.New("pandaproxy is missing internal kafka listener. This state is forbidden by the webhook"), "") //nolint:goerr113 // no need for static error
		return nil
	}

	// Point towards redpanda's headless service to utilize K8s native service
	// discovery features. This mitigates the need to update this section of
	// the config when the number of brokers change.
	// Alternatively, localhost could be used to the same end but using DNS
	// might save us in some cases where the local broker is having some
	// trouble and it makes the config more generally reusable.
	cfg.NodeConfiguration.SchemaRegistryClient = &config.KafkaClient{
		Brokers: []config.SocketAddress{
			{Address: r.serviceFQDN, Port: r.pandaCluster.InternalListener().Port},
		},
	}

	clientBrokerTLS := r.tlsConfigProvider.KafkaClientBrokerTLS(mountPoints)
	if clientBrokerTLS != nil {
		cfg.NodeConfiguration.SchemaRegistryClient.BrokerTLS = *clientBrokerTLS
	}

	if !r.pandaCluster.IsSASLOnInternalEnabled() {
		return nil
	}

	// Retrieve SCRAM credentials
	var secret corev1.Secret
	err := r.Get(ctx, r.schemaRegistrySASLUser, &secret)
	if err != nil {
		return err
	}

	// Populate configuration with SCRAM credentials
	username := string(secret.Data[corev1.BasicAuthUsernameKey])
	password := string(secret.Data[corev1.BasicAuthPasswordKey])
	mechanism := saslMechanism
	cfg.NodeConfiguration.SchemaRegistryClient.SCRAMUsername = &username
	cfg.NodeConfiguration.SchemaRegistryClient.SCRAMPassword = &password
	cfg.NodeConfiguration.SchemaRegistryClient.SASLMechanism = &mechanism

	// Add username as superuser
	return cfg.AppendToAdditionalRedpandaProperty(superusersConfigurationKey, username)
}

func (r *ConfigMapResource) preparePandaproxyTLS(
	cfgRpk *config.RedpandaYaml, mountPoints *resourcetypes.TLSMountPoints,
) {
	cfgRpk.Pandaproxy.PandaproxyAPITLS = []config.ServerTLS{}
	for _, tlsListener := range r.pandaCluster.PandaproxyAPITLSs() {
		name := PandaproxyPortInternalName
		if tlsListener.External.Enabled {
			name = tlsListener.Name
		}
		tls := config.ServerTLS{
			Name:              name,
			KeyFile:           tlsKeyPath(mountPoints.PandaProxyAPI.NodeCertMountDir),  // tls.key
			CertFile:          tlsCertPath(mountPoints.PandaProxyAPI.NodeCertMountDir), // tls.crt
			Enabled:           true,
			RequireClientAuth: tlsListener.TLS.RequireClientAuth,
		}
		if tlsListener.TLS.RequireClientAuth {
			tls.TruststoreFile = tlsCAPath(mountPoints.PandaProxyAPI.ClientCAMountDir)
		}
		cfgRpk.Pandaproxy.PandaproxyAPITLS = append(cfgRpk.Pandaproxy.PandaproxyAPITLS, tls)
	}
}

func (r *ConfigMapResource) prepareSchemaRegistryTLS(
	cfgRpk *config.RedpandaYaml, mountPoints *resourcetypes.TLSMountPoints,
) {
	cfgRpk.SchemaRegistry.SchemaRegistryAPITLS = []config.ServerTLS{}
	for _, sr := range r.pandaCluster.SchemaRegistryListeners() {
		if sr.TLS == nil {
			continue
		}
		tls := config.ServerTLS{
			Name:              sr.Name,
			KeyFile:           tlsKeyPath(mountPoints.SchemaRegistryAPI.NodeCertMountDir),  // tls.key
			CertFile:          tlsCertPath(mountPoints.SchemaRegistryAPI.NodeCertMountDir), // tls.crt
			Enabled:           true,
			RequireClientAuth: sr.TLS.RequireClientAuth,
		}
		if sr.TLS.RequireClientAuth {
			tls.TruststoreFile = tlsCAPath(mountPoints.SchemaRegistryAPI.ClientCAMountDir)
		}
		cfgRpk.SchemaRegistry.SchemaRegistryAPITLS = append(cfgRpk.SchemaRegistry.SchemaRegistryAPITLS, tls)
	}
}

func (r *ConfigMapResource) getSecretValue(
	ctx context.Context, nsName types.NamespacedName, key string,
) (string, error) {
	var secret corev1.Secret
	err := r.Get(ctx, nsName, &secret)
	if err != nil {
		return "", err
	}

	if v, exists := secret.Data[key]; exists {
		return string(v), nil
	}

	return "", fmt.Errorf("secret name %s, ns %s, data key %s: %w", nsName.Name, nsName.Namespace, key, errKeyDoesNotExistInSecretData)
}

func clusterCRPortOrRPKDefault(clusterPort, defaultPort int) int {
	if clusterPort == 0 {
		return defaultPort
	}

	return clusterPort
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *ConfigMapResource) Key() types.NamespacedName {
	return ConfigMapKey(r.pandaCluster)
}

// ConfigMapKey provides config map name that derived from redpanda.vectorized.io CR
func ConfigMapKey(pandaCluster *vectorizedv1alpha1.Cluster) types.NamespacedName {
	return types.NamespacedName{Name: resourceNameTrim(pandaCluster.Name, baseSuffix), Namespace: pandaCluster.Namespace}
}

// TODO move to utilities
var letters = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func generatePassword(length int) (string, error) {
	pwdBytes := make([]byte, length)

	if _, err := rand.Read(pwdBytes); err != nil {
		return "", err
	}

	for i, b := range pwdBytes {
		pwdBytes[i] = letters[b%byte(len(letters))]
	}

	return string(pwdBytes), nil
}

// GetNodeConfigHash returns md5 hash of the configuration.
// For clusters without centralized configuration, it computes a hash of the plain "redpanda.yaml" file.
// When using centralized configuration, it only takes into account node properties.
func (r *ConfigMapResource) GetNodeConfigHash(
	ctx context.Context,
) (string, error) {
	cfg, err := r.CreateConfiguration(ctx)
	if err != nil {
		return "", err
	}
	return cfg.GetNodeConfigurationHash()
}

// globalConfigurationChanged verifies if the new global configuration
// is different from the one in the previous version of the ConfigMap
func (r *ConfigMapResource) globalConfigurationChanged(
	current *corev1.ConfigMap, modified *corev1.ConfigMap,
) bool {
	// TODO: this is a short-term change; it won't detect a change in any referenced secrets
	oldConfigNode := current.Data[configKey]
	oldConfigBootstrap := current.Data[bootstrapTemplateFile]

	newConfigNode := modified.Data[configKey]
	newConfigBootstrap := modified.Data[bootstrapTemplateFile]

	return newConfigNode != oldConfigNode || newConfigBootstrap != oldConfigBootstrap
}

// GetAnnotationFromCluster returns the last applied configuration from the configmap,
// together with information about the presence of the configmap itself.
func (r *ConfigMapResource) GetAnnotationFromCluster(
	ctx context.Context,
	annotation string,
) (annotationValue *string, configmapExists bool, err error) {
	existing := corev1.ConfigMap{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		if apierrors.IsNotFound(err) {
			// No keys have been used previously
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("could not load configmap for reading last applied configuration: %w", err)
	}
	if ann, ok := existing.Annotations[annotation]; ok {
		return &ann, true, nil
	}
	return nil, true, nil
}

// SetAnnotationForCluster sets or updates an annotation in the configmap
func (r *ConfigMapResource) SetAnnotationForCluster(
	ctx context.Context, annotation string, newValue *string,
) error {
	existing := corev1.ConfigMap{}
	if err := r.Client.Get(ctx, r.Key(), &existing); err != nil {
		return fmt.Errorf("could not load configmap for storing last applied configuration: %w", err)
	}
	existingValue, found := existing.Annotations[annotation]
	if newValue == nil {
		if !found {
			return nil
		}
		delete(existing.Annotations, annotation)
	} else {
		if existingValue == *newValue {
			return nil
		}
		existing.Annotations[annotation] = *newValue
	}
	return r.Update(ctx, &existing)
}

// PrepareSeedServerList - supports only > 22.3 (featuregates.EmptySeedStartCluster(r.pandaCluster.Spec.Version))
func (r *ConfigMapResource) PrepareSeedServerList(cr *config.RedpandaNodeConfig) error {
	var addresses []string

	// Add addresses based on spec. Since we call GetNodePools, deleted NodePools
	// are included already, so there's no extra code required here to handle
	// this case.
	for i := int32(0); i < ptr.Deref(r.pandaCluster.Spec.Replicas, 0); i++ {
		addresses = append(addresses, fmt.Sprintf("%s-%d.%s", r.pandaCluster.Name, i, r.serviceFQDN))
	}
	nps, err := nodepools.GetNodePools(context.Background(), r.pandaCluster, r)
	if err != nil {
		return err
	}
	for _, np := range nps {
		if np.Name == vectorizedv1alpha1.DefaultNodePoolName {
			continue
		}
		prefix := fmt.Sprintf("%s-%s", r.pandaCluster.Name, np.Name)
		for i := int32(0); i < ptr.Deref(np.Replicas, 0); i++ {
			addresses = append(addresses, fmt.Sprintf("%s-%d.%s", prefix, i, r.serviceFQDN))
		}
	}

	for _, address := range addresses {
		cr.SeedServers = append(cr.SeedServers, config.SeedServer{
			Host: config.SocketAddress{
				// Example address: cluster-sample-0.cluster-sample.default.svc.cluster.local
				Address: address,
				Port:    clusterCRPortOrRPKDefault(r.pandaCluster.Spec.Configuration.RPCServer.Port, cr.RPCServer.Port),
			},
		})
	}

	// We require >= 22.3 so we can configure empty_seed_starts_clusters.
	cr.EmptySeedStartsCluster = new(bool) // default to false

	if len(cr.SeedServers) == 0 {
		return fmt.Errorf("ended up with empty seed list, aborting. require at least one replica")
	}

	return nil
}

func tlsKeyPath(dir string) string {
	return fmt.Sprintf("%s/%s", dir, corev1.TLSPrivateKeyKey)
}

func tlsCertPath(dir string) string {
	return fmt.Sprintf("%s/%s", dir, corev1.TLSCertKey)
}

func tlsCAPath(dir string) string {
	return fmt.Sprintf("%s/%s", dir, cmmetav1.TLSCAKey)
}
