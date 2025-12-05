// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
)

// RedpandaClusterConfiguration represents where values for configuration can be pulled from.
// Properties may be specified directly in properties, which will always
// take precedence, however values will initially get pulled from any exteernal secrets
// Kubernetes secrets, or config maps, if specified, before finally merging in the inline
// values. The order of merging goes from most sensitive initially to least sensitive,
// so we pull and merge in this order: externalSecretRef -> secretKeyRef -> configMapKeyRef -> properties
//
// +structType=atomic
// +kubebuilder:validation:XValidation:message="at least one of properties, configMapKeyRef, secretKeyRef, or externalSecretRef must be set",rule="has(self.properties) || has(self.configMapKeyRef) || has(self.secretKeyRef) || has(self.externalSecretRef)"
type RedpandaClusterConfiguration struct {
	// The typed property values we will merge into the configuration.
	Properties *ConfigurationProperties `json:"properties,omitempty"`
	// If the value is supplied by a kubernetes object reference, coordinates are embedded here.
	// For target values, the string value fetched from the source will be treated as
	// a raw configuration file.
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Should the value be contained in a k8s secret rather than configmap, we can refer
	// to it here.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// If the value is supplied by an external source, coordinates are embedded here.
	// Note: like configMapKeyRef, and secretKeyRef, fetched values are considered raw configuration
	// files.
	ExternalSecretRefSelector *ExternalSecretKeySelector `json:"externalSecretRef,omitempty"`
}

// +kubebuilder:validation:Enum=reject;permit
type AuditFailurePolicy string

const (
	AuditFailurePolicyReject AuditFailurePolicy = "reject"
	AuditFailurePolicyPermit AuditFailurePolicy = "permit"
)

// +kubebuilder:validation:Enum=aws;google_s3_compat;azure;minio;oracle_s3_compat;linode_s3_compat;unknown
type CloudStorageBackend string

const (
	CloudStorageBackendAws            CloudStorageBackend = "aws"
	CloudStorageBackendGoogleS3Compat CloudStorageBackend = "google_s3_compat"
	CloudStorageBackendAzure          CloudStorageBackend = "azure"
	CloudStorageBackendMinio          CloudStorageBackend = "minio"
	CloudStorageBackendOracleS3Compat CloudStorageBackend = "oracle_s3_compat"
	CloudStorageBackendLinodeS3Compat CloudStorageBackend = "linode_s3_compat"
	CloudStorageBackendUnknown        CloudStorageBackend = "unknown"
)

// +kubebuilder:validation:Enum=config_file;aws_instance_metadata;sts;gcp_instance_metadata;azure_aks_oidc_federation;azure_vm_instance_metadata
type CloudStorageCredentialsSource string

const (
	CloudStorageCredentialsSourceConfigFile              CloudStorageCredentialsSource = "config_file"
	CloudStorageCredentialsSourceAwsInstanceMetadata     CloudStorageCredentialsSource = "aws_instance_metadata"
	CloudStorageCredentialsSourceSts                     CloudStorageCredentialsSource = "sts"
	CloudStorageCredentialsSourceGcpInstanceMetadata     CloudStorageCredentialsSource = "gcp_instance_metadata"
	CloudStorageCredentialsSourceAzureAksOidcFederation  CloudStorageCredentialsSource = "azure_aks_oidc_federation"
	CloudStorageCredentialsSourceAzureVmInstanceMetadata CloudStorageCredentialsSource = "azure_vm_instance_metadata"
)

// +kubebuilder:validation:Enum=virtual_host;path
type CloudStorageUrlStyle string

const (
	CloudStorageUrlStyleVirtualHost CloudStorageUrlStyle = "virtual_host"
	CloudStorageUrlStylePath        CloudStorageUrlStyle = "path"
)

// +kubebuilder:validation:Enum=none;redpanda;compat
type EnableSchemaIdValidation string

const (
	EnableSchemaIdValidationNone     EnableSchemaIdValidation = "none"
	EnableSchemaIdValidationRedpanda EnableSchemaIdValidation = "redpanda"
	EnableSchemaIdValidationCompat   EnableSchemaIdValidation = "compat"
)

// +kubebuilder:validation:Enum=rest;object_storage
type IcebergCatalogType string

const (
	IcebergCatalogTypeRest          IcebergCatalogType = "rest"
	IcebergCatalogTypeObjectStorage IcebergCatalogType = "object_storage"
)

// +kubebuilder:validation:Enum=drop;dlq_table
type IcebergInvalidRecordAction string

const (
	IcebergInvalidRecordActionDrop     IcebergInvalidRecordAction = "drop"
	IcebergInvalidRecordActionDlqTable IcebergInvalidRecordAction = "dlq_table"
)

// +kubebuilder:validation:Enum=none;bearer;oauth2;aws_sigv4;gcp
type IcebergRestCatalogAuthenticationMode string

const (
	IcebergRestCatalogAuthenticationModeNone     IcebergRestCatalogAuthenticationMode = "none"
	IcebergRestCatalogAuthenticationModeBearer   IcebergRestCatalogAuthenticationMode = "bearer"
	IcebergRestCatalogAuthenticationModeOauth2   IcebergRestCatalogAuthenticationMode = "oauth2"
	IcebergRestCatalogAuthenticationModeAwsSigv4 IcebergRestCatalogAuthenticationMode = "aws_sigv4"
	IcebergRestCatalogAuthenticationModeGcp      IcebergRestCatalogAuthenticationMode = "gcp"
)

// +kubebuilder:validation:Enum=config_file;aws_instance_metadata;sts;gcp_instance_metadata;azure_aks_oidc_federation;azure_vm_instance_metadata
type IcebergRestCatalogCredentialsSource string

const (
	IcebergRestCatalogCredentialsSourceConfigFile              IcebergRestCatalogCredentialsSource = "config_file"
	IcebergRestCatalogCredentialsSourceAwsInstanceMetadata     IcebergRestCatalogCredentialsSource = "aws_instance_metadata"
	IcebergRestCatalogCredentialsSourceSts                     IcebergRestCatalogCredentialsSource = "sts"
	IcebergRestCatalogCredentialsSourceGcpInstanceMetadata     IcebergRestCatalogCredentialsSource = "gcp_instance_metadata"
	IcebergRestCatalogCredentialsSourceAzureAksOidcFederation  IcebergRestCatalogCredentialsSource = "azure_aks_oidc_federation"
	IcebergRestCatalogCredentialsSourceAzureVmInstanceMetadata IcebergRestCatalogCredentialsSource = "azure_vm_instance_metadata"
)

// +kubebuilder:validation:Enum=none;gzip;snappy;lz4;zstd;producer
type LogCompressionType string

const (
	LogCompressionTypeNone     LogCompressionType = "none"
	LogCompressionTypeGzip     LogCompressionType = "gzip"
	LogCompressionTypeSnappy   LogCompressionType = "snappy"
	LogCompressionTypeLz4      LogCompressionType = "lz4"
	LogCompressionTypeZstd     LogCompressionType = "zstd"
	LogCompressionTypeProducer LogCompressionType = "producer"
)

// +kubebuilder:validation:Enum=CreateTime;LogAppendTime
type LogMessageTimestampType string

const (
	LogMessageTimestampTypeCreateTime    LogMessageTimestampType = "CreateTime"
	LogMessageTimestampTypeLogAppendTime LogMessageTimestampType = "LogAppendTime"
)

// +kubebuilder:validation:Enum=off;node_add;continuous
type PartitionAutobalancingMode string

const (
	PartitionAutobalancingModeOff        PartitionAutobalancingMode = "off"
	PartitionAutobalancingModeNodeAdd    PartitionAutobalancingMode = "node_add"
	PartitionAutobalancingModeContinuous PartitionAutobalancingMode = "continuous"
)

// +kubebuilder:validation:Enum=legacy;rfc2253
type TlsCertificateNameFormat string

const (
	TlsCertificateNameFormatLegacy  TlsCertificateNameFormat = "legacy"
	TlsCertificateNameFormatRfc2253 TlsCertificateNameFormat = "rfc2253"
)

// +kubebuilder:validation:Enum=v1.0;v1.1;v1.2;v1.3
type TlsMinVersion string

const (
	TlsMinVersionV10 TlsMinVersion = "v1.0"
	TlsMinVersionV11 TlsMinVersion = "v1.1"
	TlsMinVersionV12 TlsMinVersion = "v1.2"
	TlsMinVersionV13 TlsMinVersion = "v1.3"
)

// +kubebuilder:validation:Enum=true;false;disabled
type WriteCachingDefault string

const (
	WriteCachingDefaultTrue     WriteCachingDefault = "true"
	WriteCachingDefaultFalse    WriteCachingDefault = "false"
	WriteCachingDefaultDisabled WriteCachingDefault = "disabled"
)

type ConfigurationProperties struct {
	// Whether Admin API clients must provide HTTP basic authentication headers.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	AdminApiRequireAuth *bool `json:"adminApiRequireAuth,omitempty" property:"admin_api_require_auth"`

	// Enable aggregation of metrics returned by the `/metrics` endpoint.
	// Aggregation can simplify monitoring by providing summarized data instead of
	// raw, per-instance metrics. Metric aggregation is performed by summing the
	// values of samples by labels and is done when it makes sense by the shard
	// and/or partition labels.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	AggregateMetrics *bool `json:"aggregateMetrics,omitempty" property:"aggregate_metrics"`

	// Defines the number of bytes allocated by the internal audit client for audit
	// messages. When changing this, you must disable audit logging and then
	// re-enable it for the change to take effect. Consider increasing this if your
	// system generates a very large number of audit records in a short amount of
	// time.
	//
	// requires_restart: false
	//
	// +optional
	AuditClientMaxBufferSize *int64 `json:"auditClientMaxBufferSize,omitempty" property:"audit_client_max_buffer_size"`

	// Enables or disables audit logging. When you set this to true, Redpanda checks
	// for an existing topic named `_redpanda.audit_log`. If none is found, Redpanda
	// automatically creates one for you.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	AuditEnabled *bool `json:"auditEnabled,omitempty" property:"audit_enabled"`

	// List of strings in JSON style identifying the event types to include in the
	// audit log. This may include any of the following: `management, produce,
	// consume, describe, heartbeat, authenticate, schema_registry, admin`.
	//
	// requires_restart: false
	// example: ["management", "describe"]
	//
	// +optional
	AuditEnabledEventTypes *[]string `json:"auditEnabledEventTypes,omitempty" property:"audit_enabled_event_types"`

	// List of user principals to exclude from auditing.
	//
	// requires_restart: false
	// example: ["User:principal1","User:principal2"]
	//
	// +optional
	AuditExcludedPrincipals *[]string `json:"auditExcludedPrincipals,omitempty" property:"audit_excluded_principals"`

	// List of topics to exclude from auditing.
	//
	// requires_restart: false
	// example: ["topic1","topic2"]
	//
	// +optional
	AuditExcludedTopics *[]string `json:"auditExcludedTopics,omitempty" property:"audit_excluded_topics"`

	// Defines the policy for rejecting audit log messages when the audit log queue
	// is full. If set to 'permit', then new audit messages are dropped and the
	// operation is permitted.  If set to 'reject', then the operation is rejected.
	//
	// requires_restart: false
	// allowed_values: [reject, permit]
	//
	// +optional
	AuditFailurePolicy *AuditFailurePolicy `json:"auditFailurePolicy,omitempty" property:"audit_failure_policy"`

	// Defines the number of partitions used by a newly-created audit topic. This
	// configuration applies only to the audit log topic and may be different from
	// the cluster or other topic configurations. This cannot be altered for
	// existing audit log topics.
	//
	// requires_restart: false
	//
	// +optional
	AuditLogNumPartitions *int64 `json:"auditLogNumPartitions,omitempty" property:"audit_log_num_partitions"`

	// Defines the replication factor for a newly-created audit log topic. This
	// configuration applies only to the audit log topic and may be different from
	// the cluster or other topic configurations. This cannot be altered for
	// existing audit log topics. Setting this value is optional. If a value is not
	// provided, Redpanda will use the value specified for
	// `internal_topic_replication_factor`.
	//
	// requires_restart: false
	//
	// +optional
	AuditLogReplicationFactor *int64 `json:"auditLogReplicationFactor,omitempty" property:"audit_log_replication_factor"`

	// Allow automatic topic creation. To prevent excess topics, this property is
	// not supported on Redpanda Cloud BYOC and Dedicated clusters. You should
	// explicitly manage topic creation for these Redpanda Cloud clusters. If you
	// produce to a topic that doesn't exist, the topic will be created with
	// defaults if this property is enabled.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	AutoCreateTopicsEnabled *bool `json:"autoCreateTopicsEnabled,omitempty" property:"auto_create_topics_enabled"`

	// AWS or GCP access key. This access key is part of the credentials that
	// Redpanda requires to authenticate with object storage services for Tiered
	// Storage. This access key is used with the <<cloud_storage_secret_key>> to
	// form the complete credentials required for authentication. To authenticate
	// using IAM roles, see cloud_storage_credentials_source.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageAccessKey *string `json:"cloudStorageAccessKey,omitempty" property:"cloud_storage_access_key"`

	// Optional API endpoint. - AWS: When blank, this is automatically generated
	// using <<cloud_storage_region,region>> and <<cloud_storage_bucket,bucket>>.
	// Otherwise, this uses the value assigned. - GCP: Uses
	// `storage.googleapis.com`.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageApiEndpoint *string `json:"cloudStorageApiEndpoint,omitempty" property:"cloud_storage_api_endpoint"`

	// TLS port override.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageApiEndpointPort *int64 `json:"cloudStorageApiEndpointPort,omitempty" property:"cloud_storage_api_endpoint_port"`

	// Azure Data Lake Storage v2 endpoint override. Use when hierarchical
	// namespaces are enabled on your storage account and you have set up a custom
	// endpoint.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageAzureAdlsEndpoint *string `json:"cloudStorageAzureAdlsEndpoint,omitempty" property:"cloud_storage_azure_adls_endpoint"`

	// Azure Data Lake Storage v2 port override. See also
	// `cloud_storage_azure_adls_endpoint`. Use when Hierarchical Namespaces are
	// enabled on your storage account and you have set up a custom endpoint.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageAzureAdlsPort *int64 `json:"cloudStorageAzureAdlsPort,omitempty" property:"cloud_storage_azure_adls_port"`

	// The name of the Azure container to use with Tiered Storage. If `null`, the
	// property is disabled. The container must belong to
	// cloud_storage_azure_storage_account.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageAzureContainer *string `json:"cloudStorageAzureContainer,omitempty" property:"cloud_storage_azure_container"`

	// The managed identity ID to use for access to the Azure storage account. To
	// use Azure managed identities, you must set `cloud_storage_credentials_source`
	// to `azure_vm_instance_metadata`.
	//
	// requires_restart: false
	//
	// +optional
	CloudStorageAzureManagedIdentityId *string `json:"cloudStorageAzureManagedIdentityId,omitempty" property:"cloud_storage_azure_managed_identity_id"`

	// The shared key to be used for Azure Shared Key authentication with the Azure
	// storage account configured by `cloud_storage_azure_storage_account`.  If
	// `null`, the property is disabled. Redpanda expects this key string to be
	// Base64 encoded.
	//
	// requires_restart: false
	//
	// +optional
	CloudStorageAzureSharedKey *string `json:"cloudStorageAzureSharedKey,omitempty" property:"cloud_storage_azure_shared_key"`

	// The name of the Azure storage account to use with Tiered Storage. If `null`,
	// the property is disabled.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageAzureStorageAccount *string `json:"cloudStorageAzureStorageAccount,omitempty" property:"cloud_storage_azure_storage_account"`

	// Optional object storage backend variant used to select API capabilities. If
	// not supplied, this will be inferred from other configuration properties.
	//
	// requires_restart: true
	// allowed_values: [aws, google_s3_compat, azure, minio, oracle_s3_compat, linode_s3_compat, unknown]
	// example: aws
	//
	// +optional
	CloudStorageBackend *CloudStorageBackend `json:"cloudStorageBackend,omitempty" property:"cloud_storage_backend"`

	// AWS or GCP bucket or container that should be used to store data.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageBucket *string `json:"cloudStorageBucket,omitempty" property:"cloud_storage_bucket"`

	// Maximum size of object storage cache. If both this property and
	// cloud_storage_cache_size_percent are set, Redpanda uses the minimum of the
	// two.
	//
	// requires_restart: false
	//
	// +optional
	CloudStorageCacheSize *int64 `json:"cloudStorageCacheSize,omitempty" property:"cloud_storage_cache_size"`

	// Maximum size of the cloud cache as a percentage of unreserved disk space
	// disk_reservation_percent. The default value for this option is tuned for a
	// shared disk configuration. Consider increasing the value if using a dedicated
	// cache disk. The property
	// <<cloud_storage_cache_size,`cloud_storage_cache_size`>> controls the same
	// limit expressed as a fixed number of bytes. If both
	// `cloud_storage_cache_size` and `cloud_storage_cache_size_percent` are set,
	// Redpanda uses the minimum of the two.
	//
	// requires_restart: false
	// example: 20.0
	//
	// +optional
	CloudStorageCacheSizePercent *float64 `json:"cloudStorageCacheSizePercent,omitempty" property:"cloud_storage_cache_size_percent"`

	// Optional unique name to disambiguate this cluster's metadata in object
	// storage (e.g. for Whole Cluster Restore) when multiple clusters share a
	// bucket. Must be unique within the bucket, 1-64 chars, [A-Za-z0-9_-]. Do not
	// change once set.
	//
	// requires_restart: false
	//
	// +optional
	CloudStorageClusterName *string `json:"cloudStorageClusterName,omitempty" property:"cloud_storage_cluster_name"`

	// The source of credentials used to authenticate to object storage services.
	// Required for cluster provider authentication with IAM roles. To authenticate
	// using access keys, see cloud_storage_access_key`. Accepted values:
	// `config_file`, `aws_instance_metadata`, `sts, gcp_instance_metadata`,
	// `azure_vm_instance_metadata`, `azure_aks_oidc_federation`
	//
	// requires_restart: true
	// allowed_values: [config_file, aws_instance_metadata, sts, gcp_instance_metadata, azure_aks_oidc_federation, azure_vm_instance_metadata]
	// example: config_file
	//
	// +optional
	CloudStorageCredentialsSource *CloudStorageCredentialsSource `json:"cloudStorageCredentialsSource,omitempty" property:"cloud_storage_credentials_source"`

	// Path to certificate revocation list for `cloud_storage_trust_file`.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageCrlFile *string `json:"cloudStorageCrlFile,omitempty" property:"cloud_storage_crl_file"`

	// Use legacy upload mode and do not start archiver_manager.
	//
	// requires_restart: true
	// example: false
	//
	// +optional
	CloudStorageDisableArchiverManager *bool `json:"cloudStorageDisableArchiverManager,omitempty" property:"cloud_storage_disable_archiver_manager"`

	// Disable TLS for all object storage connections.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	CloudStorageDisableTls *bool `json:"cloudStorageDisableTls,omitempty" property:"cloud_storage_disable_tls"`

	// Enable object storage. Must be set to `true` to use Tiered Storage or Remote
	// Read Replicas.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	CloudStorageEnabled *bool `json:"cloudStorageEnabled,omitempty" property:"cloud_storage_enabled"`

	// Maximum simultaneous object storage connections per shard, applicable to
	// upload and download activities.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageMaxConnections *int64 `json:"cloudStorageMaxConnections,omitempty" property:"cloud_storage_max_connections"`

	// Cloud provider region that houses the bucket or container used for storage.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageRegion *string `json:"cloudStorageRegion,omitempty" property:"cloud_storage_region"`

	// Cloud provider secret key.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageSecretKey *string `json:"cloudStorageSecretKey,omitempty" property:"cloud_storage_secret_key"`

	// Path to certificate that should be used to validate server certificate during
	// TLS handshake.
	//
	// requires_restart: true
	//
	// +optional
	CloudStorageTrustFile *string `json:"cloudStorageTrustFile,omitempty" property:"cloud_storage_trust_file"`

	// Specifies the addressing style to use for Amazon S3 requests. This
	// configuration determines how S3 bucket URLs are formatted. You can choose
	// between: `virtual_host`, (for example, `<bucket-name>.s3.amazonaws.com`),
	// `path`, (for example, `s3.amazonaws.com/<bucket-name>`), and `null`. Path
	// style is supported for backward compatibility with legacy systems. When this
	// property is not set or is `null`, the client tries to use `virtual_host`
	// addressing. If the initial request fails, the client automatically tries the
	// `path` style. If neither addressing style works, Redpanda terminates the
	// startup, requiring manual configuration to proceed.
	//
	// requires_restart: true
	// allowed_values: [virtual_host, path]
	// example: virtual_host
	//
	// +optional
	CloudStorageUrlStyle *CloudStorageUrlStyle `json:"cloudStorageUrlStyle,omitempty" property:"cloud_storage_url_style"`

	// The size limit for the object size in cloud topics. When the amount of data
	// on a shard reaches this limit, an upload is triggered.
	//
	// requires_restart: false
	//
	// +optional
	CloudTopicsProduceBatchingSizeThreshold *int64 `json:"cloudTopicsProduceBatchingSizeThreshold,omitempty" property:"cloud_topics_produce_batching_size_threshold"`

	// Threshold for the object cardinality in cloud topics. When the number of
	// partitions in waiting for the upload reach this limit, an upload is
	// triggered.
	//
	// requires_restart: false
	//
	// +optional
	CloudTopicsProduceCardinalityThreshold *int64 `json:"cloudTopicsProduceCardinalityThreshold,omitempty" property:"cloud_topics_produce_cardinality_threshold"`

	// Time interval after which the upload is triggered.
	//
	// requires_restart: false
	//
	// +optional
	CloudTopicsProduceUploadInterval *int64 `json:"cloudTopicsProduceUploadInterval,omitempty" property:"cloud_topics_produce_upload_interval"`

	// Cluster identifier.
	//
	// requires_restart: false
	//
	// +optional
	ClusterId *string `json:"clusterId,omitempty" property:"cluster_id"`

	// If set to `true`, move partitions between cores in runtime to maintain
	// balanced partition distribution.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	CoreBalancingContinuous *bool `json:"coreBalancingContinuous,omitempty" property:"core_balancing_continuous"`

	// If set to `true`, and if after a restart the number of cores changes,
	// Redpanda will move partitions between cores to maintain balanced partition
	// distribution.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	CoreBalancingOnCoreCountChange *bool `json:"coreBalancingOnCoreCountChange,omitempty" property:"core_balancing_on_core_count_change"`

	// Enables CPU profiling for Redpanda.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	CpuProfilerEnabled *bool `json:"cpuProfilerEnabled,omitempty" property:"cpu_profiler_enabled"`

	// The sample period for the CPU profiler.
	//
	// requires_restart: false
	// example: 100
	//
	// +optional
	CpuProfilerSamplePeriodMs *int64 `json:"cpuProfilerSamplePeriodMs,omitempty" property:"cpu_profiler_sample_period_ms"`

	// Enables WebAssembly-powered data transforms directly in the broker. When
	// `data_transforms_enabled` is set to `true`, Redpanda reserves memory for data
	// transforms, even if no transform functions are currently deployed. This
	// memory reservation ensures that adequate resources are available for
	// transform functions when they are needed, but it also means that some memory
	// is allocated regardless of usage.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	DataTransformsEnabled *bool `json:"dataTransformsEnabled,omitempty" property:"data_transforms_enabled"`

	// The amount of memory to reserve per core for data transform (Wasm) virtual
	// machines. Memory is reserved on boot. The maximum number of functions that
	// can be deployed to a cluster is equal to
	// `data_transforms_per_core_memory_reservation` /
	// `data_transforms_per_function_memory_limit`.
	//
	// requires_restart: true
	// example: 26214400
	// aliases: [wasm_per_core_memory_reservation]
	//
	// +optional
	DataTransformsPerCoreMemoryReservation *int64 `json:"dataTransformsPerCoreMemoryReservation,omitempty" property:"data_transforms_per_core_memory_reservation"`

	// The amount of memory to give an instance of a data transform (Wasm) virtual
	// machine. The maximum number of functions that can be deployed to a cluster is
	// equal to `data_transforms_per_core_memory_reservation` /
	// `data_transforms_per_function_memory_limit`.
	//
	// requires_restart: true
	// example: 5242880
	// aliases: [wasm_per_function_memory_limit]
	//
	// +optional
	DataTransformsPerFunctionMemoryLimit *int64 `json:"dataTransformsPerFunctionMemoryLimit,omitempty" property:"data_transforms_per_function_memory_limit"`

	// Option to explicitly disable enforcement of datalake disk space usage
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	DatalakeDiskSpaceMonitorEnable *bool `json:"datalakeDiskSpaceMonitorEnable,omitempty" property:"datalake_disk_space_monitor_enable"`

	// Size of the scratch space datalake soft limit expressed as a percentage of
	// the datalake_scratch_space_size_bytes configuration value.
	//
	// requires_restart: false
	// example: 80.0
	//
	// +optional
	DatalakeScratchSpaceSoftLimitSizePercent *float64 `json:"datalakeScratchSpaceSoftLimitSizePercent,omitempty" property:"datalake_scratch_space_soft_limit_size_percent"`

	// If set, how long debug bundles are kept in the debug bundle storage directory
	// after they are created. If not set, debug bundles are kept indefinitely.
	//
	// requires_restart: false
	//
	// +optional
	DebugBundleAutoRemovalSeconds *int64 `json:"debugBundleAutoRemovalSeconds,omitempty" property:"debug_bundle_auto_removal_seconds"`

	// Path to the debug bundle storage directory. Note: Changing this path does not
	// clean up existing debug bundles. If not set, the debug bundle is stored in
	// the Redpanda data directory specified in the redpanda.yaml broker
	// configuration file.
	//
	// requires_restart: false
	//
	// +optional
	DebugBundleStorageDir *string `json:"debugBundleStorageDir,omitempty" property:"debug_bundle_storage_dir"`

	// Default settings for preferred location of topic partition leaders. It can be
	// either "none" (no preference), or "racks:<rack1>,<rack2>,..." (prefer brokers
	// with rack id from the list).
	//
	// requires_restart: false
	//
	// +optional
	DefaultLeadersPreference *LeadersPreference `json:"defaultLeadersPreference,omitempty" property:"default_leaders_preference"`

	// Default number of partitions per topic.
	//
	// requires_restart: false
	//
	// +optional
	DefaultTopicPartitions *int64 `json:"defaultTopicPartitions,omitempty" property:"default_topic_partitions"`

	// Default replication factor for new topics.
	//
	// requires_restart: false
	// example: 1
	//
	// +optional
	DefaultTopicReplications *int64 `json:"defaultTopicReplications,omitempty" property:"default_topic_replications"`

	// Disable registering the metrics exposed on the internal `/metrics` endpoint.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	DisableMetrics *bool `json:"disableMetrics,omitempty" property:"disable_metrics"`

	// Disable registering the metrics exposed on the `/public_metrics` endpoint.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	DisablePublicMetrics *bool `json:"disablePublicMetrics,omitempty" property:"disable_public_metrics"`

	// List of enabled consumer group metrics. Accepted Values: `group`,
	// `partition`, `consumer_lag`
	//
	// requires_restart: false
	//
	// +optional
	EnableConsumerGroupMetrics *[]string `json:"enableConsumerGroupMetrics,omitempty" property:"enable_consumer_group_metrics"`

	// Limits the write rate for the controller log.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	EnableControllerLogRateLimiting *bool `json:"enableControllerLogRateLimiting,omitempty" property:"enable_controller_log_rate_limiting"`

	// Enable idempotent producers.
	//
	// requires_restart: true
	// example: false
	//
	// +optional
	EnableIdempotence *bool `json:"enableIdempotence,omitempty" property:"enable_idempotence"`

	// Enable automatic leadership rebalancing.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	EnableLeaderBalancer *bool `json:"enableLeaderBalancer,omitempty" property:"enable_leader_balancer"`

	// Enable the cluster metrics reporter. If `true`, the metrics reporter collects
	// and exports to Redpanda Data a set of customer usage metrics at the interval
	// set by `metrics_reporter_report_interval`. The cluster metrics of the metrics
	// reporter are different from the monitoring metrics. * The metrics reporter
	// exports customer usage metrics for consumption by Redpanda Data.* Monitoring
	// metrics are exported for consumption by Redpanda users.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	EnableMetricsReporter *bool `json:"enableMetricsReporter,omitempty" property:"enable_metrics_reporter"`

	// Enable rack-aware replica assignment.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	EnableRackAwareness *bool `json:"enableRackAwareness,omitempty" property:"enable_rack_awareness"`

	// Enable SASL authentication for Kafka connections. Authorization is required
	// to modify this property. See also `kafka_enable_authorization`.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	EnableSasl *bool `json:"enableSasl,omitempty" property:"enable_sasl"`

	// Mode to enable server-side schema ID validation. Accepted Values: * `none`:
	// Schema validation is disabled (no schema ID checks are done). Associated
	// topic properties cannot be modified. * `redpanda`: Schema validation is
	// enabled. Only Redpanda topic properties are accepted. * `compat`: Schema
	// validation is enabled. Both Redpanda and compatible topic properties are
	// accepted.
	//
	// requires_restart: false
	// allowed_values: [none, redpanda, compat]
	//
	// +optional
	EnableSchemaIdValidation *EnableSchemaIdValidation `json:"enableSchemaIdValidation,omitempty" property:"enable_schema_id_validation"`

	// Enable creating Shadow Links from this cluster to a remote source cluster for
	// data replication.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	EnableShadowLinking *bool `json:"enableShadowLinking,omitempty" property:"enable_shadow_linking"`

	// Enable transactions (atomic writes).
	//
	// requires_restart: true
	// example: false
	//
	// +optional
	EnableTransactions *bool `json:"enableTransactions,omitempty" property:"enable_transactions"`

	// Enables the usage tracking mechanism, storing windowed history of
	// kafka/cloud_storage metrics over time.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	EnableUsage *bool `json:"enableUsage,omitempty" property:"enable_usage"`

	// Maximum number of bytes returned in a fetch request.
	//
	// requires_restart: false
	//
	// +optional
	FetchMaxBytes *int64 `json:"fetchMaxBytes,omitempty" property:"fetch_max_bytes"`

	// The maximum allowed session timeout for registered consumers. Longer timeouts
	// give consumers more time to process messages in between heartbeats at the
	// cost of a longer time to detect failures.
	//
	// requires_restart: false
	//
	// +optional
	GroupMaxSessionTimeoutMs *int64 `json:"groupMaxSessionTimeoutMs,omitempty" property:"group_max_session_timeout_ms"`

	// The minimum allowed session timeout for registered consumers. Shorter
	// timeouts result in quicker failure detection at the cost of more frequent
	// consumer heartbeating, which can overwhelm broker resources.
	//
	// requires_restart: false
	//
	// +optional
	GroupMinSessionTimeoutMs *int64 `json:"groupMinSessionTimeoutMs,omitempty" property:"group_min_session_timeout_ms"`

	// A list of supported HTTP authentication mechanisms. Accepted Values: `BASIC`,
	// `OIDC`
	//
	// requires_restart: false
	//
	// +optional
	HttpAuthentication *[]string `json:"httpAuthentication,omitempty" property:"http_authentication"`

	// Base path for the cloud-storage-object-backed Iceberg filesystem catalog.
	// After Iceberg is enabled, do not change this value.
	//
	// requires_restart: true
	//
	// +optional
	IcebergCatalogBaseLocation *string `json:"icebergCatalogBaseLocation,omitempty" property:"iceberg_catalog_base_location"`

	// Iceberg catalog type that Redpanda will use to commit table metadata updates.
	// Supported types: 'rest', 'object_storage'
	//
	// requires_restart: true
	// allowed_values: [rest, object_storage]
	//
	// +optional
	IcebergCatalogType *IcebergCatalogType `json:"icebergCatalogType,omitempty" property:"iceberg_catalog_type"`

	// Default value for the redpanda.iceberg.partition.spec topic property that
	// determines the partition spec for the Iceberg table corresponding to the
	// topic. If this property is not set and AWS Glue is being used as the Iceberg
	// REST catalog, the default value will be overridden by an empty partition
	// spec, for compatibility with AWS Glue.
	//
	// requires_restart: false
	//
	// +optional
	IcebergDefaultPartitionSpec *string `json:"icebergDefaultPartitionSpec,omitempty" property:"iceberg_default_partition_spec"`

	// Default value for the redpanda.iceberg.delete topic property that determines
	// if the corresponding Iceberg table is deleted upon deleting the topic.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	IcebergDelete *bool `json:"icebergDelete,omitempty" property:"iceberg_delete"`

	// Whether to disable automatic Iceberg snapshot expiry. This may be useful if
	// the Iceberg catalog expects to perform snapshot expiry on its own.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	IcebergDisableAutomaticSnapshotExpiry *bool `json:"icebergDisableAutomaticSnapshotExpiry,omitempty" property:"iceberg_disable_automatic_snapshot_expiry"`

	// Whether to disable tagging of Iceberg snapshots. These tags are used to
	// ensure that the snapshots that Redpanda writes are retained during snapshot
	// removal, which in turn, helps Redpanda ensure exactly once delivery of
	// records. Disabling tags is therefore not recommended, but may be useful if
	// the Iceberg catalog does not support tags.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	IcebergDisableSnapshotTagging *bool `json:"icebergDisableSnapshotTagging,omitempty" property:"iceberg_disable_snapshot_tagging"`

	// Suffix appended to the Iceberg table name for the dead-letter queue (DLQ)
	// table that stores invalid records when the invalid record action is set to
	// `dlq_table`. Should be chosen in a way that avoids name collisions with other
	// Iceberg tables. Important for catalogs which do not support ~ (tilde) in
	// table names. Should not be changed after any DLQ tables have been created.
	//
	// requires_restart: false
	//
	// +optional
	IcebergDlqTableSuffix *string `json:"icebergDlqTableSuffix,omitempty" property:"iceberg_dlq_table_suffix"`

	// Enables the translation of topic data into Iceberg tables. Setting
	// iceberg_enabled to true activates the feature at the cluster level, but each
	// topic must also set the redpanda.iceberg.enabled topic-level property to true
	// to use it. If iceberg_enabled is set to false, the feature is disabled for
	// all topics in the cluster, overriding any topic-level settings.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	IcebergEnabled *bool `json:"icebergEnabled,omitempty" property:"iceberg_enabled"`

	// Default value for the redpanda.iceberg.invalid.record.action topic property.
	//
	// requires_restart: false
	// allowed_values: [drop, dlq_table]
	//
	// +optional
	IcebergInvalidRecordAction *IcebergInvalidRecordAction `json:"icebergInvalidRecordAction,omitempty" property:"iceberg_invalid_record_action"`

	// The authentication mode for client requests made to the Iceberg catalog.
	// Choose from: `none`, `bearer`, `oauth2`, and `aws_sigv4`. In `bearer` mode,
	// the token specified in `iceberg_rest_catalog_token` is used unconditonally,
	// and no attempts are made to refresh the token. In `oauth2` mode, the
	// credentials specified in `iceberg_rest_catalog_client_id` and
	// `iceberg_rest_catalog_client_secret` are used to obtain a bearer token from
	// the URI defined by `iceberg_rest_catalog_oauth2_server_uri`. In `aws_sigv4`
	// mode, the same AWS credentials used for cloud storage (see
	// `cloud_storage_region`, `cloud_storage_access_key`,
	// `cloud_storage_secret_key`, and `cloud_storage_credentials_source`) are used
	// to sign requests to AWS Glue catalog with SigV4.In `gcp` mode Redpanda will
	// use VM metadata for authentication.
	//
	// requires_restart: true
	// allowed_values: [none, bearer, oauth2, aws_sigv4, gcp]
	// example: none
	//
	// +optional
	IcebergRestCatalogAuthenticationMode *IcebergRestCatalogAuthenticationMode `json:"icebergRestCatalogAuthenticationMode,omitempty" property:"iceberg_rest_catalog_authentication_mode"`

	// AWS access key for Iceberg REST catalog SigV4 authentication. If not set,
	// falls back to cloud_storage_access_key when using aws_sigv4 authentication
	// mode.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogAwsAccessKey *string `json:"icebergRestCatalogAwsAccessKey,omitempty" property:"iceberg_rest_catalog_aws_access_key"`

	// AWS region for Iceberg REST catalog SigV4 authentication. If not set, falls
	// back to cloud_storage_region when using aws_sigv4 authentication mode.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogAwsRegion *string `json:"icebergRestCatalogAwsRegion,omitempty" property:"iceberg_rest_catalog_aws_region"`

	// AWS secret key for Iceberg REST catalog SigV4 authentication. If not set,
	// falls back to cloud_storage_secret_key when using aws_sigv4 authentication
	// mode.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogAwsSecretKey *string `json:"icebergRestCatalogAwsSecretKey,omitempty" property:"iceberg_rest_catalog_aws_secret_key"`

	// AWS service name for SigV4 signing when using aws_sigv4 authentication mode.
	// Defaults to 'glue' for AWS Glue Data Catalog. Can be changed to support other
	// AWS services that provide Iceberg REST catalog APIs.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogAwsServiceName *string `json:"icebergRestCatalogAwsServiceName,omitempty" property:"iceberg_rest_catalog_aws_service_name"`

	// Base URI for the Iceberg REST catalog. If unset, the REST catalog server
	// determines the location. Some REST catalogs, like AWS Glue, require the
	// client to set this. After Iceberg is enabled, do not change this value.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogBaseLocation *string `json:"icebergRestCatalogBaseLocation,omitempty" property:"iceberg_rest_catalog_base_location"`

	// Iceberg REST catalog user ID. This ID is used to query the catalog API for
	// the OAuth token. Required if catalog type is set to `rest` and
	// `iceberg_rest_catalog_authentication_mode` is set to `oauth2`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogClientId *string `json:"icebergRestCatalogClientId,omitempty" property:"iceberg_rest_catalog_client_id"`

	// Secret to authenticate against Iceberg REST catalog. Required if catalog type
	// is set to `rest` and `iceberg_rest_catalog_authentication_mode` is set to
	// `oauth2`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogClientSecret *string `json:"icebergRestCatalogClientSecret,omitempty" property:"iceberg_rest_catalog_client_secret"`

	// Source of AWS credentials for Iceberg REST catalog SigV4 authentication. If
	// not set, falls back to cloud_storage_credentials_source when using aws_sigv4
	// authentication mode. Accepted values: config_file, aws_instance_metadata,
	// sts, gcp_instance_metadata, azure_vm_instance_metadata,
	// azure_aks_oidc_federation.
	//
	// requires_restart: true
	// allowed_values: [config_file, aws_instance_metadata, sts, gcp_instance_metadata, azure_aks_oidc_federation, azure_vm_instance_metadata]
	// example: config_file
	// aliases: [iceberg_rest_catalog_aws_credentials_source]
	//
	// +optional
	IcebergRestCatalogCredentialsSource *IcebergRestCatalogCredentialsSource `json:"icebergRestCatalogCredentialsSource,omitempty" property:"iceberg_rest_catalog_credentials_source"`

	// The contents of a certificate revocation list for
	// `iceberg_rest_catalog_trust`. Takes precedence over
	// `iceberg_rest_catalog_crl_file`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogCrl *string `json:"icebergRestCatalogCrl,omitempty" property:"iceberg_rest_catalog_crl"`

	// Path to certificate revocation list for `iceberg_rest_catalog_trust_file`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogCrlFile *string `json:"icebergRestCatalogCrlFile,omitempty" property:"iceberg_rest_catalog_crl_file"`

	// URL of Iceberg REST catalog endpoint
	//
	// requires_restart: true
	// example: http://hostname:8181
	//
	// +optional
	IcebergRestCatalogEndpoint *string `json:"icebergRestCatalogEndpoint,omitempty" property:"iceberg_rest_catalog_endpoint"`

	// The GCP project that is billed for charges associated with Iceberg REST
	// Catalog requests.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogGcpUserProject *string `json:"icebergRestCatalogGcpUserProject,omitempty" property:"iceberg_rest_catalog_gcp_user_project"`

	// The OAuth scope used to retrieve access tokens for Iceberg catalog
	// authentication. Only meaningful when
	// `iceberg_rest_catalog_authentication_mode` is set to `oauth2`
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogOauth2Scope *string `json:"icebergRestCatalogOauth2Scope,omitempty" property:"iceberg_rest_catalog_oauth2_scope"`

	// The OAuth URI used to retrieve access tokens for Iceberg catalog
	// authentication. If left undefined, the deprecated Iceberg catalog endpoint
	// `/v1/oauth/tokens` is used instead.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogOauth2ServerUri *string `json:"icebergRestCatalogOauth2ServerUri,omitempty" property:"iceberg_rest_catalog_oauth2_server_uri"`

	// Token used to access the REST Iceberg catalog. Required if
	// `iceberg_rest_catalog_authentication_mode` is set to `bearer`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogToken *string `json:"icebergRestCatalogToken,omitempty" property:"iceberg_rest_catalog_token"`

	// The contents of a certificate chain to trust for the REST Iceberg catalog.
	// Takes precedence over `iceberg_rest_catalog_trust_file`.
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogTrust *string `json:"icebergRestCatalogTrust,omitempty" property:"iceberg_rest_catalog_trust"`

	// Path to a file containing a certificate chain to trust for the REST Iceberg
	// catalog
	//
	// requires_restart: true
	//
	// +optional
	IcebergRestCatalogTrustFile *string `json:"icebergRestCatalogTrustFile,omitempty" property:"iceberg_rest_catalog_trust_file"`

	// Warehouse to use for the Iceberg REST catalog. Redpanda will query the
	// catalog for configurations specific to the warehouse, for example, using it
	// to automatically configure the appropriate prefix.
	//
	// requires_restart: true
	// aliases: [iceberg_rest_catalog_prefix]
	//
	// +optional
	IcebergRestCatalogWarehouse *string `json:"icebergRestCatalogWarehouse,omitempty" property:"iceberg_rest_catalog_warehouse"`

	// Default value for the redpanda.iceberg.target.lag.ms topic property, which
	// controls how often data in an Iceberg table is refreshed with new data from
	// the corresponding Redpanda topic. Redpanda attempts to commit all the data
	// produced to the topic within the lag target in a best effort fashion, subject
	// to resource availability.
	//
	// requires_restart: false
	// example: 4611686023427
	//
	// +optional
	IcebergTargetLagMs *int64 `json:"icebergTargetLagMs,omitempty" property:"iceberg_target_lag_ms"`

	// Optional replacement string for dots in topic names when deriving Iceberg
	// table names, useful when downstream systems do not permit dots in table
	// names. The replacement string cannot contain dots. Be careful to avoid table
	// name collisions caused by the replacement.If an Iceberg topic with dots in
	// the name exists in the cluster, the value of this property should not be
	// changed.
	//
	// requires_restart: false
	//
	// +optional
	IcebergTopicNameDotReplacement *string `json:"icebergTopicNameDotReplacement,omitempty" property:"iceberg_topic_name_dot_replacement"`

	// Initial local retention size target for partitions of topics with Tiered
	// Storage enabled. If no initial local target retention is configured all
	// locally retained data will be delivered to learner when joining partition
	// replica set.
	//
	// requires_restart: false
	//
	// +optional
	InitialRetentionLocalTargetBytesDefault *int64 `json:"initialRetentionLocalTargetBytesDefault,omitempty" property:"initial_retention_local_target_bytes_default"`

	// Initial local retention time target for partitions of topics with Tiered
	// Storage enabled. If no initial local target retention is configured all
	// locally retained data will be delivered to learner when joining partition
	// replica set.
	//
	// requires_restart: false
	//
	// +optional
	InitialRetentionLocalTargetMsDefault *int64 `json:"initialRetentionLocalTargetMsDefault,omitempty" property:"initial_retention_local_target_ms_default"`

	// Target replication factor for internal topics.
	//
	// requires_restart: true
	//
	// +optional
	InternalTopicReplicationFactor *int64 `json:"internalTopicReplicationFactor,omitempty" property:"internal_topic_replication_factor"`

	// Maximum connections per second for one core. If `null` (the default), then
	// the number of connections per second is unlimited.
	//
	// requires_restart: false
	//
	// +optional
	KafkaConnectionRateLimit *int64 `json:"kafkaConnectionRateLimit,omitempty" property:"kafka_connection_rate_limit"`

	// Overrides the maximum connections per second for one core for the specified
	// IP addresses (for example, `['127.0.0.1:90', '50.20.1.1:40']`)
	//
	// requires_restart: false
	// example: ['127.0.0.1:90', '50.20.1.1:40']
	//
	// +optional
	KafkaConnectionRateLimitOverrides *[]string `json:"kafkaConnectionRateLimitOverrides,omitempty" property:"kafka_connection_rate_limit_overrides"`

	// Maximum number of Kafka client connections per broker. If `null`, the
	// property is disabled.
	//
	// requires_restart: false
	//
	// +optional
	KafkaConnectionsMax *int64 `json:"kafkaConnectionsMax,omitempty" property:"kafka_connections_max"`

	// A list of IP addresses for which Kafka client connection limits are
	// overridden and don't apply. For example, `(['127.0.0.1:90',
	// '50.20.1.1:40']).`
	//
	// requires_restart: false
	// example: ['127.0.0.1:90', '50.20.1.1:40']
	//
	// +optional
	KafkaConnectionsMaxOverrides *[]string `json:"kafkaConnectionsMaxOverrides,omitempty" property:"kafka_connections_max_overrides"`

	// Maximum number of Kafka client connections per IP address, per broker. If
	// `null`, the property is disabled.
	//
	// requires_restart: false
	//
	// +optional
	KafkaConnectionsMaxPerIp *int64 `json:"kafkaConnectionsMaxPerIp,omitempty" property:"kafka_connections_max_per_ip"`

	// Flag to require authorization for Kafka connections. If `null`, the property
	// is disabled, and authorization is instead enabled by enable_sasl. * `null`:
	// Ignored. Authorization is enabled with `enable_sasl`: `true` * `true`:
	// authorization is required. * `false`: authorization is disabled.
	//
	// requires_restart: false
	//
	// +optional
	KafkaEnableAuthorization *bool `json:"kafkaEnableAuthorization,omitempty" property:"kafka_enable_authorization"`

	// Whether to include Tiered Storage as a special remote:// directory in
	// `DescribeLogDirs Kafka` API requests.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	KafkaEnableDescribeLogDirsRemoteStorage *bool `json:"kafkaEnableDescribeLogDirsRemoteStorage,omitempty" property:"kafka_enable_describe_log_dirs_remote_storage"`

	// Enable the Kafka partition reassignment API.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	KafkaEnablePartitionReassignment *bool `json:"kafkaEnablePartitionReassignment,omitempty" property:"kafka_enable_partition_reassignment"`

	// Kafka group recovery timeout.
	//
	// requires_restart: false
	//
	// +optional
	KafkaGroupRecoveryTimeoutMs *int64 `json:"kafkaGroupRecoveryTimeoutMs,omitempty" property:"kafka_group_recovery_timeout_ms"`

	// The share of Kafka subsystem memory that can be used for fetch read buffers,
	// as a fraction of the Kafka subsystem memory amount.
	//
	// requires_restart: true
	// example: 0.5
	//
	// +optional
	KafkaMemoryShareForFetch *float64 `json:"kafkaMemoryShareForFetch,omitempty" property:"kafka_memory_share_for_fetch"`

	// Principal mapping rules for mTLS authentication on the Kafka API. If `null`,
	// the property is disabled.
	//
	// requires_restart: false
	//
	// +optional
	KafkaMtlsPrincipalMappingRules *[]string `json:"kafkaMtlsPrincipalMappingRules,omitempty" property:"kafka_mtls_principal_mapping_rules"`

	// A list of topics that are protected from deletion and configuration changes
	// by Kafka clients. Set by default to a list of Redpanda internal topics.
	//
	// requires_restart: false
	//
	// +optional
	KafkaNodeleteTopics *[]string `json:"kafkaNodeleteTopics,omitempty" property:"kafka_nodelete_topics"`

	// A list of topics that are protected from being produced to by Kafka clients.
	// Set by default to a list of Redpanda internal topics.
	//
	// requires_restart: false
	//
	// +optional
	KafkaNoproduceTopics *[]string `json:"kafkaNoproduceTopics,omitempty" property:"kafka_noproduce_topics"`

	// Enable kafka queue depth control.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	KafkaQdcEnable *bool `json:"kafkaQdcEnable,omitempty" property:"kafka_qdc_enable"`

	// Maximum latency threshold for Kafka queue depth control depth tracking.
	//
	// requires_restart: true
	//
	// +optional
	KafkaQdcMaxLatencyMs *int64 `json:"kafkaQdcMaxLatencyMs,omitempty" property:"kafka_qdc_max_latency_ms"`

	// Size of the Kafka server TCP receive buffer. If `null`, the property is
	// disabled.
	//
	// requires_restart: true
	// example: 65536
	//
	// +optional
	KafkaRpcServerTcpRecvBuf *int64 `json:"kafkaRpcServerTcpRecvBuf,omitempty" property:"kafka_rpc_server_tcp_recv_buf"`

	// Size of the Kafka server TCP transmit buffer. If `null`, the property is
	// disabled.
	//
	// requires_restart: true
	// example: 65536
	//
	// +optional
	KafkaRpcServerTcpSendBuf *int64 `json:"kafkaRpcServerTcpSendBuf,omitempty" property:"kafka_rpc_server_tcp_send_buf"`

	// The maximum time between Kafka client reauthentications. If a client has not
	// reauthenticated a connection within this time frame, that connection is torn
	// down. If this property is not set (or set to `null`), session expiry is
	// disabled, and a connection could live long after the client's credentials are
	// expired or revoked.
	//
	// requires_restart: false
	// example: 1000
	//
	// +optional
	KafkaSaslMaxReauthMs *int64 `json:"kafkaSaslMaxReauthMs,omitempty" property:"kafka_sasl_max_reauth_ms"`

	// List of throughput control groups that define exclusions from node-wide
	// throughput limits. Clients excluded from node-wide throughput limits are
	// still potentially subject to client-specific throughput limits. For more
	// information see
	// https://docs.redpanda.com/current/reference/properties/cluster-properties/#kafka_throughput_control.
	//
	// requires_restart: false
	// example: [{'name': 'first_group','client_id': 'client1'}, {'client_id': 'consumer-\d+'}, {'name': 'catch all'}]
	//
	// +optional
	KafkaThroughputControl *[]ThroughputControlGroup `json:"kafkaThroughputControl,omitempty" property:"kafka_throughput_control"`

	// List of Kafka API keys that are subject to cluster-wide and node-wide
	// throughput limit control.
	//
	// requires_restart: false
	//
	// +optional
	KafkaThroughputControlledApiKeys *[]string `json:"kafkaThroughputControlledApiKeys,omitempty" property:"kafka_throughput_controlled_api_keys"`

	// The maximum rate of all ingress Kafka API traffic for a node. Includes all
	// Kafka API traffic (requests, responses, headers, fetched data, produced data,
	// etc.). If `null`, the property is disabled, and traffic is not limited.
	//
	// requires_restart: false
	//
	// +optional
	KafkaThroughputLimitNodeInBps *int64 `json:"kafkaThroughputLimitNodeInBps,omitempty" property:"kafka_throughput_limit_node_in_bps"`

	// The maximum rate of all egress Kafka traffic for a node. Includes all Kafka
	// API traffic (requests, responses, headers, fetched data, produced data,
	// etc.). If `null`, the property is disabled, and traffic is not limited.
	//
	// requires_restart: false
	//
	// +optional
	KafkaThroughputLimitNodeOutBps *int64 `json:"kafkaThroughputLimitNodeOutBps,omitempty" property:"kafka_throughput_limit_node_out_bps"`

	// Maximum number of Kafka user topics that can be created. If `null`, then no
	// limit is enforced.
	//
	// requires_restart: false
	//
	// +optional
	KafkaTopicsMax *int64 `json:"kafkaTopicsMax,omitempty" property:"kafka_topics_max"`

	// Flag to enable a Redpanda cluster operator to use unsafe control characters
	// within strings, such as consumer group names or user names. This flag applies
	// only for Redpanda clusters that were originally on version 23.1 or earlier
	// and have been upgraded to version 23.2 or later. Starting in version 23.2,
	// newly-created Redpanda clusters ignore this property.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	LegacyPermitUnsafeLogOperation *bool `json:"legacyPermitUnsafeLogOperation,omitempty" property:"legacy_permit_unsafe_log_operation"`

	// Period at which to log a warning about using unsafe strings containing
	// control characters. If unsafe strings are permitted by
	// `legacy_permit_unsafe_log_operation`, a warning will be logged at an interval
	// specified by this property.
	//
	// requires_restart: false
	//
	// +optional
	LegacyUnsafeLogWarningIntervalSec *int64 `json:"legacyUnsafeLogWarningIntervalSec,omitempty" property:"legacy_unsafe_log_warning_interval_sec"`

	// Default cleanup policy for topic logs. The topic property `cleanup.policy`
	// overrides the value of `log_cleanup_policy` at the topic level.
	//
	// requires_restart: false
	// example: compact,delete
	//
	// +optional
	LogCleanupPolicy *string `json:"logCleanupPolicy,omitempty" property:"log_cleanup_policy"`

	// How often to trigger background compaction.
	//
	// requires_restart: false
	//
	// +optional
	LogCompactionIntervalMs *int64 `json:"logCompactionIntervalMs,omitempty" property:"log_compaction_interval_ms"`

	// Default topic compression type. The topic property `compression.type`
	// overrides the value of `log_compression_type` at the topic level.
	//
	// requires_restart: false
	// allowed_values: [none, gzip, snappy, lz4, zstd, producer]
	// example: snappy
	//
	// +optional
	LogCompressionType *LogCompressionType `json:"logCompressionType,omitempty" property:"log_compression_type"`

	// The maximum allowable timestamp difference between the broker's timestamp and
	// a record's timestamp. For topics with `message.timestamp.type` set to
	// `CreateTime`, Redpanda rejects records that have timestamps later than the
	// broker timestamp and exceed this difference. Redpanda ignores this property
	// for topics with `message.timestamp.type` set to `AppendTime`. The topic
	// property `message.timestamp.after.max.ms` overrides the value of
	// `log_message_timestamp_after_max_ms` at the topic level.
	//
	// requires_restart: false
	// example: 4611686018427
	//
	// +optional
	LogMessageTimestampAfterMaxMs *int64 `json:"logMessageTimestampAfterMaxMs,omitempty" property:"log_message_timestamp_after_max_ms"`

	// The maximum allowable timestamp difference between the broker's timestamp and
	// a record's timestamp. For topics with `message.timestamp.type` set to
	// `CreateTime`, Redpanda rejects records that have timestamps earlier than the
	// broker timestamp and exceed this difference. Redpanda ignores this property
	// for topics with `message.timestamp.type` set to `AppendTime`. The topic
	// property `message.timestamp.before.max.ms` overrides the value of
	// `log_message_timestamp_before_max_ms` at the topic level.
	//
	// requires_restart: false
	// example: 4611686018427
	//
	// +optional
	LogMessageTimestampBeforeMaxMs *int64 `json:"logMessageTimestampBeforeMaxMs,omitempty" property:"log_message_timestamp_before_max_ms"`

	// Default timestamp type for topic messages (CreateTime or LogAppendTime). The
	// topic property `message.timestamp.type` overrides the value of
	// `log_message_timestamp_type` at the topic level.
	//
	// requires_restart: false
	// allowed_values: [CreateTime, LogAppendTime]
	// example: LogAppendTime
	//
	// +optional
	LogMessageTimestampType *LogMessageTimestampType `json:"logMessageTimestampType,omitempty" property:"log_message_timestamp_type"`

	// The amount of time to keep a log file before deleting it (in milliseconds).
	// If set to `-1`, no time limit is applied. This is a cluster-wide default when
	// a topic does not set or disable `retention.ms`.
	//
	// requires_restart: false
	// aliases: [delete_retention_ms]
	//
	// +optional
	LogRetentionMs *int64 `json:"logRetentionMs,omitempty" property:"log_retention_ms"`

	// Default lifetime of log segments. If `null`, the property is disabled, and no
	// default lifetime is set. Any value under 60 seconds (60000 ms) is rejected.
	// This property can also be set in the Kafka API using the Kafka-compatible
	// alias, `log.roll.ms`. The topic property `segment.ms` overrides the value of
	// `log_segment_ms` at the topic level.
	//
	// requires_restart: false
	// example: 3600000
	//
	// +optional
	LogSegmentMs *int64 `json:"logSegmentMs,omitempty" property:"log_segment_ms"`

	// For a compacted topic, the maximum time a message remains ineligible for
	// compaction. The topic property `max.compaction.lag.ms` overrides this
	// property.
	//
	// requires_restart: false
	//
	// +optional
	MaxCompactionLagMs *int64 `json:"maxCompactionLagMs,omitempty" property:"max_compaction_lag_ms"`

	// The minimum ratio between the number of bytes in "dirty" segments and the
	// total number of bytes in closed segments that must be reached before a
	// partition's log is eligible for compaction in a compact topic. The topic
	// property `min.cleanable.dirty.ratio` overrides the value of
	// `min_cleanable_dirty_ratio` at the topic level.
	//
	// requires_restart: false
	// example: 0.2
	//
	// +optional
	MinCleanableDirtyRatio *float64 `json:"minCleanableDirtyRatio,omitempty" property:"min_cleanable_dirty_ratio"`

	// For a compacted topic, the minimum time a message remains uncompacted in the
	// log. The topic property `min.compaction.lag.ms` overrides this property.
	//
	// requires_restart: false
	//
	// +optional
	MinCompactionLagMs *int64 `json:"minCompactionLagMs,omitempty" property:"min_compaction_lag_ms"`

	// Minimum allowable replication factor for topics in this cluster. The set
	// value must be positive, odd, and equal to or less than the number of
	// available brokers. Changing this parameter only restricts newly-created
	// topics. Redpanda returns an `INVALID_REPLICATION_FACTOR` error on any attempt
	// to create a topic with a replication factor less than this property. If you
	// change the `minimum_topic_replications` setting, the replication factor of
	// existing topics remains unchanged. However, Redpanda will log a warning on
	// start-up with a list of any topics that have fewer replicas than this
	// minimum. For example, you might see a message such as `Topic X has a
	// replication factor less than specified minimum: 1 < 3`.
	//
	// requires_restart: false
	// example: 1
	//
	// +optional
	MinimumTopicReplications *int64 `json:"minimumTopicReplications,omitempty" property:"minimum_topic_replications"`

	// Maximum backoff (in milliseconds) to reconnect to an unresponsive peer during
	// node status liveness checks.
	//
	// requires_restart: false
	//
	// +optional
	NodeStatusReconnectMaxBackoffMs *int64 `json:"nodeStatusReconnectMaxBackoffMs,omitempty" property:"node_status_reconnect_max_backoff_ms"`

	// The amount of time (in seconds) to allow for when validating the expiry claim
	// in the token.
	//
	// requires_restart: false
	//
	// +optional
	OidcClockSkewTolerance *int64 `json:"oidcClockSkewTolerance,omitempty" property:"oidc_clock_skew_tolerance"`

	// The URL pointing to the well-known discovery endpoint for the OIDC provider.
	//
	// requires_restart: false
	//
	// +optional
	OidcDiscoveryUrl *string `json:"oidcDiscoveryUrl,omitempty" property:"oidc_discovery_url"`

	// The frequency of refreshing the JSON Web Keys (JWKS) used to validate access
	// tokens.
	//
	// requires_restart: false
	//
	// +optional
	OidcKeysRefreshInterval *int64 `json:"oidcKeysRefreshInterval,omitempty" property:"oidc_keys_refresh_interval"`

	// Rule for mapping JWT payload claim to a Redpanda user principal.
	//
	// requires_restart: false
	//
	// +optional
	OidcPrincipalMapping *string `json:"oidcPrincipalMapping,omitempty" property:"oidc_principal_mapping"`

	// A string representing the intended recipient of the token.
	//
	// requires_restart: false
	//
	// +optional
	OidcTokenAudience *string `json:"oidcTokenAudience,omitempty" property:"oidc_token_audience"`

	// When the disk usage of a node exceeds this threshold, it triggers Redpanda to
	// move partitions off of the node. This property applies only when
	// partition_autobalancing_mode is set to `continuous`.
	//
	// requires_restart: false
	// example: 52
	//
	// +optional
	PartitionAutobalancingMaxDiskUsagePercent *int64 `json:"partitionAutobalancingMaxDiskUsagePercent,omitempty" property:"partition_autobalancing_max_disk_usage_percent"`

	// Mode of partition balancing for a cluster. * `node_add`: partition balancing
	// happens when a node is added. * `continuous`: partition balancing happens
	// automatically to maintain optimal performance and availability, based on
	// continuous monitoring for node changes (same as `node_add`) and also high
	// disk usage. This option requires an Enterprise license, and it is customized
	// by `partition_autobalancing_node_availability_timeout_sec` and
	// `partition_autobalancing_max_disk_usage_percent` properties. * `off`:
	// partition balancing is disabled. This option is not recommended for
	// production clusters.
	//
	// requires_restart: false
	// allowed_values: [off, node_add, continuous]
	// example: node_add
	//
	// +optional
	PartitionAutobalancingMode *PartitionAutobalancingMode `json:"partitionAutobalancingMode,omitempty" property:"partition_autobalancing_mode"`

	// When a node is unavailable for at least this timeout duration, it triggers
	// Redpanda to move partitions off of the node. This property applies only when
	// `partition_autobalancing_mode` is set to `continuous`.
	//
	// requires_restart: false
	//
	// +optional
	PartitionAutobalancingNodeAvailabilityTimeoutSec *int64 `json:"partitionAutobalancingNodeAvailabilityTimeoutSec,omitempty" property:"partition_autobalancing_node_availability_timeout_sec"`

	// If `true`, Redpanda prioritizes balancing a topic’s partition replica count
	// evenly across all brokers while it’s balancing the cluster’s overall
	// partition count. Because different topics in a cluster can have vastly
	// different load profiles, this better distributes the workload of the most
	// heavily-used topics evenly across brokers.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	PartitionAutobalancingTopicAware *bool `json:"partitionAutobalancingTopicAware,omitempty" property:"partition_autobalancing_topic_aware"`

	// Default maximum number of bytes per partition on disk before triggering
	// deletion of the oldest messages. If `null` (the default value), no limit is
	// applied. The topic property `retention.bytes` overrides the value of
	// `retention_bytes` at the topic level.
	//
	// requires_restart: false
	//
	// +optional
	RetentionBytes *int64 `json:"retentionBytes,omitempty" property:"retention_bytes"`

	// Flag to allow Tiered Storage topics to expand to consumable retention policy
	// limits. When this flag is enabled, non-local retention settings are used, and
	// local retention settings are used to inform data removal policies in low-disk
	// space scenarios.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	RetentionLocalStrict *bool `json:"retentionLocalStrict,omitempty" property:"retention_local_strict"`

	// Trim log data when a cloud topic reaches its local retention limit. When this
	// option is disabled Redpanda will allow partitions to grow past the local
	// retention limit, and will be trimmed automatically as storage reaches the
	// configured target size.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	RetentionLocalStrictOverride *bool `json:"retentionLocalStrictOverride,omitempty" property:"retention_local_strict_override"`

	// Local retention size target for partitions of topics with object storage
	// write enabled. If `null`, the property is disabled. This property can be
	// overridden on a per-topic basis by setting `retention.local.target.bytes` in
	// each topic enabled for Tiered Storage. Both
	// `retention_local_target_bytes_default` and
	// `retention_local_target_ms_default` can be set. The limit that is reached
	// earlier is applied.
	//
	// requires_restart: false
	//
	// +optional
	RetentionLocalTargetBytesDefault *int64 `json:"retentionLocalTargetBytesDefault,omitempty" property:"retention_local_target_bytes_default"`

	// The target capacity (in bytes) that log storage will try to use before
	// additional retention rules take over to trim data to meet the target. When no
	// target is specified, storage usage is unbounded. Redpanda Data recommends
	// setting only one of `retention_local_target_capacity_bytes` or
	// `retention_local_target_capacity_percent`. If both are set, the minimum of
	// the two is used as the effective target capacity.
	//
	// requires_restart: false
	// example: 2147483648000
	//
	// +optional
	RetentionLocalTargetCapacityBytes *int64 `json:"retentionLocalTargetCapacityBytes,omitempty" property:"retention_local_target_capacity_bytes"`

	// The target capacity in percent of unreserved space
	// (`disk_reservation_percent`) that log storage will try to use before
	// additional retention rules will take over to trim data in order to meet the
	// target. When no target is specified storage usage is unbounded. Redpanda Data
	// recommends setting only one of `retention_local_target_capacity_bytes` or
	// `retention_local_target_capacity_percent`. If both are set, the minimum of
	// the two is used as the effective target capacity.
	//
	// requires_restart: false
	// example: 80.0
	//
	// +optional
	RetentionLocalTargetCapacityPercent *float64 `json:"retentionLocalTargetCapacityPercent,omitempty" property:"retention_local_target_capacity_percent"`

	// Local retention time target for partitions of topics with object storage
	// write enabled. This property can be overridden on a per-topic basis by
	// setting `retention.local.target.ms` in each topic enabled for Tiered Storage.
	// Both `retention_local_target_bytes_default` and
	// `retention_local_target_ms_default` can be set. The limit that is reached
	// first is applied.
	//
	// requires_restart: false
	//
	// +optional
	RetentionLocalTargetMsDefault *int64 `json:"retentionLocalTargetMsDefault,omitempty" property:"retention_local_target_ms_default"`

	// Resource manager's synchronization timeout. Specifies the maximum time for
	// this node to wait for the internal state machine to catch up with all events
	// written by previous leaders before rejecting a request.
	//
	// requires_restart: false
	//
	// +optional
	RmSyncTimeoutMs *int64 `json:"rmSyncTimeoutMs,omitempty" property:"rm_sync_timeout_ms"`

	// The maximum number of connections a broker will open to each of its peers.
	//
	// requires_restart: true
	// example: 8
	//
	// +optional
	RpcClientConnectionsPerPeer *int64 `json:"rpcClientConnectionsPerPeer,omitempty" property:"rpc_client_connections_per_peer"`

	// Maximum TCP connection queue length for Kafka server and internal RPC server.
	// If `null` (the default value), no queue length is set.
	//
	// requires_restart: true
	//
	// +optional
	RpcServerListenBacklog *int64 `json:"rpcServerListenBacklog,omitempty" property:"rpc_server_listen_backlog"`

	// Internal RPC TCP receive buffer size. If `null` (the default value), no
	// buffer size is set by Redpanda.
	//
	// requires_restart: true
	// example: 65536
	//
	// +optional
	RpcServerTcpRecvBuf *int64 `json:"rpcServerTcpRecvBuf,omitempty" property:"rpc_server_tcp_recv_buf"`

	// Internal RPC TCP send buffer size. If `null` (the default value), then no
	// buffer size is set by Redpanda.
	//
	// requires_restart: true
	// example: 65536
	//
	// +optional
	RpcServerTcpSendBuf *int64 `json:"rpcServerTcpSendBuf,omitempty" property:"rpc_server_tcp_send_buf"`

	// The location of the Kerberos `krb5.conf` file for Redpanda.
	//
	// requires_restart: false
	//
	// +optional
	SaslKerberosConfig *string `json:"saslKerberosConfig,omitempty" property:"sasl_kerberos_config"`

	// The location of the Kerberos keytab file for Redpanda.
	//
	// requires_restart: false
	//
	// +optional
	SaslKerberosKeytab *string `json:"saslKerberosKeytab,omitempty" property:"sasl_kerberos_keytab"`

	// The primary of the Kerberos Service Principal Name (SPN) for Redpanda.
	//
	// requires_restart: false
	//
	// +optional
	SaslKerberosPrincipal *string `json:"saslKerberosPrincipal,omitempty" property:"sasl_kerberos_principal"`

	// Rules for mapping Kerberos principal names to Redpanda user principals.
	//
	// requires_restart: false
	//
	// +optional
	SaslKerberosPrincipalMapping *[]string `json:"saslKerberosPrincipalMapping,omitempty" property:"sasl_kerberos_principal_mapping"`

	// A list of supported SASL mechanisms, if no override is defined in
	// `sasl_mechanisms_overrides` for each Kafka listener. Accepted values:
	// `SCRAM`, `GSSAPI`, `OAUTHBEARER`, `PLAIN`.  Note that in order to enable
	// PLAIN, you must also enable SCRAM.
	//
	// requires_restart: false
	//
	// +optional
	SaslMechanisms *[]string `json:"saslMechanisms,omitempty" property:"sasl_mechanisms"`

	// A list of overrides for SASL mechanisms, defined by listener. SASL mechanisms
	// defined here will replace the ones set in `sasl_mechanisms`. The same
	// limitations apply as for `sasl_mechanisms`.
	//
	// requires_restart: false
	// example: [{'listener':'kafka_listener', 'sasl_mechanisms':['SCRAM']}]
	//
	// +optional
	SaslMechanismsOverrides *[]SaslMechanismsOverride `json:"saslMechanismsOverrides,omitempty" property:"sasl_mechanisms_overrides"`

	// Always normalize schemas. If set, this overrides the normalize parameter in
	// API requests.
	//
	// requires_restart: false
	// example: true
	// aliases: [schema_registry_normalize_on_startup]
	//
	// +optional
	SchemaRegistryAlwaysNormalize *bool `json:"schemaRegistryAlwaysNormalize,omitempty" property:"schema_registry_always_normalize"`

	// Enable ACL-based authorization for Schema Registry requests. When true, uses
	// ACL-based authorization instead of the default public/user/superuser
	// authorization model. When false, uses the default authorization model.
	// Requires authentication to be enabled via schema_registry_api.authn_method.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	SchemaRegistryEnableAuthorization *bool `json:"schemaRegistryEnableAuthorization,omitempty" property:"schema_registry_enable_authorization"`

	// Option to explicitly disable automatic disk space management. If this
	// property was explicitly disabled while using v23.2, it will remain disabled
	// following an upgrade.
	//
	// requires_restart: false
	// example: false
	//
	// +optional
	SpaceManagementEnable *bool `json:"spaceManagementEnable,omitempty" property:"space_management_enable"`

	// Enable automatic space management. This option is ignored and deprecated in
	// versions >= v23.3.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	SpaceManagementEnableOverride *bool `json:"spaceManagementEnableOverride,omitempty" property:"space_management_enable_override"`

	// Requires that an empty file named `.redpanda_data_dir` be present in the
	// broker configuration `data_directory`. If set to `true`, Redpanda will refuse
	// to start if the file is not found in the data directory.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	StorageStrictDataInit *bool `json:"storageStrictDataInit,omitempty" property:"storage_strict_data_init"`

	// List of superuser usernames.
	//
	// requires_restart: false
	//
	// +optional
	Superusers *[]string `json:"superusers,omitempty" property:"superusers"`

	// The format of the certificates's distinguished name to use for mTLS principal
	// mapping.  Legacy format would appear as 'C=US,ST=California,L=San
	// Francisco,O=Redpanda,CN=redpanda', while rfc2253 format would appear as
	// 'CN=redpanda,O=Redpanda,L=San Francisco,ST=California,C=US'.
	//
	// requires_restart: false
	// allowed_values: [legacy, rfc2253]
	//
	// +optional
	TlsCertificateNameFormat *TlsCertificateNameFormat `json:"tlsCertificateNameFormat,omitempty" property:"tls_certificate_name_format"`

	// The minimum TLS version that Redpanda clusters support. This property
	// prevents client applications from negotiating a downgrade to the TLS version
	// when they make a connection to a Redpanda cluster.
	//
	// requires_restart: true
	// allowed_values: [v1.0, v1.1, v1.2, v1.3]
	//
	// +optional
	TlsMinVersion *TlsMinVersion `json:"tlsMinVersion,omitempty" property:"tls_min_version"`

	// Specifies the TLS 1.2 cipher suites available for external client connections
	// as a colon-separated OpenSSL-compatible list. Configure this property to
	// support legacy clients.
	//
	// requires_restart: true
	//
	// +optional
	TlsV12CipherSuites *string `json:"tlsV12CipherSuites,omitempty" property:"tls_v1_2_cipher_suites"`

	// Specifies the TLS 1.3 cipher suites available for external client connections
	// as a colon-separated OpenSSL-compatible list. Most deployments don't need to
	// modify this setting. Configure this property only for specific organizational
	// security policies.
	//
	// requires_restart: true
	//
	// +optional
	TlsV13CipherSuites *string `json:"tlsV13CipherSuites,omitempty" property:"tls_v1_3_cipher_suites"`

	// Transaction manager's synchronization timeout. Maximum time to wait for
	// internal state machine to catch up before rejecting a request.
	//
	// requires_restart: true
	//
	// +optional
	TmSyncTimeoutMs *int64 `json:"tmSyncTimeoutMs,omitempty" property:"tm_sync_timeout_ms"`

	// The retention time for tombstone records and transaction markers in a
	// compacted topic.
	//
	// requires_restart: false
	//
	// +optional
	TombstoneRetentionMs *int64 `json:"tombstoneRetentionMs,omitempty" property:"tombstone_retention_ms"`

	// Cleanup policy for a transaction coordinator topic. Accepted Values:
	// `compact`, `delete`, `["compact","delete"]`, `none`
	//
	// requires_restart: false
	// example: compact,delete
	//
	// +optional
	TransactionCoordinatorCleanupPolicy *string `json:"transactionCoordinatorCleanupPolicy,omitempty" property:"transaction_coordinator_cleanup_policy"`

	// Delete segments older than this age. To ensure transaction state is retained
	// as long as the longest-running transaction, make sure this is no less than
	// `transactional_id_expiration_ms`.
	//
	// requires_restart: false
	//
	// +optional
	TransactionCoordinatorDeleteRetentionMs *int64 `json:"transactionCoordinatorDeleteRetentionMs,omitempty" property:"transaction_coordinator_delete_retention_ms"`

	// Expiration time of producer IDs. Measured starting from the time of the last
	// write until now for a given ID. Producer IDs are automatically removed from
	// memory when they expire, which helps manage memory usage. However, this
	// natural cleanup may not be sufficient for workloads with high producer churn
	// rates. For applications with long-running transactions, ensure this value
	// accommodates your typical transaction lifetime to avoid premature producer ID
	// expiration.
	//
	// requires_restart: false
	//
	// +optional
	TransactionalIdExpirationMs *int64 `json:"transactionalIdExpirationMs,omitempty" property:"transactional_id_expiration_ms"`

	// Delay before scheduling the next check for timed out transactions.
	//
	// requires_restart: true
	//
	// +optional
	TxTimeoutDelayMs *int64 `json:"txTimeoutDelayMs,omitempty" property:"tx_timeout_delay_ms"`

	// Enables delete retention of consumer offsets topic. This is an internal-only
	// configuration and should be enabled only after consulting with Redpanda
	// support.
	//
	// requires_restart: true
	// example: true
	//
	// +optional
	UnsafeEnableConsumerOffsetsDeleteRetention *bool `json:"unsafeEnableConsumerOffsetsDeleteRetention,omitempty" property:"unsafe_enable_consumer_offsets_delete_retention"`

	// Enable cloud topics.
	//
	// requires_restart: false
	// example: true
	//
	// +optional
	UnstableBetaFeatureCloudTopicsEnabled *bool `json:"unstableBetaFeatureCloudTopicsEnabled,omitempty" property:"unstable_beta_feature_cloud_topics_enabled"`

	// The default write caching mode to apply to user topics. Write caching
	// acknowledges a message as soon as it is received and acknowledged on a
	// majority of brokers, without waiting for it to be written to disk. With
	// `acks=all`, this provides lower latency while still ensuring that a majority
	// of brokers acknowledge the write. Fsyncs follow
	// `raft_replica_max_pending_flush_bytes` and `raft_replica_max_flush_delay_ms`,
	// whichever is reached first. The `write_caching_default` cluster property can
	// be overridden with the `write.caching` topic property. Accepted values: *
	// `true` * `false` * `disabled`: This takes precedence over topic overrides and
	// disables write caching for the entire cluster.
	//
	// requires_restart: false
	// allowed_values: [true, false, disabled]
	// example: true
	//
	// +optional
	WriteCachingDefault *WriteCachingDefault `json:"writeCachingDefault,omitempty" property:"write_caching_default"`

	// Deprecations/Aliases

	// Deprecated: `wasmPerCoreMemoryReservation` has been deprecated, use `dataTransformsPerCoreMemoryReservation` instead
	// +optional
	DeprecatedWasmPerCoreMemoryReservation *int64 `json:"wasmPerCoreMemoryReservation,omitempty" property:"data_transforms_per_core_memory_reservation,alias:0"`
	// Deprecated: `wasmPerFunctionMemoryLimit` has been deprecated, use `dataTransformsPerFunctionMemoryLimit` instead
	// +optional
	DeprecatedWasmPerFunctionMemoryLimit *int64 `json:"wasmPerFunctionMemoryLimit,omitempty" property:"data_transforms_per_function_memory_limit,alias:0"`
	// Deprecated: `icebergRestCatalogAwsCredentialsSource` has been deprecated, use `icebergRestCatalogCredentialsSource` instead
	// +optional
	DeprecatedIcebergRestCatalogAwsCredentialsSource *IcebergRestCatalogCredentialsSource `json:"icebergRestCatalogAwsCredentialsSource,omitempty" property:"iceberg_rest_catalog_credentials_source,alias:0"`
	// Deprecated: `icebergRestCatalogPrefix` has been deprecated, use `icebergRestCatalogWarehouse` instead
	// +optional
	DeprecatedIcebergRestCatalogPrefix *string `json:"icebergRestCatalogPrefix,omitempty" property:"iceberg_rest_catalog_warehouse,alias:0"`
	// Deprecated: `deleteRetentionMs` has been deprecated, use `logRetentionMs` instead
	// +optional
	DeprecatedDeleteRetentionMs *int64 `json:"deleteRetentionMs,omitempty" property:"log_retention_ms,alias:0"`
	// Deprecated: `schemaRegistryNormalizeOnStartup` has been deprecated, use `schemaRegistryAlwaysNormalize` instead
	// +optional
	DeprecatedSchemaRegistryNormalizeOnStartup *bool `json:"schemaRegistryNormalizeOnStartup,omitempty" property:"schema_registry_always_normalize,alias:0"`
}

func (c *ConfigurationProperties) Equals(other *ConfigurationProperties) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if !primitiveEquals(c.AdminApiRequireAuth, other.AdminApiRequireAuth) {
		return false
	}
	if !primitiveEquals(c.AggregateMetrics, other.AggregateMetrics) {
		return false
	}
	if !primitiveEquals(c.AuditClientMaxBufferSize, other.AuditClientMaxBufferSize) {
		return false
	}
	if !primitiveEquals(c.AuditEnabled, other.AuditEnabled) {
		return false
	}
	if !arrayEquals(c.AuditEnabledEventTypes, other.AuditEnabledEventTypes) {
		return false
	}
	if !arrayEquals(c.AuditExcludedPrincipals, other.AuditExcludedPrincipals) {
		return false
	}
	if !arrayEquals(c.AuditExcludedTopics, other.AuditExcludedTopics) {
		return false
	}
	if !primitiveEquals(c.AuditFailurePolicy, other.AuditFailurePolicy) {
		return false
	}
	if !primitiveEquals(c.AuditLogNumPartitions, other.AuditLogNumPartitions) {
		return false
	}
	if !primitiveEquals(c.AuditLogReplicationFactor, other.AuditLogReplicationFactor) {
		return false
	}
	if !primitiveEquals(c.AutoCreateTopicsEnabled, other.AutoCreateTopicsEnabled) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAccessKey, other.CloudStorageAccessKey) {
		return false
	}
	if !primitiveEquals(c.CloudStorageApiEndpoint, other.CloudStorageApiEndpoint) {
		return false
	}
	if !primitiveEquals(c.CloudStorageApiEndpointPort, other.CloudStorageApiEndpointPort) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureAdlsEndpoint, other.CloudStorageAzureAdlsEndpoint) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureAdlsPort, other.CloudStorageAzureAdlsPort) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureContainer, other.CloudStorageAzureContainer) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureManagedIdentityId, other.CloudStorageAzureManagedIdentityId) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureSharedKey, other.CloudStorageAzureSharedKey) {
		return false
	}
	if !primitiveEquals(c.CloudStorageAzureStorageAccount, other.CloudStorageAzureStorageAccount) {
		return false
	}
	if !primitiveEquals(c.CloudStorageBackend, other.CloudStorageBackend) {
		return false
	}
	if !primitiveEquals(c.CloudStorageBucket, other.CloudStorageBucket) {
		return false
	}
	if !primitiveEquals(c.CloudStorageCacheSize, other.CloudStorageCacheSize) {
		return false
	}
	if !primitiveEquals(c.CloudStorageCacheSizePercent, other.CloudStorageCacheSizePercent) {
		return false
	}
	if !primitiveEquals(c.CloudStorageClusterName, other.CloudStorageClusterName) {
		return false
	}
	if !primitiveEquals(c.CloudStorageCredentialsSource, other.CloudStorageCredentialsSource) {
		return false
	}
	if !primitiveEquals(c.CloudStorageCrlFile, other.CloudStorageCrlFile) {
		return false
	}
	if !primitiveEquals(c.CloudStorageDisableArchiverManager, other.CloudStorageDisableArchiverManager) {
		return false
	}
	if !primitiveEquals(c.CloudStorageDisableTls, other.CloudStorageDisableTls) {
		return false
	}
	if !primitiveEquals(c.CloudStorageEnabled, other.CloudStorageEnabled) {
		return false
	}
	if !primitiveEquals(c.CloudStorageMaxConnections, other.CloudStorageMaxConnections) {
		return false
	}
	if !primitiveEquals(c.CloudStorageRegion, other.CloudStorageRegion) {
		return false
	}
	if !primitiveEquals(c.CloudStorageSecretKey, other.CloudStorageSecretKey) {
		return false
	}
	if !primitiveEquals(c.CloudStorageTrustFile, other.CloudStorageTrustFile) {
		return false
	}
	if !primitiveEquals(c.CloudStorageUrlStyle, other.CloudStorageUrlStyle) {
		return false
	}
	if !primitiveEquals(c.CloudTopicsProduceBatchingSizeThreshold, other.CloudTopicsProduceBatchingSizeThreshold) {
		return false
	}
	if !primitiveEquals(c.CloudTopicsProduceCardinalityThreshold, other.CloudTopicsProduceCardinalityThreshold) {
		return false
	}
	if !primitiveEquals(c.CloudTopicsProduceUploadInterval, other.CloudTopicsProduceUploadInterval) {
		return false
	}
	if !primitiveEquals(c.ClusterId, other.ClusterId) {
		return false
	}
	if !primitiveEquals(c.CoreBalancingContinuous, other.CoreBalancingContinuous) {
		return false
	}
	if !primitiveEquals(c.CoreBalancingOnCoreCountChange, other.CoreBalancingOnCoreCountChange) {
		return false
	}
	if !primitiveEquals(c.CpuProfilerEnabled, other.CpuProfilerEnabled) {
		return false
	}
	if !primitiveEquals(c.CpuProfilerSamplePeriodMs, other.CpuProfilerSamplePeriodMs) {
		return false
	}
	if !primitiveEquals(c.DataTransformsEnabled, other.DataTransformsEnabled) {
		return false
	}
	if !primitiveEquals(c.DataTransformsPerCoreMemoryReservation, other.DataTransformsPerCoreMemoryReservation) {
		return false
	}
	if !primitiveEquals(c.DataTransformsPerFunctionMemoryLimit, other.DataTransformsPerFunctionMemoryLimit) {
		return false
	}
	if !primitiveEquals(c.DatalakeDiskSpaceMonitorEnable, other.DatalakeDiskSpaceMonitorEnable) {
		return false
	}
	if !primitiveEquals(c.DatalakeScratchSpaceSoftLimitSizePercent, other.DatalakeScratchSpaceSoftLimitSizePercent) {
		return false
	}
	if !primitiveEquals(c.DebugBundleAutoRemovalSeconds, other.DebugBundleAutoRemovalSeconds) {
		return false
	}
	if !primitiveEquals(c.DebugBundleStorageDir, other.DebugBundleStorageDir) {
		return false
	}
	if !primitiveEquals(c.DefaultLeadersPreference, other.DefaultLeadersPreference) {
		return false
	}
	if !primitiveEquals(c.DefaultTopicPartitions, other.DefaultTopicPartitions) {
		return false
	}
	if !primitiveEquals(c.DefaultTopicReplications, other.DefaultTopicReplications) {
		return false
	}
	if !primitiveEquals(c.DisableMetrics, other.DisableMetrics) {
		return false
	}
	if !primitiveEquals(c.DisablePublicMetrics, other.DisablePublicMetrics) {
		return false
	}
	if !arrayEquals(c.EnableConsumerGroupMetrics, other.EnableConsumerGroupMetrics) {
		return false
	}
	if !primitiveEquals(c.EnableControllerLogRateLimiting, other.EnableControllerLogRateLimiting) {
		return false
	}
	if !primitiveEquals(c.EnableIdempotence, other.EnableIdempotence) {
		return false
	}
	if !primitiveEquals(c.EnableLeaderBalancer, other.EnableLeaderBalancer) {
		return false
	}
	if !primitiveEquals(c.EnableMetricsReporter, other.EnableMetricsReporter) {
		return false
	}
	if !primitiveEquals(c.EnableRackAwareness, other.EnableRackAwareness) {
		return false
	}
	if !primitiveEquals(c.EnableSasl, other.EnableSasl) {
		return false
	}
	if !primitiveEquals(c.EnableSchemaIdValidation, other.EnableSchemaIdValidation) {
		return false
	}
	if !primitiveEquals(c.EnableShadowLinking, other.EnableShadowLinking) {
		return false
	}
	if !primitiveEquals(c.EnableTransactions, other.EnableTransactions) {
		return false
	}
	if !primitiveEquals(c.EnableUsage, other.EnableUsage) {
		return false
	}
	if !primitiveEquals(c.FetchMaxBytes, other.FetchMaxBytes) {
		return false
	}
	if !primitiveEquals(c.GroupMaxSessionTimeoutMs, other.GroupMaxSessionTimeoutMs) {
		return false
	}
	if !primitiveEquals(c.GroupMinSessionTimeoutMs, other.GroupMinSessionTimeoutMs) {
		return false
	}
	if !arrayEquals(c.HttpAuthentication, other.HttpAuthentication) {
		return false
	}
	if !primitiveEquals(c.IcebergCatalogBaseLocation, other.IcebergCatalogBaseLocation) {
		return false
	}
	if !primitiveEquals(c.IcebergCatalogType, other.IcebergCatalogType) {
		return false
	}
	if !primitiveEquals(c.IcebergDefaultPartitionSpec, other.IcebergDefaultPartitionSpec) {
		return false
	}
	if !primitiveEquals(c.IcebergDelete, other.IcebergDelete) {
		return false
	}
	if !primitiveEquals(c.IcebergDisableAutomaticSnapshotExpiry, other.IcebergDisableAutomaticSnapshotExpiry) {
		return false
	}
	if !primitiveEquals(c.IcebergDisableSnapshotTagging, other.IcebergDisableSnapshotTagging) {
		return false
	}
	if !primitiveEquals(c.IcebergDlqTableSuffix, other.IcebergDlqTableSuffix) {
		return false
	}
	if !primitiveEquals(c.IcebergEnabled, other.IcebergEnabled) {
		return false
	}
	if !primitiveEquals(c.IcebergInvalidRecordAction, other.IcebergInvalidRecordAction) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogAuthenticationMode, other.IcebergRestCatalogAuthenticationMode) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogAwsAccessKey, other.IcebergRestCatalogAwsAccessKey) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogAwsRegion, other.IcebergRestCatalogAwsRegion) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogAwsSecretKey, other.IcebergRestCatalogAwsSecretKey) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogAwsServiceName, other.IcebergRestCatalogAwsServiceName) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogBaseLocation, other.IcebergRestCatalogBaseLocation) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogClientId, other.IcebergRestCatalogClientId) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogClientSecret, other.IcebergRestCatalogClientSecret) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogCredentialsSource, other.IcebergRestCatalogCredentialsSource) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogCrl, other.IcebergRestCatalogCrl) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogCrlFile, other.IcebergRestCatalogCrlFile) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogEndpoint, other.IcebergRestCatalogEndpoint) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogGcpUserProject, other.IcebergRestCatalogGcpUserProject) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogOauth2Scope, other.IcebergRestCatalogOauth2Scope) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogOauth2ServerUri, other.IcebergRestCatalogOauth2ServerUri) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogToken, other.IcebergRestCatalogToken) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogTrust, other.IcebergRestCatalogTrust) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogTrustFile, other.IcebergRestCatalogTrustFile) {
		return false
	}
	if !primitiveEquals(c.IcebergRestCatalogWarehouse, other.IcebergRestCatalogWarehouse) {
		return false
	}
	if !primitiveEquals(c.IcebergTargetLagMs, other.IcebergTargetLagMs) {
		return false
	}
	if !primitiveEquals(c.IcebergTopicNameDotReplacement, other.IcebergTopicNameDotReplacement) {
		return false
	}
	if !primitiveEquals(c.InitialRetentionLocalTargetBytesDefault, other.InitialRetentionLocalTargetBytesDefault) {
		return false
	}
	if !primitiveEquals(c.InitialRetentionLocalTargetMsDefault, other.InitialRetentionLocalTargetMsDefault) {
		return false
	}
	if !primitiveEquals(c.InternalTopicReplicationFactor, other.InternalTopicReplicationFactor) {
		return false
	}
	if !primitiveEquals(c.KafkaConnectionRateLimit, other.KafkaConnectionRateLimit) {
		return false
	}
	if !arrayEquals(c.KafkaConnectionRateLimitOverrides, other.KafkaConnectionRateLimitOverrides) {
		return false
	}
	if !primitiveEquals(c.KafkaConnectionsMax, other.KafkaConnectionsMax) {
		return false
	}
	if !arrayEquals(c.KafkaConnectionsMaxOverrides, other.KafkaConnectionsMaxOverrides) {
		return false
	}
	if !primitiveEquals(c.KafkaConnectionsMaxPerIp, other.KafkaConnectionsMaxPerIp) {
		return false
	}
	if !primitiveEquals(c.KafkaEnableAuthorization, other.KafkaEnableAuthorization) {
		return false
	}
	if !primitiveEquals(c.KafkaEnableDescribeLogDirsRemoteStorage, other.KafkaEnableDescribeLogDirsRemoteStorage) {
		return false
	}
	if !primitiveEquals(c.KafkaEnablePartitionReassignment, other.KafkaEnablePartitionReassignment) {
		return false
	}
	if !primitiveEquals(c.KafkaGroupRecoveryTimeoutMs, other.KafkaGroupRecoveryTimeoutMs) {
		return false
	}
	if !primitiveEquals(c.KafkaMemoryShareForFetch, other.KafkaMemoryShareForFetch) {
		return false
	}
	if !arrayEquals(c.KafkaMtlsPrincipalMappingRules, other.KafkaMtlsPrincipalMappingRules) {
		return false
	}
	if !arrayEquals(c.KafkaNodeleteTopics, other.KafkaNodeleteTopics) {
		return false
	}
	if !arrayEquals(c.KafkaNoproduceTopics, other.KafkaNoproduceTopics) {
		return false
	}
	if !primitiveEquals(c.KafkaQdcEnable, other.KafkaQdcEnable) {
		return false
	}
	if !primitiveEquals(c.KafkaQdcMaxLatencyMs, other.KafkaQdcMaxLatencyMs) {
		return false
	}
	if !primitiveEquals(c.KafkaRpcServerTcpRecvBuf, other.KafkaRpcServerTcpRecvBuf) {
		return false
	}
	if !primitiveEquals(c.KafkaRpcServerTcpSendBuf, other.KafkaRpcServerTcpSendBuf) {
		return false
	}
	if !primitiveEquals(c.KafkaSaslMaxReauthMs, other.KafkaSaslMaxReauthMs) {
		return false
	}
	if !complexArrayEquals(c.KafkaThroughputControl, other.KafkaThroughputControl) {
		return false
	}
	if !arrayEquals(c.KafkaThroughputControlledApiKeys, other.KafkaThroughputControlledApiKeys) {
		return false
	}
	if !primitiveEquals(c.KafkaThroughputLimitNodeInBps, other.KafkaThroughputLimitNodeInBps) {
		return false
	}
	if !primitiveEquals(c.KafkaThroughputLimitNodeOutBps, other.KafkaThroughputLimitNodeOutBps) {
		return false
	}
	if !primitiveEquals(c.KafkaTopicsMax, other.KafkaTopicsMax) {
		return false
	}
	if !primitiveEquals(c.LegacyPermitUnsafeLogOperation, other.LegacyPermitUnsafeLogOperation) {
		return false
	}
	if !primitiveEquals(c.LegacyUnsafeLogWarningIntervalSec, other.LegacyUnsafeLogWarningIntervalSec) {
		return false
	}
	if !primitiveEquals(c.LogCleanupPolicy, other.LogCleanupPolicy) {
		return false
	}
	if !primitiveEquals(c.LogCompactionIntervalMs, other.LogCompactionIntervalMs) {
		return false
	}
	if !primitiveEquals(c.LogCompressionType, other.LogCompressionType) {
		return false
	}
	if !primitiveEquals(c.LogMessageTimestampAfterMaxMs, other.LogMessageTimestampAfterMaxMs) {
		return false
	}
	if !primitiveEquals(c.LogMessageTimestampBeforeMaxMs, other.LogMessageTimestampBeforeMaxMs) {
		return false
	}
	if !primitiveEquals(c.LogMessageTimestampType, other.LogMessageTimestampType) {
		return false
	}
	if !primitiveEquals(c.LogRetentionMs, other.LogRetentionMs) {
		return false
	}
	if !primitiveEquals(c.LogSegmentMs, other.LogSegmentMs) {
		return false
	}
	if !primitiveEquals(c.MaxCompactionLagMs, other.MaxCompactionLagMs) {
		return false
	}
	if !primitiveEquals(c.MinCleanableDirtyRatio, other.MinCleanableDirtyRatio) {
		return false
	}
	if !primitiveEquals(c.MinCompactionLagMs, other.MinCompactionLagMs) {
		return false
	}
	if !primitiveEquals(c.MinimumTopicReplications, other.MinimumTopicReplications) {
		return false
	}
	if !primitiveEquals(c.NodeStatusReconnectMaxBackoffMs, other.NodeStatusReconnectMaxBackoffMs) {
		return false
	}
	if !primitiveEquals(c.OidcClockSkewTolerance, other.OidcClockSkewTolerance) {
		return false
	}
	if !primitiveEquals(c.OidcDiscoveryUrl, other.OidcDiscoveryUrl) {
		return false
	}
	if !primitiveEquals(c.OidcKeysRefreshInterval, other.OidcKeysRefreshInterval) {
		return false
	}
	if !primitiveEquals(c.OidcPrincipalMapping, other.OidcPrincipalMapping) {
		return false
	}
	if !primitiveEquals(c.OidcTokenAudience, other.OidcTokenAudience) {
		return false
	}
	if !primitiveEquals(c.PartitionAutobalancingMaxDiskUsagePercent, other.PartitionAutobalancingMaxDiskUsagePercent) {
		return false
	}
	if !primitiveEquals(c.PartitionAutobalancingMode, other.PartitionAutobalancingMode) {
		return false
	}
	if !primitiveEquals(c.PartitionAutobalancingNodeAvailabilityTimeoutSec, other.PartitionAutobalancingNodeAvailabilityTimeoutSec) {
		return false
	}
	if !primitiveEquals(c.PartitionAutobalancingTopicAware, other.PartitionAutobalancingTopicAware) {
		return false
	}
	if !primitiveEquals(c.RetentionBytes, other.RetentionBytes) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalStrict, other.RetentionLocalStrict) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalStrictOverride, other.RetentionLocalStrictOverride) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalTargetBytesDefault, other.RetentionLocalTargetBytesDefault) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalTargetCapacityBytes, other.RetentionLocalTargetCapacityBytes) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalTargetCapacityPercent, other.RetentionLocalTargetCapacityPercent) {
		return false
	}
	if !primitiveEquals(c.RetentionLocalTargetMsDefault, other.RetentionLocalTargetMsDefault) {
		return false
	}
	if !primitiveEquals(c.RmSyncTimeoutMs, other.RmSyncTimeoutMs) {
		return false
	}
	if !primitiveEquals(c.RpcClientConnectionsPerPeer, other.RpcClientConnectionsPerPeer) {
		return false
	}
	if !primitiveEquals(c.RpcServerListenBacklog, other.RpcServerListenBacklog) {
		return false
	}
	if !primitiveEquals(c.RpcServerTcpRecvBuf, other.RpcServerTcpRecvBuf) {
		return false
	}
	if !primitiveEquals(c.RpcServerTcpSendBuf, other.RpcServerTcpSendBuf) {
		return false
	}
	if !primitiveEquals(c.SaslKerberosConfig, other.SaslKerberosConfig) {
		return false
	}
	if !primitiveEquals(c.SaslKerberosKeytab, other.SaslKerberosKeytab) {
		return false
	}
	if !primitiveEquals(c.SaslKerberosPrincipal, other.SaslKerberosPrincipal) {
		return false
	}
	if !arrayEquals(c.SaslKerberosPrincipalMapping, other.SaslKerberosPrincipalMapping) {
		return false
	}
	if !arrayEquals(c.SaslMechanisms, other.SaslMechanisms) {
		return false
	}
	if !complexArrayEquals(c.SaslMechanismsOverrides, other.SaslMechanismsOverrides) {
		return false
	}
	if !primitiveEquals(c.SchemaRegistryAlwaysNormalize, other.SchemaRegistryAlwaysNormalize) {
		return false
	}
	if !primitiveEquals(c.SchemaRegistryEnableAuthorization, other.SchemaRegistryEnableAuthorization) {
		return false
	}
	if !primitiveEquals(c.SpaceManagementEnable, other.SpaceManagementEnable) {
		return false
	}
	if !primitiveEquals(c.SpaceManagementEnableOverride, other.SpaceManagementEnableOverride) {
		return false
	}
	if !primitiveEquals(c.StorageStrictDataInit, other.StorageStrictDataInit) {
		return false
	}
	if !arrayEquals(c.Superusers, other.Superusers) {
		return false
	}
	if !primitiveEquals(c.TlsCertificateNameFormat, other.TlsCertificateNameFormat) {
		return false
	}
	if !primitiveEquals(c.TlsMinVersion, other.TlsMinVersion) {
		return false
	}
	if !primitiveEquals(c.TlsV12CipherSuites, other.TlsV12CipherSuites) {
		return false
	}
	if !primitiveEquals(c.TlsV13CipherSuites, other.TlsV13CipherSuites) {
		return false
	}
	if !primitiveEquals(c.TmSyncTimeoutMs, other.TmSyncTimeoutMs) {
		return false
	}
	if !primitiveEquals(c.TombstoneRetentionMs, other.TombstoneRetentionMs) {
		return false
	}
	if !primitiveEquals(c.TransactionCoordinatorCleanupPolicy, other.TransactionCoordinatorCleanupPolicy) {
		return false
	}
	if !primitiveEquals(c.TransactionCoordinatorDeleteRetentionMs, other.TransactionCoordinatorDeleteRetentionMs) {
		return false
	}
	if !primitiveEquals(c.TransactionalIdExpirationMs, other.TransactionalIdExpirationMs) {
		return false
	}
	if !primitiveEquals(c.TxTimeoutDelayMs, other.TxTimeoutDelayMs) {
		return false
	}
	if !primitiveEquals(c.UnsafeEnableConsumerOffsetsDeleteRetention, other.UnsafeEnableConsumerOffsetsDeleteRetention) {
		return false
	}
	if !primitiveEquals(c.UnstableBetaFeatureCloudTopicsEnabled, other.UnstableBetaFeatureCloudTopicsEnabled) {
		return false
	}
	if !primitiveEquals(c.WriteCachingDefault, other.WriteCachingDefault) {
		return false
	}

	if !primitiveEquals(c.DeprecatedWasmPerCoreMemoryReservation, other.DeprecatedWasmPerCoreMemoryReservation) {
		return false
	}
	if !primitiveEquals(c.DeprecatedWasmPerFunctionMemoryLimit, other.DeprecatedWasmPerFunctionMemoryLimit) {
		return false
	}
	if !primitiveEquals(c.DeprecatedIcebergRestCatalogAwsCredentialsSource, other.DeprecatedIcebergRestCatalogAwsCredentialsSource) {
		return false
	}
	if !primitiveEquals(c.DeprecatedIcebergRestCatalogPrefix, other.DeprecatedIcebergRestCatalogPrefix) {
		return false
	}
	if !primitiveEquals(c.DeprecatedDeleteRetentionMs, other.DeprecatedDeleteRetentionMs) {
		return false
	}
	if !primitiveEquals(c.DeprecatedSchemaRegistryNormalizeOnStartup, other.DeprecatedSchemaRegistryNormalizeOnStartup) {
		return false
	}

	return true
}

// TODO: ThroughputControlGroup docs
type ThroughputControlGroup struct {
	// +optional
	Name *string `json:"name,omitempty" property:"name"`

	// +optional
	ClientId *string `json:"clientId,omitempty" property:"client_id"`
}

func (t ThroughputControlGroup) Equals(other ThroughputControlGroup) bool {
	if !primitiveEquals(t.Name, other.Name) {
		return false
	}

	return primitiveEquals(t.ClientId, other.ClientId)
}

// TODO: SaslMechanismsOverride docs
type SaslMechanismsOverride struct {
	// +optional
	Listener *string `json:"listener,omitempty" property:"listener"`

	// +optional
	SaslMechanisms *[]string `json:"saslMechanisms,omitempty" property:"sasl_mechanisms"`
}

func (s SaslMechanismsOverride) Equals(other SaslMechanismsOverride) bool {
	if !primitiveEquals(s.Listener, other.Listener) {
		return false
	}

	return arrayEquals(s.SaslMechanisms, other.SaslMechanisms)
}

// +kubebuilder:validation:XValidation:message="leadersPreference must be either 'none' or a comma separated list starting with 'racks:'",rule="self == 'none'  || self.startsWith('racks:')"
type LeadersPreference string

func primitiveEquals[T ~string | bool | float64 | int64](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	return *a == *b
}

func arrayEquals[T ~string | bool | float64 | int64](a, b *[]T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	for i := range *a {
		if (*a)[i] != (*b)[i] {
			return false
		}
	}

	return true
}

type equalityChecker[T any] interface {
	Equals(T) bool
}

func complexArrayEquals[T any, U equalityChecker[T]](a *[]U, b *[]T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	for i := range *a {
		if !(*a)[i].Equals((*b)[i]) {
			return false
		}
	}

	return true
}
