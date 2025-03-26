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

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/nodepools"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/configuration"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// NewConfigMap creates ConfigMapResource
func CreateConfiguration(
	ctx context.Context,
	logger logr.Logger,
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	serviceFQDN string,
	pandaproxySASLUser types.NamespacedName,
	schemaRegistrySASLUser types.NamespacedName,
	tlsConfigProvider resourcetypes.BrokerTLSConfigProvider,
) (*configuration.nodeCfg, *configuration.clusterCfg, error) {
	nodeCfg := configuration.NewNodeCfg()
	clusterCfg := configuration.newClusterCfg()

	mountPoints := resourcetypes.GetTLSMountPoints()

	c := pandaCluster.Spec.Configuration
	cr := &nodeCfg.Cfg.Redpanda

	internalListener := pandaCluster.InternalListener()
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

	externalListeners := pandaCluster.KafkaAPIExternalListeners()
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

	cr.AdminAPI[0].Port = clusterCRPortOrRPKDefault(pandaCluster.AdminAPIInternal().Port, cr.AdminAPI[0].Port)
	cr.AdminAPI[0].Name = AdminPortName
	if pandaCluster.AdminAPIExternal() != nil {
		externalAdminAPI := config.NamedSocketAddress{
			Address: cr.AdminAPI[0].Address,
			Port:    calculateExternalPort(cr.AdminAPI[0].Port, pandaCluster.AdminAPIExternal().Port),
			Name:    AdminPortExternalName,
		}
		cr.AdminAPI = append(cr.AdminAPI, externalAdminAPI)
	}

	cr.DeveloperMode = c.DeveloperMode
	cr.Directory = dataDirectory
	kl := pandaCluster.KafkaTLSListeners()
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
	adminAPITLSListener := pandaCluster.AdminAPITLS()
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

	if pandaCluster.Spec.CloudStorage.Enabled {
		if err := prepareCloudStorage(ctx, nodeCfg, clusterCfg, pandaCluster); err != nil {
			return nil, nil, fmt.Errorf("preparing cloud storage: %w", err)
		}
	}

	for _, user := range pandaCluster.Spec.Superusers {
		if err := configuration.AppendValue(clusterCfg, superusersConfigurationKey, user.Username); err != nil {
			return nil, nil, fmt.Errorf("appending superusers property: %w", err)
		}
	}

	if pandaCluster.Spec.EnableSASL {
		if err := configuration.SetValue(clusterCfg, "enable_sasl", true); err != nil {
			return nil, nil, fmt.Errorf("setting enable_sasl: %w", err)
		}
	}
	if pandaCluster.Spec.KafkaEnableAuthorization != nil && *pandaCluster.Spec.KafkaEnableAuthorization {
		if err := configuration.SetValue(clusterCfg, "kafka_enable_authorization", true); err != nil {
			return nil, nil, fmt.Errorf("setting kafka_enable_authorization: %w", err)
		}
	}

	partitions := pandaCluster.Spec.Configuration.GroupTopicPartitions
	if partitions != 0 {
		if err := configuration.SetValue(clusterCfg, "group_topic_partitions", partitions); err != nil {
			return nil, nil, fmt.Errorf("setting group_topic_partitions: %w", err)
		}
	}

	if err := configuration.SetValue(clusterCfg, "auto_create_topics_enabled", pandaCluster.Spec.Configuration.AutoCreateTopics); err != nil {
		return nil, nil, fmt.Errorf("setting auto_create_topics_enabled: %w", err)
	}

	intervalSec := 60 * 30 // 60s * 30 = 30 minutes
	if err := configuration.SetValue(clusterCfg, "cloud_storage_segment_max_upload_interval_sec", intervalSec); err != nil {
		return nil, nil, fmt.Errorf("setting cloud_storage_segment_max_upload_interval_sec: %w", err)
	}

	if err := configuration.SetValue(clusterCfg, "log_segment_size", logSegmentSize); err != nil {
		return nil, nil, fmt.Errorf("setting log_segment_size: %w", err)
	}

	if err := PrepareSeedServerList(cr); err != nil {
		return nil, nil, fmt.Errorf("preparing seed server list: %w", err)
	}
	preparePandaproxy(nodeCfg.Cfg, pandaCluster)
	preparePandaproxyTLS(nodeCfg, mountPoints)
	err := preparePandaproxyClient(ctx, nodeCfg, clusterCfg, mountPoints)
	if err != nil {
		return nil, nil, fmt.Errorf("preparing proxy client: %w", err)
	}

	for _, sr := range pandaCluster.SchemaRegistryListeners() {
		var authN *string
		if sr.AuthenticationMethod != "" {
			authN = &sr.AuthenticationMethod
		}
		nodeCfg.Cfg.SchemaRegistry.SchemaRegistryAPI = append(nodeCfg.Cfg.SchemaRegistry.SchemaRegistryAPI,
			config.NamedAuthNSocketAddress{
				Address: "0.0.0.0",
				Port:    sr.Port,
				Name:    sr.Name,
				AuthN:   authN,
			})
	}
	prepareSchemaRegistryTLS(nodeCfg, mountPoints)
	err = prepareSchemaRegistryClient(ctx, nodeCfg, clusterCfg, mountPoints)
	if err != nil {
		return nil, nil, fmt.Errorf("preparing schemaRegistry client: %w", err)
	}

	if err := configuration.SetValue(clusterCfg, "enable_rack_awareness", true); err != nil {
		return nil, nil, fmt.Errorf("setting enable_rack_awareness: %w", err)
	}

	// Set additional flat legacy properties
	for k, v := range pandaCluster.Spec.AdditionalConfiguration {
		if err := configuration.SetAdditionalFlatProperty(nodeCfg, clusterCfg, k, v); err != nil {
			return nil, nil, fmt.Errorf("adding additional flat property %q: %w", k, err)
		}
	}

	// Fold in any last overriding attributes. Prefer the new ClusterConfiguration attribute.
	for k, v := range pandaCluster.Spec.ClusterConfiguration {
		clusterCfg.Set(k, *(v.DeepCopy()))
	}

	return nodeCfg, clusterCfg, nil
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

func prepareCloudStorage(
	ctx context.Context,
	n *configuration.nodeCfg, c *configuration.clusterCfg, pandaCluster *vectorizedv1alpha1.Cluster,
) error {
	if pandaCluster.Spec.CloudStorage.AccessKey != "" {
		_ = configuration.SetValue(c, "cloud_storage_access_key", pandaCluster.Spec.CloudStorage.AccessKey)
	}
	if pandaCluster.Spec.CloudStorage.SecretKeyRef.Name != "" {
		if pandaCluster.Namespace != pandaCluster.Spec.CloudStorage.SecretKeyRef.Namespace {
			return fmt.Errorf("Spec.CloudStorage.SecretKeyRef.Namespace must match %q", pandaCluster.Namespace)
		}
		c.Set("cloud_storage_secret_key", vectorizedv1alpha1.ClusterConfigValue{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: pandaCluster.Spec.CloudStorage.SecretKeyRef.Name,
				},
				Key: pandaCluster.Spec.CloudStorage.SecretKeyRef.Name,
			},
		})
	}

	if pandaCluster.Spec.CloudStorage.CredentialsSource != "" {
		_ = configuration.SetValue(c, "cloud_storage_credentials_source", string(pandaCluster.Spec.CloudStorage.CredentialsSource))
	}

	_ = configuration.SetValue(c, "cloud_storage_enabled", pandaCluster.Spec.CloudStorage.Enabled)
	_ = configuration.SetValue(c, "cloud_storage_region", pandaCluster.Spec.CloudStorage.Region)
	_ = configuration.SetValue(c, "cloud_storage_bucket", pandaCluster.Spec.CloudStorage.Bucket)
	_ = configuration.SetValue(c, "cloud_storage_disable_tls", pandaCluster.Spec.CloudStorage.DisableTLS)

	interval := pandaCluster.Spec.CloudStorage.ReconcilicationIntervalMs
	if interval != 0 {
		_ = configuration.SetValue(c, "cloud_storage_reconciliation_interval_ms", interval)
	}
	maxCon := pandaCluster.Spec.CloudStorage.MaxConnections
	if maxCon != 0 {
		_ = configuration.SetValue(c, "cloud_storage_max_connections", maxCon)
	}
	apiEndpoint := pandaCluster.Spec.CloudStorage.APIEndpoint
	if apiEndpoint != "" {
		_ = configuration.SetValue(c, "cloud_storage_api_endpoint", apiEndpoint)
	}
	endpointPort := pandaCluster.Spec.CloudStorage.APIEndpointPort
	if endpointPort != 0 {
		_ = configuration.SetValue(c, "cloud_storage_api_endpoint_port", endpointPort)
	}
	trustfile := pandaCluster.Spec.CloudStorage.Trustfile
	if trustfile != "" {
		_ = configuration.SetValue(c, "cloud_storage_trust_file", trustfile)
	}

	n.Cfg.Redpanda.CloudStorageCacheDirectory = archivalCacheIndexDirectory

	// Only set cache_size (in bytes) if no percentage based cluster property is set.
	// This avoids problems with multiple node pools: bytes-based is very different if disk size changes (move between different tiers in cloud, for example).
	// Percentage based is kept stable mostly (even exactly the same most of the time, 15% typically), and will not cause immediate churn on the old nodePool if disk size is going down.
	_, cloudStoragePercentageConfigured := pandaCluster.Spec.AdditionalConfiguration["redpanda.cloud_storage_cache_size_percent"]

	if pandaCluster.Spec.CloudStorage.CacheStorage != nil && pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value() > 0 && !cloudStoragePercentageConfigured {
		//size := strconv.FormatInt(r.pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value(), 10)
		_ = configuration.SetValue(c, "cloud_storage_cache_size", pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value())
	}

	return c.Error()
}

func preparePandaproxy(cfgRpk *config.RedpandaYaml, pandaCluster *vectorizedv1alpha1.Cluster) {
	internal := pandaCluster.PandaproxyAPIInternal()
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

	for _, external := range pandaCluster.PandaproxyAPIExternalListeners() {
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

func preparePandaproxyClient(
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
