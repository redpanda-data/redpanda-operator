@cluster:basic
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