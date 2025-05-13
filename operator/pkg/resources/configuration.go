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
	"fmt"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/nodepools"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
)

// CreateConfiguration creates a combined config.
// This code is specific to the v1 CRD, but it uses a shared capability for managing node and cluster configuration.
// Extracted from configmap.go.
// TODO: CombinedCfg supports the registration of volume mounts, etc; at the moment this code makes only very
// limited use of that facility. We can lift the volume management, etc, from pki and so on into the configuration
// construction to have it all in one spot.
func CreateConfiguration(
	ctx context.Context,
	client k8sclient.Client,
	cloudExpander *pkgsecrets.CloudExpander,
	pandaCluster *vectorizedv1alpha1.Cluster,
	serviceFQDN string,
	pandaproxySASLUser types.NamespacedName,
	schemaRegistrySASLUser types.NamespacedName,
	tlsConfigProvider resourcetypes.BrokerTLSConfigProvider,
) (*clusterconfiguration.CombinedCfg, error) {
	cfg := clusterconfiguration.NewConfig(pandaCluster.Namespace, client, cloudExpander)

	mountPoints := resourcetypes.GetTLSMountPoints()

	addresses, err := generateSeedServer(ctx, pandaCluster, serviceFQDN, client)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	cfg.Node.Rpk.KafkaAPI.Brokers = addresses
	if tls := tlsConfigProvider.KafkaClientBrokerTLS(mountPoints); tls != nil {
		cfg.Node.Rpk.KafkaAPI.TLS = &config.TLS{
			KeyFile:            tls.KeyFile,
			CertFile:           tls.CertFile,
			TruststoreFile:     tls.TruststoreFile,
			InsecureSkipVerify: false,
		}
	}

	cfg.Node.Rpk.AdminAPI.Addresses = addresses
	if tls := tlsConfigProvider.AdminAPIClientTLS(mountPoints); tls != nil {
		cfg.Node.Rpk.AdminAPI.TLS = tls
	}

	cfg.Node.Rpk.SR.Addresses = addresses
	if tls := tlsConfigProvider.SchemaRegistryClientTLS(mountPoints); tls != nil {
		cfg.Node.Rpk.SR.TLS = tls
	}

	c := pandaCluster.Spec.Configuration
	cr := &cfg.Node.Redpanda

	internalListener := pandaCluster.InternalListener()
	var internalAuthN *string
	var internalPort int
	if internalListener != nil {
		if internalListener.AuthenticationMethod != "" {
			internalAuthN = &internalListener.AuthenticationMethod
		}
		internalPort = internalListener.Port
	}
	cr.KafkaAPI = []config.NamedAuthNSocketAddress{} // we don't want to inherit default kafka port
	cr.KafkaAPI = append(cr.KafkaAPI, config.NamedAuthNSocketAddress{
		Address: "0.0.0.0",
		Port:    internalPort,
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
		if err := prepareCloudStorage(cfg, pandaCluster); err != nil {
			return nil, fmt.Errorf("preparing cloud storage: %w", err)
		}
	}

	for _, user := range pandaCluster.Spec.Superusers {
		if err := clusterconfiguration.AppendValue(cfg.Cluster, superusersConfigurationKey, user.Username); err != nil {
			return nil, fmt.Errorf("appending superusers property: %w", err)
		}
	}

	if pandaCluster.Spec.EnableSASL {
		if err := cfg.Cluster.SetValue("enable_sasl", true); err != nil {
			return nil, fmt.Errorf("setting enable_sasl: %w", err)
		}
	}
	if pandaCluster.Spec.KafkaEnableAuthorization != nil && *pandaCluster.Spec.KafkaEnableAuthorization {
		if err := cfg.Cluster.SetValue("kafka_enable_authorization", true); err != nil {
			return nil, fmt.Errorf("setting kafka_enable_authorization: %w", err)
		}
	}

	partitions := pandaCluster.Spec.Configuration.GroupTopicPartitions
	if partitions != 0 {
		if err := cfg.Cluster.SetValue("group_topic_partitions", partitions); err != nil {
			return nil, fmt.Errorf("setting group_topic_partitions: %w", err)
		}
	}

	if err := cfg.Cluster.SetValue("auto_create_topics_enabled", pandaCluster.Spec.Configuration.AutoCreateTopics); err != nil {
		return nil, fmt.Errorf("setting auto_create_topics_enabled: %w", err)
	}

	intervalSec := 60 * 30 // 60s * 30 = 30 minutes
	if err := cfg.Cluster.SetValue("cloud_storage_segment_max_upload_interval_sec", intervalSec); err != nil {
		return nil, fmt.Errorf("setting cloud_storage_segment_max_upload_interval_sec: %w", err)
	}

	if err := cfg.Cluster.SetValue("log_segment_size", logSegmentSize); err != nil {
		return nil, fmt.Errorf("setting log_segment_size: %w", err)
	}

	if err := prepareSeedServerList(ctx, cfg, serviceFQDN, pandaCluster, client); err != nil {
		return nil, fmt.Errorf("preparing seed server list: %w", err)
	}
	preparePandaproxy(cfg, pandaCluster)
	preparePandaproxyTLS(cfg, pandaCluster, mountPoints)
	err = preparePandaproxyClient(cfg, serviceFQDN, tlsConfigProvider, pandaCluster, pandaproxySASLUser, mountPoints)
	if err != nil {
		return nil, fmt.Errorf("preparing proxy client: %w", err)
	}

	for _, sr := range pandaCluster.SchemaRegistryListeners() {
		var authN *string
		if sr.AuthenticationMethod != "" {
			authN = &sr.AuthenticationMethod
		}
		cfg.Node.SchemaRegistry.SchemaRegistryAPI = append(cfg.Node.SchemaRegistry.SchemaRegistryAPI,
			config.NamedAuthNSocketAddress{
				Address: "0.0.0.0",
				Port:    sr.Port,
				Name:    sr.Name,
				AuthN:   authN,
			})
	}
	prepareSchemaRegistryTLS(cfg, pandaCluster, mountPoints)
	err = prepareSchemaRegistryClient(cfg, serviceFQDN, tlsConfigProvider, pandaCluster, schemaRegistrySASLUser, mountPoints)
	if err != nil {
		return nil, fmt.Errorf("preparing schemaRegistry client: %w", err)
	}

	if err := cfg.Cluster.SetValue("enable_rack_awareness", true); err != nil {
		return nil, fmt.Errorf("setting enable_rack_awareness: %w", err)
	}

	// Set additional flat legacy properties
	for k, v := range pandaCluster.Spec.AdditionalConfiguration {
		if err := cfg.SetAdditionalFlatProperty(k, v); err != nil {
			return nil, fmt.Errorf("adding additional flat property %q: %w", k, err)
		}
	}

	// Fold in any last overriding attributes. Prefer the new ClusterConfiguration attribute.
	for k, v := range pandaCluster.Spec.ClusterConfiguration {
		cfg.Cluster.Set(k, *(v.DeepCopy()))
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

func prepareCloudStorage(
	cfg *clusterconfiguration.CombinedCfg, pandaCluster *vectorizedv1alpha1.Cluster,
) error {
	if pandaCluster.Spec.CloudStorage.AccessKey != "" {
		_ = cfg.Cluster.SetValue("cloud_storage_access_key", pandaCluster.Spec.CloudStorage.AccessKey)
	}
	if pandaCluster.Spec.CloudStorage.SecretKeyRef.Name != "" {
		if pandaCluster.Namespace != pandaCluster.Spec.CloudStorage.SecretKeyRef.Namespace {
			return fmt.Errorf("Spec.CloudStorage.SecretKeyRef.Namespace must match %q", pandaCluster.Namespace)
		}
		cfg.Cluster.Set("cloud_storage_secret_key", vectorizedv1alpha1.ClusterConfigValue{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: pandaCluster.Spec.CloudStorage.SecretKeyRef.Name,
				},
				Key: pandaCluster.Spec.CloudStorage.SecretKeyRef.Name,
			},
		})
	}

	if pandaCluster.Spec.CloudStorage.CredentialsSource != "" {
		_ = cfg.Cluster.SetValue("cloud_storage_credentials_source", string(pandaCluster.Spec.CloudStorage.CredentialsSource))
	}

	_ = cfg.Cluster.SetValue("cloud_storage_enabled", pandaCluster.Spec.CloudStorage.Enabled)
	_ = cfg.Cluster.SetValue("cloud_storage_region", pandaCluster.Spec.CloudStorage.Region)
	_ = cfg.Cluster.SetValue("cloud_storage_bucket", pandaCluster.Spec.CloudStorage.Bucket)
	_ = cfg.Cluster.SetValue("cloud_storage_disable_tls", pandaCluster.Spec.CloudStorage.DisableTLS)

	interval := pandaCluster.Spec.CloudStorage.ReconcilicationIntervalMs
	if interval != 0 {
		_ = cfg.Cluster.SetValue("cloud_storage_reconciliation_interval_ms", interval)
	}
	maxCon := pandaCluster.Spec.CloudStorage.MaxConnections
	if maxCon != 0 {
		_ = cfg.Cluster.SetValue("cloud_storage_max_connections", maxCon)
	}
	apiEndpoint := pandaCluster.Spec.CloudStorage.APIEndpoint
	if apiEndpoint != "" {
		_ = cfg.Cluster.SetValue("cloud_storage_api_endpoint", apiEndpoint)
	}
	endpointPort := pandaCluster.Spec.CloudStorage.APIEndpointPort
	if endpointPort != 0 {
		_ = cfg.Cluster.SetValue("cloud_storage_api_endpoint_port", endpointPort)
	}
	trustfile := pandaCluster.Spec.CloudStorage.Trustfile
	if trustfile != "" {
		_ = cfg.Cluster.SetValue("cloud_storage_trust_file", trustfile)
	}

	cfg.Node.Redpanda.CloudStorageCacheDirectory = archivalCacheIndexDirectory

	// Only set cache_size (in bytes) if no percentage based cluster property is set.
	// This avoids problems with multiple node pools: bytes-based is very different if disk size changes (move between different tiers in cloud, for example).
	// Percentage based is kept stable mostly (even exactly the same most of the time, 15% typically), and will not cause immediate churn on the old nodePool if disk size is going down.
	_, cloudStoragePercentageConfigured := pandaCluster.Spec.AdditionalConfiguration["redpanda.cloud_storage_cache_size_percent"]

	if pandaCluster.Spec.CloudStorage.CacheStorage != nil && pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value() > 0 && !cloudStoragePercentageConfigured {
		_ = cfg.Cluster.SetValue("cloud_storage_cache_size", pandaCluster.Spec.CloudStorage.CacheStorage.Capacity.Value())
	}

	return cfg.Error()
}

func preparePandaproxy(cfg *clusterconfiguration.CombinedCfg, pandaCluster *vectorizedv1alpha1.Cluster) {
	internal := pandaCluster.PandaproxyAPIInternal()
	if internal == nil {
		return
	}

	var internalAuthN *string
	if internal.AuthenticationMethod != "" {
		internalAuthN = &internal.AuthenticationMethod
	}
	cfg.Node.Pandaproxy.PandaproxyAPI = []config.NamedAuthNSocketAddress{
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
		cfg.Node.Pandaproxy.PandaproxyAPI = append(cfg.Node.Pandaproxy.PandaproxyAPI,
			config.NamedAuthNSocketAddress{
				Address: "0.0.0.0",
				Port:    calculateExternalPort(internal.Port, external.Port),
				Name:    external.Name,
				AuthN:   externalAuthN,
			})
	}
}

func preparePandaproxyClient(
	cfg *clusterconfiguration.CombinedCfg,
	serviceFQDN string, tlsConfigProvider resourcetypes.BrokerTLSConfigProvider,
	pandaCluster *vectorizedv1alpha1.Cluster,
	pandaproxySASLUser types.NamespacedName,
	mountPoints *resourcetypes.TLSMountPoints,
) error {
	if internal := pandaCluster.PandaproxyAPIInternal(); internal == nil {
		return nil
	}
	kafkaInternal := pandaCluster.InternalListener()
	if kafkaInternal == nil {
		return errors.New("pandaproxy is missing internal kafka listener. This state is forbidden by the webhook") //nolint:goerr113 // no need for static error
	}

	// Point towards redpanda's headless service to utilize K8s native service
	// discovery features. This mitigates the need to update this section of
	// the config when the number of brokers change.
	// Alternatively, localhost could be used to the same end but using DNS
	// might save us in some cases where the local broker is having some
	// trouble and it makes the config more generally reusable.
	cfg.Node.PandaproxyClient = &config.KafkaClient{
		Brokers: []config.SocketAddress{
			{Address: serviceFQDN, Port: pandaCluster.InternalListener().Port},
		},
	}

	clientBrokerTLS := tlsConfigProvider.KafkaClientBrokerTLS(mountPoints)
	if clientBrokerTLS != nil {
		cfg.Node.PandaproxyClient.BrokerTLS = *clientBrokerTLS
	}

	if !pandaCluster.IsSASLOnInternalEnabled() {
		return nil
	}

	// Late binding of SASL SCRAM credentialsRetrieve SCRAM credentials
	cfg.Node.PandaproxyClient.SASLMechanism = ptr.To(saslMechanism)
	_ = cfg.EnsureInitEnv(corev1.EnvVar{
		Name: "PANDAPROXY_CLIENT_USERNAME",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: pandaproxySASLUser.Name,
				},
				Key: corev1.BasicAuthUsernameKey,
			},
		},
	})
	cfg.Node.AddFixup("pandaproxy_client.scram_username", fmt.Sprintf(`%s("PANDAPROXY_CLIENT_USERNAME")`, clusterconfiguration.CELEnvString))
	cfg.Cluster.AddFixup(superusersConfigurationKey, fmt.Sprintf(`%s(it, %s("PANDAPROXY_CLIENT_USERNAME"))`, clusterconfiguration.CELAppendYamlStringArray, clusterconfiguration.CELEnvString))
	_ = cfg.EnsureInitEnv(corev1.EnvVar{
		Name: "PANDAPROXY_CLIENT_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: pandaproxySASLUser.Name,
				},
				Key: corev1.BasicAuthPasswordKey,
			},
		},
	})
	cfg.Node.AddFixup("pandaproxy_client.scram_password", fmt.Sprintf(`%s("PANDAPROXY_CLIENT_PASSWORD")`, clusterconfiguration.CELEnvString))

	return cfg.Error()
}

func prepareSchemaRegistryClient(
	cfg *clusterconfiguration.CombinedCfg,
	serviceFQDN string,
	tlsConfigProvider resourcetypes.BrokerTLSConfigProvider,
	pandaCluster *vectorizedv1alpha1.Cluster,
	schemaRegistrySASLUser types.NamespacedName,
	mountPoints *resourcetypes.TLSMountPoints,
) error {
	if pandaCluster.Spec.Configuration.SchemaRegistry == nil {
		return nil
	}
	kafkaInternal := pandaCluster.InternalListener()
	if kafkaInternal == nil {
		return errors.New("schema registry is missing internal kafka listener. This state is forbidden by the webhook") //nolint:goerr113 // no need for static error
	}

	// Point towards redpanda's headless service to utilize K8s native service
	// discovery features. This mitigates the need to update this section of
	// the config when the number of brokers change.
	// Alternatively, localhost could be used to the same end but using DNS
	// might save us in some cases where the local broker is having some
	// trouble and it makes the config more generally reusable.
	cfg.Node.SchemaRegistryClient = &config.KafkaClient{
		Brokers: []config.SocketAddress{
			{Address: serviceFQDN, Port: pandaCluster.InternalListener().Port},
		},
	}

	clientBrokerTLS := tlsConfigProvider.KafkaClientBrokerTLS(mountPoints)
	if clientBrokerTLS != nil {
		cfg.Node.SchemaRegistryClient.BrokerTLS = *clientBrokerTLS
	}

	if !pandaCluster.IsSASLOnInternalEnabled() {
		return nil
	}

	// Late binding of SASL SCRAM credentialsRetrieve SCRAM credentials
	cfg.Node.SchemaRegistryClient.SASLMechanism = ptr.To(saslMechanism)
	_ = cfg.EnsureInitEnv(corev1.EnvVar{
		Name: "SCHEMA_REGISTRY_CLIENT_USERNAME",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: schemaRegistrySASLUser.Name,
				},
				Key: corev1.BasicAuthUsernameKey,
			},
		},
	})
	cfg.Node.AddFixup("schema_registry_client.scram_username", fmt.Sprintf(`%s("SCHEMA_REGISTRY_CLIENT_USERNAME")`, clusterconfiguration.CELEnvString))
	cfg.Cluster.AddFixup(superusersConfigurationKey, fmt.Sprintf(`%s(it, %s("SCHEMA_REGISTRY_CLIENT_USERNAME"))`, clusterconfiguration.CELAppendYamlStringArray, clusterconfiguration.CELEnvString))

	_ = cfg.EnsureInitEnv(corev1.EnvVar{
		Name: "SCHEMA_REGISTRY_CLIENT_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: schemaRegistrySASLUser.Name,
				},
				Key: corev1.BasicAuthPasswordKey,
			},
		},
	})
	cfg.Node.AddFixup("schema_registry_client.scram_password", fmt.Sprintf(`%s("SCHEMA_REGISTRY_CLIENT_PASSWORD")`, clusterconfiguration.CELEnvString))

	return cfg.Error()
}

func preparePandaproxyTLS(
	cfg *clusterconfiguration.CombinedCfg,
	pandaCluster *vectorizedv1alpha1.Cluster,
	mountPoints *resourcetypes.TLSMountPoints,
) {
	cfg.Node.Pandaproxy.PandaproxyAPITLS = []config.ServerTLS{}
	for _, tlsListener := range pandaCluster.PandaproxyAPITLSs() {
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
		cfg.Node.Pandaproxy.PandaproxyAPITLS = append(cfg.Node.Pandaproxy.PandaproxyAPITLS, tls)
	}
}

func prepareSchemaRegistryTLS(
	cfg *clusterconfiguration.CombinedCfg,
	pandaCluster *vectorizedv1alpha1.Cluster,
	mountPoints *resourcetypes.TLSMountPoints,
) {
	cfg.Node.SchemaRegistry.SchemaRegistryAPITLS = []config.ServerTLS{}
	for _, sr := range pandaCluster.SchemaRegistryListeners() {
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
		cfg.Node.SchemaRegistry.SchemaRegistryAPITLS = append(cfg.Node.SchemaRegistry.SchemaRegistryAPITLS, tls)
	}
}

func clusterCRPortOrRPKDefault(clusterPort, defaultPort int) int {
	if clusterPort == 0 {
		return defaultPort
	}

	return clusterPort
}

func generateSeedServer(
	ctx context.Context,
	pandaCluster *vectorizedv1alpha1.Cluster,
	serviceFQDN string,
	reader k8sclient.Reader,
) ([]string, error) {
	var addresses []string

	for i := int32(0); i < ptr.Deref(pandaCluster.Spec.Replicas, 0); i++ {
		addresses = append(addresses, fmt.Sprintf("%s-%d.%s", pandaCluster.Name, i, serviceFQDN))
	}

	nps, err := nodepools.GetNodePools(ctx, pandaCluster, reader)
	if err != nil {
		return []string{}, err
	}

	for _, np := range nps {
		if np.Name == vectorizedv1alpha1.DefaultNodePoolName {
			continue
		}
		prefix := fmt.Sprintf("%s-%s", pandaCluster.Name, np.Name)
		for i := int32(0); i < ptr.Deref(np.Replicas, 0); i++ {
			addresses = append(addresses, fmt.Sprintf("%s-%d.%s", prefix, i, serviceFQDN))
		}
	}

	return addresses, nil
}

// prepareSeedServerList - supports only > 22.3
func prepareSeedServerList(
	ctx context.Context,
	cfg *clusterconfiguration.CombinedCfg,
	serviceFQDN string,
	pandaCluster *vectorizedv1alpha1.Cluster,
	reader k8sclient.Reader,
) error {
	addresses, err := generateSeedServer(ctx, pandaCluster, serviceFQDN, reader)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, address := range addresses {
		cfg.Node.Redpanda.SeedServers = append(cfg.Node.Redpanda.SeedServers, config.SeedServer{
			Host: config.SocketAddress{
				// Example address: cluster-sample-0.cluster-sample.default.svc.cluster.local
				Address: address,
				Port:    clusterCRPortOrRPKDefault(pandaCluster.Spec.Configuration.RPCServer.Port, cfg.Node.Redpanda.RPCServer.Port),
			},
		})
	}

	// We require >= 22.3 so we can configure empty_seed_starts_clusters.
	cfg.Node.Redpanda.EmptySeedStartsCluster = new(bool) // default to false

	if len(cfg.Node.Redpanda.SeedServers) == 0 {
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
