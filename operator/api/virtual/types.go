// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package virtual

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// ShadowLink is a shadow link that exists in a cluster
type ShadowLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowLinkSpec   `json:"spec,omitempty"`
	Status ShadowLinkStatus `json:"status,omitempty"`
}

// GetCluster returns the name of the undelrying cluster.
func (s *ShadowLink) GetCluster() string {
	return s.Spec.ClusterRef.Name
}

type TopicMetadataSyncOffset string

const (
	TopicMetadataSyncOffsetEarliest  TopicMetadataSyncOffset = "earliest"
	TopicMetadataSyncOffsetLatest    TopicMetadataSyncOffset = "latest"
	TopicMetadataSyncOffsetTimestamp TopicMetadataSyncOffset = "timestamp"
)

// PatternType specifies the type of pattern applied for ACL resource matching.
type PatternType string

const (
	PatternTypeUnknown  PatternType = ""
	PatternTypeLiteral  PatternType = "literal"
	PatternTypePrefixed PatternType = "prefixed"
	PatternTypeMatch    PatternType = "match"
)

// FilterType specifies the type, either include or exclude of a consumer group filter.
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
	Name string `json:"name,omitempty"`
	// Valid values:
	// - include
	// - exclude
	FilterType FilterType `json:"filterType"`
	// Default value is literal. Valid values:
	// - literal
	// - prefixed
	PatternType PatternType `json:"patternType,omitempty"`
}

// Options for syncing topic metadata
// +kubebuilder:object:generate=true
type ShadowLinkTopicMetadataSyncOptions struct {
	// How often to sync metadata
	// If 0 provided, defaults to 30 seconds
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

// ShadowLinkSpec defines the desired state of a shadow link.
// +kubebuilder:object:generate=true
type ShadowLinkSpec struct {
	ClusterRef               ClusterRef                         `json:"clusterRef"`
	TopicMetadataSyncOptions ShadowLinkTopicMetadataSyncOptions `json:"topicMetadataSyncOptions"`
}

// ClusterRef is a reference to the cluster where this resource
// exists.
type ClusterRef struct {
	Name string `json:"name"`
}

// ShadowLinkStatus is the status for a shadow link.
type ShadowLinkStatus struct{}

// +kubebuilder:object:root=true

// ShadowLinkList is a list of ShadowLink objects.
type ShadowLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShadowLink `json:"items"`
}

// GetItems returns the underlying ShadowLink items.
func (s *ShadowLinkList) GetItems() []ShadowLink {
	return s.Items
}

// SetItems sets the underlying ShadowLink items.
func (s *ShadowLinkList) SetItems(items []ShadowLink) {
	s.Items = items
}
