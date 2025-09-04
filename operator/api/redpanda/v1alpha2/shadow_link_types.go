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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// ShadowLink defines the CRD for ShadowLink cluster configuration.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=shadowlinks
// +kubebuilder:resource:shortName=sl
type ShadowLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowLinkSpec   `json:"spec,omitempty"`
	Status ShadowLinkStatus `json:"status,omitempty"`
}

func (n *ShadowLink) GetClusterSource() *ClusterSource {
	return &n.Spec.SourceCluster
}

func (n *ShadowLink) GetRemoteClusterSource() *ClusterSource {
	return &n.Spec.DestinationCluster
}

// ShadowLinkStatus defines the observed state of any node pools tied to this cluster
type ShadowLinkStatus struct {
	// Conditions holds the conditions for the ShadowLink.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
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

// +kubebuilder:validation:Enum=destination;source
type LinkMode string

var (
	LinkModeUnknown      LinkMode = ""
	LinkModeDestionation LinkMode = "destination"
	LinkModeSource       LinkMode = "source"
)

// FilterType specifies the type, either include or exclude of a consumer group filter.
// +kubebuilder:validation:Enum=include;exclude
type FilterType string

var (
	FilterTypeUnknown FilterType = ""
	FilterTypeInclude FilterType = "include"
	FilterTypeExclude FilterType = "exclude"
)

type ConsumerGroupFilter struct {
	// +kubebuilder:default=*
	Name string `json:"name,optional"`
	// Valid values:
	// - include
	// - exclude
	FilterType FilterType `json:"filterType"`
	// Default value is literal. Valid values:
	// - literal
	// - prefixed
	//
	// +kubebuilder:default=literal
	PatternType *PatternType `json:"patternType,omitempty"`
}

type ACLAccessFilter struct {
	// +kubebuilder:default=*
	Host           string        `json:"host"`
	Operation      *ACLOperation `json:"operation,omitempty"`
	PermissionType *ACLType      `json:"permissionType,omitempty"`
	// +kubebuilder:default=*
	Principal string `json:"principal"`
}

type ACLResourceFilter struct {
	// +kubebuilder:default=*
	Name         string        `json:"name"`
	PatternType  *PatternType  `json:"patternType,omitempty"`
	ResourceType *ResourceType `json:"resourceType,omitempty"`
}

type ACLFilter struct {
	AccessFilter   ACLAccessFilter   `json:"accessFilter"`
	ResourceFilter ACLResourceFilter `json:"resourceFilter"`
}

// +kubebuilder:validation:Enum=pause;promote;failover;active
type MirrorTopicState string

var (
	MirrorTopicStateUnknown  MirrorTopicState = ""
	MirrorTopicStatePause    MirrorTopicState = "pause"
	MirrorTopicStatePromote  MirrorTopicState = "promote"
	MirrorTopicStateFailover MirrorTopicState = "failover"
	MirrorTopicStateActive   MirrorTopicState = "active"
)

type MirrorTopic struct {
	Name string `json:"name"`
	// +kubebuilder:default=active
	State             MirrorTopicState      `json:"state,omitempty"`
	Configs           *runtime.RawExtension `json:"configs,omitempty"`
	ReplicationFactor *int32                `json:"replicationFactor,omitempty"`
	SourceTopicName   *string               `json:"sourceTopicName,omitempty"`
}

type TopicFilter struct {
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
	PatternType *PatternType `json:"patternType,omitempty"`
}

type AutoCreateTopics struct {
	// +kubebuilder:default=false
	Enabled      bool          `json:"enabled,omitempty"`
	TopicFilters []TopicFilter `json:"topicFilters,omitempty"`
}

type MirrorTopicOptions struct {
	AutoCreateTopics AutoCreateTopics `json:"autoCreateTopics"`
	Prefix           *string          `json:"prefix,omitempty"`
}

type ShadowLinkSpec struct {
	// +kubebuilder:default=source
	LinkMode             LinkMode              `json:"linkMode"`
	SourceCluster        ClusterSource         `json:"sourceCluster"`
	DestinationCluster   ClusterSource         `json:"destinationCluster"`
	ConsumerGroupFilters []ConsumerGroupFilter `json:"consumerGroupFilters,omitempty"`
	ACLFilter            []ACLFilter           `json:"aclFilters,omitempty"`
	Configs              *runtime.RawExtension `json:"configs,omitempty"`
	MirrorTopics         []MirrorTopic         `json:"mirrorTopics,omitempty"`
	MirrorTopicOptions   MirrorTopicOptions    `json:"mirrorTopicOptions"`
}
