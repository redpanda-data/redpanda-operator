// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// ShadowLink defines the CRD for ShadowLink cluster configuration.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=shadowlinks
// +kubebuilder:resource:shortName=sl
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
type ShadowLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ShadowLinkSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status ShadowLinkStatus `json:"status,omitempty"`
}

func (n *ShadowLink) GetClusterSource() *ClusterSource {
	return n.Spec.ShadowCluster
}

func (n *ShadowLink) GetRemoteClusterSource() *ClusterSource {
	return n.Spec.SourceCluster
}

// +kubebuilder:object:root=true
type ShadowLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShadowLink `json:"items"`
}

func (s *ShadowLinkList) GetItems() []*ShadowLink {
	return functional.MapFn(ptr.To, s.Items)
}

// State of the shadow link
type ShadowLinkState string

const (
	// Unspecified
	ShadowLinkStateUnknown ShadowLinkState = ""
	// Shadow link is active
	ShadowLinkStateActive ShadowLinkState = "active"
	// Shadow link was paused
	ShadowLinkStatePaused ShadowLinkState = "paused"
)

// ShadowLinkStatus defines the observed state of any node pools tied to this cluster
type ShadowLinkStatus struct {
	// State of the shadow link
	State ShadowLinkState `json:"state,omitempty"`
	// Statuses of the running tasks
	TaskStatuses []ShadowLinkTaskStatus `json:"taskStatuses,omitempty"`
	// Status of shadow topics
	ShadowTopicStatuses []ShadowTopicStatus `json:"shadowTopicStatuses,omitempty"`

	// Conditions holds the conditions for the ShadowLink.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Task states
type TaskState string

const (
	TaskStateUnknown TaskState = ""
	// Task is active
	TaskStateActive TaskState = "active"
	// Task was paused
	TaskStatePaused TaskState = "paused"
	// Task is unable to communicate with source cluster
	TaskStateUnavailable TaskState = "unavailable"
	// Task is not running
	TaskStateNotRunning TaskState = "not running"
	// Task is faulted
	TaskStateFaulted TaskState = "faulted"
)

type ShadowLinkTaskStatus struct {
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Name of the task
	Name string `json:"name,omitempty"`
	// State of the task
	State TaskState `json:"state,omitempty"`
	// Reason for task being in state
	Reason string `json:"reason,omitempty"`
	// The broker the task is running on
	BrokerID int32 `json:"brokerId,omitempty"`
	// The shard the task is running on
	Shard int32 `json:"shard,omitempty"`
}

// State of a shadow topic
type ShadowTopicState string

const (
	ShadowTopicStateUnknown ShadowTopicState = ""
	// Shadow topic is active
	ShadowTopicStateActive ShadowTopicState = "active"
	// Shadow topic has faulted
	ShadowTopicStateFaulted ShadowTopicState = "faulted"
	// Shadow topic has been paused
	ShadowTopicStatePaused ShadowTopicState = "paused"
	// Shadow topic is in the process of failing over
	ShadowTopicStateFailingOver ShadowTopicState = "failing over"
	// Shadow topic is in the process of being promoted
	ShadowTopicStateFailedOver ShadowTopicState = "failed over"
	// Shadow topic is in the process of being promoted
	ShadowTopicStatePromoting ShadowTopicState = "promoting"
	// Shadow topic has failed over successfully
	ShadowTopicStatePromoted ShadowTopicState = "promoted"
)

// Status of a ShadowTopic
type ShadowTopicStatus struct {
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Name of the shadow topic
	Name string `json:"name,omitempty"`
	// Topic ID of the shadow topic
	TopicID string `json:"topicId,omitempty"`
	// State of the shadow topic
	State ShadowTopicState `json:"state,omitempty"`
}

type ShadowLinkSpec struct {
	// From: https://github.com/redpanda-data/redpanda/blob/60c590be34d5b2bd2934ac2143105ee7e2442388/src/v/redpanda/admin/services/shadow_link/shadow_link.cc#L64C1-L66C57
	// The following client_options identity fields are immutable, since we
	// derive them from our cluster sources anyway, which are already immutable:
	// "configurations", "client_options", "bootstrap_servers"
	// "configurations", "client_options", "tls_settings"
	// The remaining client_options are mutable tuning knobs, surfaced via the
	// ClientOptions field below.

	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ClusterSource is immutable"
	ShadowCluster *ClusterSource `json:"shadowCluster"`
	// +kubebuilder:validation:XValidation:rule="(!has(self.clusterRef) && !has(oldSelf.clusterRef)) || (self.clusterRef == oldSelf.clusterRef)",message="ClusterSource clusterRef is immutable"
	// +kubebuilder:validation:XValidation:rule="!has(self.staticConfiguration) || has(self.staticConfiguration.kafka)",message="static configuration must contain a kafka block"
	SourceCluster *ClusterSource `json:"sourceCluster"`

	// Topic metadata sync options
	TopicMetadataSyncOptions *ShadowLinkTopicMetadataSyncOptions `json:"topicMetadataSyncOptions,omitempty"`
	// Consumer offset sync options
	ConsumerOffsetSyncOptions *ShadowLinkConsumerOffsetSyncOptions `json:"consumerOffsetSyncOptions,omitempty"`
	// Security settings sync options
	SecuritySyncOptions *ShadowLinkSecuritySettingsSyncOptions `json:"securitySyncOptions,omitempty"`
	// Schema registry sync options. Schemas are replicated from the source
	// cluster by default; set
	// schemaRegistrySyncOptions.schema_registry_shadowing_mode to disabled
	// to turn schema replication off.
	// +kubebuilder:default={}
	SchemaRegistrySyncOptions *ShadowLinkSchemaRegistrySyncOptions `json:"schemaRegistrySyncOptions,omitempty"`
	// RBAC role sync options. Roles are replicated from the source cluster
	// by default; set roleSyncOptions.enabled to false to turn role
	// replication off.
	// +kubebuilder:default={}
	RoleSyncOptions *ShadowLinkRoleSyncOptions `json:"roleSyncOptions,omitempty"`
	// Tuning knobs for the Kafka client the shadow cluster uses to fetch data
	// from the source cluster. Connection details (bootstrap servers, TLS) are
	// derived from sourceCluster and are not configurable here; only the mutable
	// performance/latency knobs are exposed.
	ClientOptions *ShadowLinkClientOptions `json:"clientOptions,omitempty"`
}

// ShadowLinkClientOptions configures the source-cluster Kafka fetch/connection
// behavior of a shadow link. These knobs primarily control replication latency
// vs. batching efficiency. Every field defaults server-side when left at 0, so
// omitting a field (or setting it to 0) preserves the Redpanda default noted in
// its documentation.
type ShadowLinkClientOptions struct {
	// Minimum bytes the source broker accumulates before answering a fetch
	// request. Lowering this reduces replication latency at low throughput, at
	// the cost of more, smaller fetches. If 0, defaults to 5 MiB (5242880).
	// +optional
	// +kubebuilder:validation:Minimum=0
	FetchMinBytes int32 `json:"fetchMinBytes,omitempty"`
	// Maximum time in milliseconds the source broker waits to satisfy
	// fetchMinBytes before answering a fetch request. Lowering this caps the
	// worst-case replication latency when fetchMinBytes is not met. If 0,
	// defaults to 500ms.
	// +optional
	// +kubebuilder:validation:Minimum=0
	FetchWaitMaxMs int32 `json:"fetchWaitMaxMs,omitempty"`
	// Maximum bytes returned by a single fetch request. If 0, defaults to 20 MiB (20971520).
	// +optional
	// +kubebuilder:validation:Minimum=0
	FetchMaxBytes int32 `json:"fetchMaxBytes,omitempty"`
	// Maximum bytes returned per partition in a fetch request. If 0, defaults to 5 MiB (5242880).
	// +optional
	// +kubebuilder:validation:Minimum=0
	FetchPartitionMaxBytes int32 `json:"fetchPartitionMaxBytes,omitempty"`
	// How often in milliseconds the client refreshes source cluster metadata. If 0, defaults to 10000ms.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MetadataMaxAgeMs int32 `json:"metadataMaxAgeMs,omitempty"`
	// Connection timeout to the source cluster in milliseconds. If 0, defaults to 1000ms.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ConnectionTimeoutMs int32 `json:"connectionTimeoutMs,omitempty"`
	// Base backoff between connection retries in milliseconds. If 0, defaults to 100ms.
	// +optional
	// +kubebuilder:validation:Minimum=0
	RetryBackoffMs int32 `json:"retryBackoffMs,omitempty"`
}

// FilterType specifies the type, either include or exclude of a consumer group filter.
// +kubebuilder:validation:Enum=include;exclude
type FilterType string

var (
	FilterTypeUnknown FilterType = ""
	FilterTypeInclude FilterType = "include"
	FilterTypeExclude FilterType = "exclude"
)

// A filter based on the name of a resource
type NameFilter struct {
	// The resource name, or "*"
	// Note if the wildcar "*" is used it must be the _only_ character
	// and `patternType` must be `literal`
	// +kubebuilder:default=*
	Name string `json:"name,omitempty"`
	// Valid values:
	// - include
	// - exclude
	FilterType FilterType `json:"filterType"`
	// Default value is literal. Valid values:
	// - literal
	// - prefixed
	//
	// +kubebuilder:default=literal
	PatternType PatternType `json:"patternType,omitempty"`
}

// Filter an ACL based on its access
type ACLAccessFilter struct {
	// The host to match.  If not set, will default to match all hosts
	// with the specified `operation` and `permissionType`. Note that
	// the asterisk `*` is literal and matches hosts that are set to `*`
	Host string `json:"host,omitempty"`
	// The ACL operation to match
	Operation *ACLOperation `json:"operation,omitempty"`
	// The permission type
	PermissionType *ACLType `json:"permissionType,omitempty"`
	// The name of the principal, if not set will default to match
	// all principals with the specified `operation` and `permissionType`
	// +kubebuilder:default=*
	Principal string `json:"principal"`
}

type ACLResourceFilter struct {
	// +kubebuilder:default=*
	Name         string        `json:"name"`
	PatternType  *PatternType  `json:"patternType,omitempty"`
	ResourceType *ResourceType `json:"resourceType,omitempty"`
}

// A filter for ACLs
type ACLFilter struct {
	// The access filter
	AccessFilter ACLAccessFilter `json:"accessFilter"`
	// The resource filter
	ResourceFilter ACLResourceFilter `json:"resourceFilter"`
}

// +kubebuilder:validation:Enum=earliest;latest;timestamp
type TopicMetadataSyncOffset string

const (
	TopicMetadataSyncOffsetEarliest  TopicMetadataSyncOffset = "earliest"
	TopicMetadataSyncOffsetLatest    TopicMetadataSyncOffset = "latest"
	TopicMetadataSyncOffsetTimestamp TopicMetadataSyncOffset = "timestamp"
)

// Options for syncing topic metadata
// +kubebuilder:validation:XValidation:message="startOffsetTimestamp must be specified when startOffset is set to timestamp",rule="has(self.startOffset) && ((self.startOffset != 'timestamp') || has(self.startOffsetTimestamp))"
type ShadowLinkTopicMetadataSyncOptions struct {
	// How often to sync metadata
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval *metav1.Duration `json:"interval,omitempty"`
	// List of filters that indicate which topics should be automatically
	// created as shadow topics on the shadow cluster.  This only controls
	// automatic creation of shadow topics and does not effect the state of the
	// mirror topic once it is created.
	// Literal filters for __consumer_offsets and _redpanda.audit_log will be
	// rejected as well as prefix filters to match topics prefixed with
	// _redpanda or __redpanda.
	// Wildcard `*` is permitted only for literal filters and will _not_ match
	// any topics that start with _redpanda or __redpanda.  If users wish to
	// shadow topics that start with _redpanda or __redpanda, they should
	// provide a literal filter for those topics.
	AutoCreateShadowTopicFilters []NameFilter `json:"autoCreateShadowTopicFilters,omitempty"`
	// List of topic properties that should be synced from the source topic.
	// The following properties will always be replicated
	// - Partition count
	// - `max.message.bytes`
	// - `cleanup.policy`
	// - `timestamp.type`
	//
	// The following properties are not allowed to be replicated and adding them
	// to this list will result in an error:
	// - `redpanda.remote.readreplica`
	// - `redpanda.remote.recovery`
	// - `redpanda.remote.allowgaps`
	// - `redpanda.virtual.cluster.id`
	// - `redpanda.leaders.preference`
	// - `redpanda.cloud_topic.enabled`
	//
	// This list is a list of properties in addition to the default properties
	// that will be synced.  See `excludeDefault`.
	SyncedShadowTopicProperties []string `json:"syncedShadowTopicProperties,omitempty"`
	// If false, then the following topic properties will be synced by default:
	// - `compression.type`
	// - `retention.bytes`
	// - `retention.ms`
	// - `delete.retention.ms`
	// - Replication Factor
	// - `min.compaction.lag.ms`
	// - `max.compaction.lag.ms`
	//
	// If this is true, then only the properties listed in
	// `synced_shadow_topic_properties` will be synced.
	ExcludeDefault bool `json:"excludeDefault,omitempty"`
	// The starting offset for new shadow topic partitions.
	// Defaults to earliest.
	// Only applies if the shadow partition is empty.
	// +kubebuilder:default="earliest"
	StartOffset *TopicMetadataSyncOffset `json:"startOffset,omitempty"`
	// The timestamp to start at if `startOffset`` is set to "timestamp".
	// Not providing this when setting `startOffset` to "timestamp" is
	// an error.
	StartOffsetTimestamp *metav1.Time `json:"startOffsetTimestamp,omitempty"`
	// Allows user to pause the topic sync task.  If paused, then
	// the task will enter the 'paused' state and not sync topics or their
	// properties from the source cluster
	Paused bool `json:"paused,omitempty"`
}

// Options for syncing consumer offsets
type ShadowLinkConsumerOffsetSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval *metav1.Duration `json:"interval,omitempty"`
	// Allows user to pause the consumer offset sync task.  If paused, then
	// the task will enter the 'paused' state and not sync consumer offsets from
	// the source cluster
	Paused bool `json:"paused,omitempty"`
	// The filters
	GroupFilters []NameFilter `json:"groupFilters,omitempty"`
}

// Options for syncing security settings
type ShadowLinkSecuritySettingsSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval *metav1.Duration `json:"interval,omitempty"`
	// Allows user to pause the security settings sync task.  If paused,
	// then the task will enter the 'paused' state and will not sync security
	// settings from the source cluster
	Paused bool `json:"paused,omitempty"`
	// ACL filters
	ACLFilters []ACLFilter `json:"aclFilters,omitempty"`
}

// Options for syncing RBAC roles
type ShadowLinkRoleSyncOptions struct {
	// Enabled controls whether RBAC role definitions and role memberships
	// are replicated from the source cluster. Defaults to true, which
	// replicates every role (subject to roleNameFilters). Set to false to
	// turn role replication off.
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`
	// How often to sync roles
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval *metav1.Duration `json:"interval,omitempty"`
	// Allows user to pause the role sync task. If paused, then the task
	// will enter the 'paused' state and not sync roles from the source
	// cluster
	Paused bool `json:"paused,omitempty"`
	// List of filters that select which roles are replicated. Defaults to
	// a single include-all filter so that every role is replicated.
	// +kubebuilder:default={{name: "*", filterType: "include", patternType: "literal"}}
	RoleNameFilters []NameFilter `json:"roleNameFilters,omitempty"`
}

// ShadowLinkSchemaRegistrySyncOptionsMode selects how schemas are replicated
// from the source cluster. It mirrors the schema_registry_shadowing_mode
// oneof in shadow_link.proto so that additional modes can be introduced.
// +kubebuilder:validation:Enum=disabled;topic;api
type ShadowLinkSchemaRegistrySyncOptionsMode string

const (
	// Schemas are not replicated.
	ShadowLinkSchemaRegistrySyncOptionsModeDisabled ShadowLinkSchemaRegistrySyncOptionsMode = "disabled"
	// The source Redpanda cluster's internal schemas topic is shadowed
	// byte-for-byte.
	ShadowLinkSchemaRegistrySyncOptionsModeTopic ShadowLinkSchemaRegistrySyncOptionsMode = "topic"
	// Schemas are replicated from the source schema registry over its REST
	// API, configured via shadowSchemaRegistryAPI.
	ShadowLinkSchemaRegistrySyncOptionsModeAPI ShadowLinkSchemaRegistrySyncOptionsMode = "api"
)

// Options for syncing schema registry settings
// +kubebuilder:validation:XValidation:message="shadowSchemaRegistryAPI is required when schema_registry_shadowing_mode is api",rule="!has(self.schema_registry_shadowing_mode) || self.schema_registry_shadowing_mode != 'api' || has(self.shadowSchemaRegistryAPI)"
// +kubebuilder:validation:XValidation:message="shadowSchemaRegistryAPI may only be set when schema_registry_shadowing_mode is api",rule="!has(self.shadowSchemaRegistryAPI) || (has(self.schema_registry_shadowing_mode) && self.schema_registry_shadowing_mode == 'api')"
type ShadowLinkSchemaRegistrySyncOptions struct {
	// Mode selects how schemas are replicated from the source cluster.
	// Defaults to topic, which shadows the source Redpanda cluster's
	// internal schemas topic. Set to api to replicate from the source
	// schema registry over its REST API (configured via
	// shadowSchemaRegistryAPI), or disabled to turn schema replication off.
	// +kubebuilder:default=topic
	Mode ShadowLinkSchemaRegistrySyncOptionsMode `json:"schema_registry_shadowing_mode,omitempty"`
	// Configuration for replicating schemas from the source cluster's
	// schema registry REST API (for example a Confluent Schema Registry)
	// instead of shadowing a source Redpanda cluster's internal schemas
	// topic. Required when mode is api and forbidden otherwise.
	ShadowSchemaRegistryAPI *ShadowLinkSchemaRegistryAPIOptions `json:"shadowSchemaRegistryAPI,omitempty"`
}

// UnsupportedSchemaFeaturePolicy controls what happens when a source schema
// uses features that the Redpanda schema registry does not support.
// +kubebuilder:validation:Enum=fail;remove
type UnsupportedSchemaFeaturePolicy string

const (
	// The schema is not replicated. The sync records an error, reports it
	// in the link status, and continues with the remaining schemas.
	UnsupportedSchemaFeaturePolicyFail UnsupportedSchemaFeaturePolicy = "fail"
	// The unsupported feature is stripped and the rest of the schema is
	// replicated.
	UnsupportedSchemaFeaturePolicyRemove UnsupportedSchemaFeaturePolicy = "remove"
)

// Options for replicating schemas from a source schema registry over its
// REST API.
type ShadowLinkSchemaRegistryAPIOptions struct {
	// URL of the source schema registry, for example
	// https://psrc-xxxxx.us-east-1.aws.confluent.cloud
	// +kubebuilder:validation:MinLength=1
	SourceURL string `json:"sourceURL"`
	// Authentication options used to connect to the source schema
	// registry.
	Authentication *ShadowLinkSchemaRegistryAuthentication `json:"authentication,omitempty"`
	// TLS settings used to connect to the source schema registry. Unlike
	// Kafka API connections, schema registry connection material is passed
	// to the cluster as PEM data, so insecureSkipTlsVerify and the
	// deprecated secret-reference fields are not supported here.
	// +kubebuilder:validation:XValidation:message="insecureSkipTlsVerify is not supported for schema registry connections",rule="!has(self.insecureSkipTlsVerify) || !self.insecureSkipTlsVerify"
	// +kubebuilder:validation:XValidation:message="use caCert, cert, and key rather than the deprecated secret-reference fields",rule="!has(self.caCertSecretRef) && !has(self.certSecretRef) && !has(self.keySecretRef)"
	TLS *CommonTLS `json:"tls,omitempty"`
	// How often to poll the source registry for incremental changes.
	// If not provided, the cluster default (10s) is used.
	TailInterval *metav1.Duration `json:"tailInterval,omitempty"`
	// How often to run a full scan of the source registry.
	// If not provided, the cluster default (5m) is used.
	FullSyncInterval *metav1.Duration `json:"fullSyncInterval,omitempty"`
	// Rate limit for requests against the source registry.
	// If not provided, the cluster default (30) is used.
	// +kubebuilder:validation:Minimum=1
	MaxSourceRequestsPerSecond *int32 `json:"maxSourceRequestsPerSecond,omitempty"`
	// Selects which schema registry contexts and subjects are replicated.
	// If not provided, the entire source registry is replicated.
	SourceFilter *ShadowLinkSchemaRegistrySourceFilter `json:"sourceFilter,omitempty"`
	// Destination context mapping for source schema registry data. If not
	// provided, source context names are preserved on the shadow cluster.
	Destination *ShadowLinkSchemaRegistryContextDestination `json:"destination,omitempty"`
	// Policy applied when a source schema uses features that the Redpanda
	// schema registry does not support. Defaults to fail.
	UnsupportedSchemaFeaturePolicy *UnsupportedSchemaFeaturePolicy `json:"unsupportedSchemaFeaturePolicy,omitempty"`
	// Allows the user to pause the schema registry sync task. If paused,
	// the task enters the 'paused' state, stops replicating schemas from
	// the source, and the per-context client write protection on the
	// contexts this link owns is lifted.
	Paused bool `json:"paused,omitempty"`
}

// Authentication options for a source schema registry.
type ShadowLinkSchemaRegistryAuthentication struct {
	// HTTP basic authentication credentials. For a Confluent Schema
	// Registry these are the schema registry API key (username) and API
	// secret (password).
	Basic *ShadowLinkSchemaRegistryBasicAuthentication `json:"basic,omitempty"`
}

// HTTP basic authentication credentials for a source schema registry.
type ShadowLinkSchemaRegistryBasicAuthentication struct {
	// Username used for basic authentication, for example a Confluent
	// Schema Registry API key. May reference a Kubernetes Secret.
	Username ValueSource `json:"username"`
	// Password used for basic authentication, for example a Confluent
	// Schema Registry API secret. May reference a Kubernetes Secret.
	Password ValueSource `json:"password"`
}

// Selects which contexts and subjects are replicated from a source schema
// registry.
type ShadowLinkSchemaRegistrySourceFilter struct {
	// Schema registry contexts to replicate, for example ".". If empty,
	// all contexts are replicated.
	Contexts []string `json:"contexts,omitempty"`
	// Subjects to replicate within the selected contexts. If empty, all
	// subjects are replicated.
	Subjects []string `json:"subjects,omitempty"`
}

// Destination context mapping for source schema registry data. It mirrors
// the SchemaRegistryContextDestination oneof in shadow_link.proto: exactly
// one of identity or exact must be set.
// +kubebuilder:validation:XValidation:message="exactly one of identity or exact must be set",rule="has(self.identity) != has(self.exact)"
type ShadowLinkSchemaRegistryContextDestination struct {
	// Preserve source context names in the destination schema registry.
	// Mutually exclusive with exact.
	Identity *ShadowLinkSchemaRegistryIdentityContextMapping `json:"identity,omitempty"`
	// Map selected source contexts to explicit destination contexts. Every
	// source context in the effective source scope must have exactly one
	// mapping. Mutually exclusive with identity.
	// +kubebuilder:validation:MinItems=1
	Exact []ShadowLinkSchemaRegistryContextMapping `json:"exact,omitempty"`
}

// Preserve source context names in the destination schema registry.
type ShadowLinkSchemaRegistryIdentityContextMapping struct{}

// Maps a source schema registry context to a destination context on the
// shadow cluster.
type ShadowLinkSchemaRegistryContextMapping struct {
	// The source context name.
	Source string `json:"source"`
	// The destination context name on the shadow cluster.
	Destination string `json:"destination"`
}
