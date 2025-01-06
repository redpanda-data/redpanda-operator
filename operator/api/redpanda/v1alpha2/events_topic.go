// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

// Defines valid event severity values.
const (
	// Indicates an error when topic creation
	// was not successful.
	EventTopicCreationFailure string = "topicCreationFailure"
	// Indicates an error when topic deletion
	// was not successful.
	EventTopicDeletionFailure string = "topicDeletionFailure"
	// Indicates an error when topic configuration altering
	// was not successful.
	EventTopicConfigurationAlteringFailure string = "topicConfigurationAlteringFailure"
	// Indicates an error when topic configuration describe
	// was not successful.
	EventTopicConfigurationDescribeFailure string = "topicConfigurationDescribeFailure"
	// Indicates that the topic is already synced.
	EventTopicAlreadySynced string = "topicAlreadySynced"
	// Indicates that a topic is synced.
	EventTopicSynced string = "topicSynced"
)
