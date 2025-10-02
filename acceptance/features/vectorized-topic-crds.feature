@cluster:vectorized/basic
Feature: Vectorized Topic CRDs
  Background: Vectorized Cluster available
    Given vectorized cluster "basic" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage vectorized topics
    Given there is no topic "topic1" in vectorized cluster "basic"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Topic
    metadata:
      name: topic1
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: basic
      partitions: 1
      replicationFactor: 1
    """
    And topic "topic1" is successfully synced
    Then I should be able to produce and consume from "topic1" in vectorized cluster "basic"
