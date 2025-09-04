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
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
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
	metav1.ObjectMeta `json:"metadata"`

	Spec   ShadowLinkSpec   `json:"spec"`
	Status ShadowLinkStatus `json:"status"`
}

func (n *ShadowLink) GetClusterSource() *ClusterSource {
	return &n.Spec.SourceCluster
}

func (n *ShadowLink) GetRemoteClusterSource() *ClusterSource {
	return &n.Spec.DestinationCluster
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

// ShadowLinkStatus defines the observed state of any node pools tied to this cluster
type ShadowLinkStatus struct {
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

func ShadowLinkTaskStatusesToConfigs(existing, updated []ShadowLinkTaskStatus) []*redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration {
	now := metav1.Now()
	tasks := []*redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration{}

	findStatus := func(status ShadowLinkTaskStatus) *ShadowLinkTaskStatus {
		for _, o := range existing {
			if o.Name == status.Name {
				return &o
			}
		}
		return nil
	}

	for _, task := range updated {
		existingTask := findStatus(task)
		if existingTask == nil {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.State != task.State {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.Reason != task.Reason {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.BrokerID != task.BrokerID {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		tasks = append(tasks, shadowLinkTaskStatusToConfig(existingTask.LastTransitionTime, *existingTask))
	}

	return tasks
}

func shadowLinkTaskStatusToConfig(now metav1.Time, task ShadowLinkTaskStatus) *redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration {
	return redpandav1alpha2ac.ShadowLinkTaskStatus().
		WithName(task.Name).
		WithState(task.State).
		WithReason(task.Reason).
		WithBrokerID(task.BrokerID).
		WithLastTransitionTime(now)
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
	// List of partition information for the shadow topic
	PartitionInformation []TopicPartitionInformation `json:"partitionInformation,omitempty"`
}

func ShadowTopicStatusesToConfigs(existing, updated []ShadowTopicStatus) []*redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration {
	now := metav1.Now()
	topics := []*redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration{}

	findStatus := func(status ShadowTopicStatus) *ShadowTopicStatus {
		for _, o := range existing {
			if o.Name == status.Name && o.TopicID == status.TopicID {
				return &o
			}
		}
		return nil
	}

OUTER:
	for _, topic := range updated {
		existingTopic := findStatus(topic)
		if existingTopic == nil {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		if existingTopic.State != topic.State {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		if len(existingTopic.PartitionInformation) != len(topic.PartitionInformation) {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		for i, updatedPartition := range topic.PartitionInformation {
			if !reflect.DeepEqual(updatedPartition, existingTopic.PartitionInformation[i]) {
				continue OUTER
			}
		}

		topics = append(topics, shadowTopicStatusToConfig(existingTopic.LastTransitionTime, *existingTopic))
	}

	return topics
}

func shadowTopicStatusToConfig(now metav1.Time, topic ShadowTopicStatus) *redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration {
	return redpandav1alpha2ac.ShadowTopicStatus().
		WithName(topic.Name).
		WithTopicID(topic.TopicID).
		WithState(topic.State).
		WithPartitionInformation(functional.MapFn(func(info TopicPartitionInformation) *redpandav1alpha2ac.TopicPartitionInformationApplyConfiguration {
			return redpandav1alpha2ac.TopicPartitionInformation().
				WithPartitionID(info.PartitionID).
				WithSourceLastStableOffset(info.SourceLastStableOffset).
				WithSourceHighWatermark(info.SourceHighWatermark).
				WithHighWatermark(info.HighWatermark)
		}, topic.PartitionInformation)...).
		WithLastTransitionTime(now)
}

// Topic partition information
type TopicPartitionInformation struct {
	// Partition ID
	PartitionID int64 `json:"partitionId,omitempty"`
	// Source partition's LSO
	SourceLastStableOffset int64 `json:"sourceLastStableOffset,omitempty"`
	// Source partition's HWM
	SourceHighWatermark int64 `json:"sourceHighWatermark,omitempty"`
	// Shadowed partition's HWM
	HighWatermark int64 `json:"highWatermark,omitempty"`
}

type ShadowLinkSpec struct {
	SourceCluster      ClusterSource `json:"sourceCluster"`
	DestinationCluster ClusterSource `json:"destinationCluster"`

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
	PatternType *PatternType `json:"patternType,omitempty"`
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
	// +kubebuilder:default=30
	Interval time.Duration `json:"interval,omitempty"`
	// The topic filters to use
	TopicFilters []NameFilter `json:"topicFilters,omitempty"`
	// Additional topic properties to shadow
	// Partition count, `max.message.bytes`, `cleanup.policy` and
	// `timestamp.type` will always be replicated
	ShadowedTopicProperties []string `json:"shadowedTopicProperties,omitempty"`
}

// Options for syncing consumer offsets
type ShadowLinkConsumerOffsetSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default=30
	Interval time.Duration `json:"interval,omitempty"`
	// Whether it's enabled
	Enabled bool `json:"enabled,omitempty"`
	// The filters
	GroupFilters []NameFilter `json:"groupFilters,omitempty"`
}

// Options for syncing security settings
type ShadowLinkSecuritySettingsSyncOptions struct {
	// Sync interval
	// If 0 provided, defaults to 30 seconds
	// +kubebuilder:default=30
	Interval time.Duration `json:"interval,omitempty"`
	// Whether or not it's enabled
	Enabled bool `json:"enabled,omitempty"`
	// Role filters
	RoleFilters []NameFilter `json:"roleFilters,omitempty"`
	// SCRAM credential filters
	ScramCredentialFilters []NameFilter `json:"scramCredFilters,omitempty"`
	// ACL filters
	ACLFilters []*ACLFilter `json:"aclFilters,omitempty"`
}
