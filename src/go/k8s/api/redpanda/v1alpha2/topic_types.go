// Copyright 2021-2023 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TopicSpec defines the desired state of the topic. See https://docs.redpanda.com/current/manage/kubernetes/manage-topics/.
type TopicSpec struct {
	// Specifies the number of topic shards that are distributed across the brokers in a cluster.
	// This number cannot be decreased after topic creation.
	// It can be increased after topic creation, but it is
	// important to understand the consequences that has, especially for
	// topics with semantic partitioning. When absent this will default to
	// the Redpanda cluster configuration `default_topic_partitions`.
	// See https://docs.redpanda.com/docs/reference/cluster-properties/#default_topic_partitions and
	// https://docs.redpanda.com/docs/get-started/architecture/#partitions
	Partitions *int `json:"partitions,omitempty"`
	// Specifies the number of replicas the topic should have. Must be odd value.
	// When absent this will default to the Redpanda cluster configuration `default_topic_replications`.
	// See https://docs.redpanda.com/docs/reference/cluster-properties/#default_topic_replications.
	ReplicationFactor *int `json:"replicationFactor,omitempty"`
	// Changes the topic name from the value of `metadata.name`.
	OverwriteTopicName *string `json:"overwriteTopicName,omitempty"`
	// Adds extra topic configurations. This is a free-form map of any configuration options that topics can have.
	// Examples:
	// `cleanup.policy=compact`
	// `redpanda.remote.write=true`
	// `redpanda.remote.read=true`
	// `redpanda.remote.recovery=true`
	// `redpanda.remote.delete=true`
	AdditionalConfig map[string]*string `json:"additionalConfig,omitempty"`

	// Defines client configuration for connecting to Redpanda brokers.
	KafkaAPISpec *KafkaAPISpec `json:"kafkaApiSpec,omitempty"`

	// Overwrites the fully-qualified
	// name of the metric. This should be easier to identify if
	// multiple operator instances runs inside the same Kubernetes cluster.
	// By default, it is set to `redpanda-operator`.
	MetricsNamespace *string `json:"metricsNamespace,omitempty"`

	// Defines when the topic controller will schedule the next reconciliation.
	// Default is 3 seconds.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:default="3s"
	SynchronizationInterval *metav1.Duration `json:"interval,omitempty"`
}

// TopicStatus defines the observed state of the Topic resource.
type TopicStatus struct {
	// ObservedGeneration is the last observed generation of the Topic.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Topic.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TopicConfiguration is the last snapshot of the topic configuration during successful reconciliation.
	TopicConfiguration []Configuration `json:"topicConfiguration,omitempty"`
}

// Configuration was copied from https://github.com/twmb/franz-go/blob/01651affd204d4a3577a341e748c5d09b52587f8/pkg/kmsg/generated.go#L24593-L24634
type Configuration struct {
	// Name is a key this entry corresponds to (e.g. segment.bytes).
	Name string `json:"name"`

	// Value is the value for this config key. If the key is sensitive,
	// the value will be null.
	Value *string `json:"value,omitempty"`

	// ReadOnly signifies whether this is not a dynamic config option.
	//
	// Note that this field is not always correct, and you may need to check
	// whether the Source is any dynamic enum. See franz-go#91 for more details.
	ReadOnly bool `json:"readOnly"`

	// IsDefault is whether this is a default config option. This has been
	// replaced in favor of Source.
	IsDefault bool `json:"isDefault"`

	// Source is where this config entry is from.
	//
	// This field has a default of -1.
	Source string `json:"source"`

	// IsSensitive signifies whether this is a sensitive config key, which
	// is either a password or an unknown type.
	IsSensitive bool `json:"isSensitive"`

	// ConfigSynonyms contains fallback key/value pairs for this config
	// entry, in order of preference. That is, if a config entry is both
	// dynamically configured and has a default, the top level return will be
	// the dynamic configuration, while its "synonym" will be the default.
	ConfigSynonyms []ConfigSynonyms `json:"configSynonyms,omitempty"`

	// ConfigType specifies the configuration data type.
	ConfigType string `json:"configType"`

	// Documentation is optional documentation for the config entry.
	Documentation *string `json:"documentation,omitempty"`

	// UnknownTags are tags Kafka sent that we do not know the purpose of.
	UnknownTags map[string]string `json:"unknownTags"`
}

// ConfigSynonyms was copied from https://github.com/twmb/franz-go/blob/01651affd204d4a3577a341e748c5d09b52587f8/pkg/kmsg/generated.go#L24569-L24578
type ConfigSynonyms struct {
	Name string `json:"name"`

	Value *string `json:"value,omitempty"`

	Source string `json:"source"`

	// UnknownTags are tags Kafka sent that we do not know the purpose of.
	UnknownTags map[string]string `json:"unknownTags,omitempty"`
}

// Topic defines the CRD for Topic resources. See https://docs.redpanda.com/current/manage/kubernetes/manage-topics/.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
type Topic struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Defines the desired state of the Topic resource.
	Spec TopicSpec `json:"spec,omitempty"`
	// Represents the current status of the Topic resource.
	Status TopicStatus `json:"status,omitempty"`
}

var _ KafkaConnectedObjectWithMetrics = (*Topic)(nil)

func (t *Topic) GetKafkaAPISpec() *KafkaAPISpec {
	return t.Spec.KafkaAPISpec
}

func (t *Topic) GetMetricsNamespace() *string {
	return t.Spec.MetricsNamespace
}

//+kubebuilder:object:root=true

// TopicList contains a list of Topic objects.
type TopicList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Topic resources.
	Items []Topic `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Topic{}, &TopicList{})
}

func (t *Topic) GetTopicName() string {
	topicName := t.Name
	if t.Spec.OverwriteTopicName != nil && *t.Spec.OverwriteTopicName != "" {
		topicName = *t.Spec.OverwriteTopicName
	}
	return topicName
}

const (
	// ReadyCondition indicates the resource is ready and fully reconciled.
	// If the Condition is False, the resource SHOULD be considered to be in the process of reconciling and not a
	// representation of actual state.
	ReadyCondition = "Ready"
)

const (
	// ProgressingReason indicates a condition or event observed progression, for example when the reconciliation of a
	// resource or an action has started.
	//
	// When this reason is given, other conditions and types MAY no longer be considered as an up-to-date observation.
	// Producers of the specific condition type or event SHOULD provide more information about the expectations and
	// precise meaning in their API specification.
	//
	// More information about the reason or the current state of the progression MAY be available as additional metadata
	// in an attached message.
	ProgressingReason string = "Progressing"

	// SucceededReason indicates a condition or event observed a success, for example when declared desired state
	// matches actual state, or a performed action succeeded.
	//
	// More information about the reason of success MAY be available as additional metadata in an attached message.
	SucceededReason string = "Succeeded"

	// FailedReason indicates a condition or event observed a failure, for example when declared state does not match
	// actual state, or a performed action failed.
	//
	// More information about the reason of failure MAY be available as additional metadata in an attached message.
	FailedReason string = "Failed"
)

// TopicProgressing resets any failures and registers progress toward
// reconciling the given Topic by setting the meta.ReadyCondition to
// 'Unknown' for meta.ProgressingReason.
func TopicProgressing(topic *Topic) *Topic {
	return setCondition(ProgressingReason, "Topic reconciliation in progress", metav1.ConditionUnknown, topic)
}

// TopicReady resets any failures and registers ready condition
// the given Topic by setting the meta.ReadyCondition to
// 'Ready' for meta.SucceededReason.
func TopicReady(topic *Topic) *Topic {
	return setCondition(SucceededReason, "Topic reconciliation succeeded", metav1.ConditionTrue, topic)
}

// TopicFailed resets all conditions to failure the given Topic
// by setting the meta.ReadyCondition to 'Failed' for meta.FailedReason.
func TopicFailed(topic *Topic) *Topic {
	return setCondition(FailedReason, "Topic reconciliation failed", metav1.ConditionFalse, topic)
}

func setCondition(reason, message string, status metav1.ConditionStatus, topic *Topic) *Topic {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: topic.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	for i := range topic.Status.Conditions {
		if topic.Status.Conditions[i].Type == ReadyCondition {
			if topic.Status.Conditions[i].Status == status &&
				topic.Status.Conditions[i].Reason == reason {
				return topic
			}
			topic.Status.Conditions[i] = condition
			return topic
		}
	}

	topic.Status.Conditions = append(topic.Status.Conditions, condition)
	return topic
}
