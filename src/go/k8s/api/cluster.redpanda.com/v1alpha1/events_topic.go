package v1alpha1

// Defines valid event severity values.
const (
	// Indicate an error when topic creation
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
