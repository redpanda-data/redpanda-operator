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
	TaskStateUnavailable TaskState = "unvailable"
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
}

// State of a shadow topic
type ShadowTopicState string

const (
	ShadowTopicStateUnknown ShadowTopicState = ""
	// Shadow topic is active
	ShadowTopicStateActive ShadowTopicState = "active"
	// Shadow topic has been promoted
	ShadowTopicStatePromoted ShadowTopicState = "promoted"
	// Shadow topic has faulted
	ShadowTopicStateFaulted ShadowTopicState = "faulted"
	// Shadow topic has been paused
	ShadowTopicStatePaused ShadowTopicState = "paused"
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
	// the following are immutable, since we derive those from our cluster sources anyway, which are already immutable, that should be fine
	// "configurations", "client_options", "bootstrap_servers"
	// "configurations", "client_options", "tls_settings"

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
	// options for schema registry
	SchemaRegistrySyncOptions *ShadowLinkSchemaRegistrySyncOptions `json:"schemaRegistrySyncOptions,omitempty"`
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
}

// Options for syncing consumer offsets
type ShadowLinkConsumerOffsetSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval *metav1.Duration `json:"interval,omitempty"`
	// Whether it's enabled
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
	// Whether or not it's enabled
	Paused bool `json:"paused,omitempty"`
	// ACL filters
	ACLFilters []ACLFilter `json:"aclFilters,omitempty"`
}

type ShadowLinkSchemaRegistrySyncOptionsMode string

const (
	ShadowLinkSchemaRegistrySyncOptionsModeNone  ShadowLinkSchemaRegistrySyncOptionsMode = ""
	ShadowLinkSchemaRegistrySyncOptionsModeTopic ShadowLinkSchemaRegistrySyncOptionsMode = "topic"
)

// Options for syncing schema registry settings
type ShadowLinkSchemaRegistrySyncOptions struct {
	Mode ShadowLinkSchemaRegistrySyncOptionsMode `json:"schema_registry_shadowing_mode,omitempty"`
}
