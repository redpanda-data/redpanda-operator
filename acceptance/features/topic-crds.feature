@cluster:basic @variant:vectorized
Feature: Topic CRDs
  Background: Cluster available
    Given cluster "basic" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage topics
    Given there is no topic "topic1" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::basic-topic-example[]
    # In this example manifest, a topic called "topic1" is created in a cluster called "basic". It has a replication factor of 1 and is distributed across a single partition.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Topic
    metadata:
      name: topic1
    spec:
      cluster:
        clusterRef:
          name: basic
      partitions: 1
      replicationFactor: 1
# end::basic-topic-example[]
    """
    And topic "topic1" is successfully synced
    Then I should be able to produce and consume from "topic1" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage topic with write caching
    Given there is no topic "chat-room" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::write-caching-topic-example[]
    # This manifest creates a topic called "chat-room" with write caching enabled.
    # Write caching provides better performance at the expense of durability.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Topic
    metadata:
      name: chat-room
    spec:
      cluster:
        clusterRef:
          name: basic
      partitions: 3
      replicationFactor: 1
      additionalConfig:
        write.caching: "true"
# end::write-caching-topic-example[]
    """
    And topic "chat-room" is successfully synced
    Then I should be able to produce and consume from "chat-room" in cluster "basic"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage topic with cleanup policy
    Given there is no topic "delete-policy-topic" in cluster "basic"
    When I apply Kubernetes manifest:
    """
# tag::cleanup-policy-topic-example[]
    # This manifest creates a topic with the cleanup policy set to "delete".
    # The cleanup policy determines how partition log files are managed when they reach a certain size.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Topic
    metadata:
      name: delete-policy-topic
    spec:
      cluster:
        clusterRef:
          name: basic
      partitions: 3
      replicationFactor: 1
      additionalConfig:
        cleanup.policy: "delete"
# end::cleanup-policy-topic-example[]
    """
    And topic "delete-policy-topic" is successfully synced
    Then I should be able to produce and consume from "delete-policy-topic" in cluster "basic"
