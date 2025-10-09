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

	Spec   ShadowLinkSpec   `json:"spec,omitempty"`
	Status ShadowLinkStatus `json:"status,omitempty"`
}

func (n *ShadowLink) GetClusterSource() *ClusterSource {
	return &n.Spec.ShadowCluster
}

func (n *ShadowLink) GetRemoteClusterSource() *ClusterSource {
	return &n.Spec.SourceCluster
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

	ShadowCluster ClusterSource `json:"shadowCluster"`
	SourceCluster ClusterSource `json:"sourceCluster"`

	// Topic metadata sync options
	TopicMetadataSyncOptions *ShadowLinkTopicMetadataSyncOptions `json:"topicMetadataSyncOptions,omitempty"`
	// Consumer offset sync options
	ConsumerOffsetSyncOptions *ShadowLinkConsumerOffsetSyncOptions `json:"consumerOffsetSyncOptions,omitempty"`
	// Security settings sync options
	SecuritySyncOptions *ShadowLinkSecuritySettingsSyncOptions `json:"securitySyncOptions,omitempty"`
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

// Options for syncing topic metadata
type ShadowLinkTopicMetadataSyncOptions struct {
	// How often to sync metadata
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval metav1.Duration `json:"interval,omitempty"`
	// The topic filters to use
	AutoCreateShadowTopicFilters []NameFilter `json:"autoCreateShadowTopicFilters,omitempty"`
	// Additional topic properties to shadow
	// Partition count, `max.message.bytes`, `cleanup.policy` and
	// `timestamp.type` will always be replicated
	ShadowedTopicProperties []string `json:"shadowedTopicProperties,omitempty"`
}

// Options for syncing consumer offsets
type ShadowLinkConsumerOffsetSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval metav1.Duration `json:"interval,omitempty"`
	// Whether it's enabled
	Enabled bool `json:"enabled,omitempty"`
	// The filters
	GroupFilters []NameFilter `json:"groupFilters,omitempty"`
}

// Options for syncing security settings
type ShadowLinkSecuritySettingsSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default="30s"
	Interval metav1.Duration `json:"interval,omitempty"`
	// Whether or not it's enabled
	Enabled bool `json:"enabled,omitempty"`
	// Role filters
	RoleFilters []NameFilter `json:"roleFilters,omitempty"`
	// SCRAM credential filters
	ScramCredentialFilters []NameFilter `json:"scramCredFilters,omitempty"`
	// ACL filters
	ACLFilters []ACLFilter `json:"aclFilters,omitempty"`
}
