@cluster:basic:sasl @variant:vectorized
Feature: ShadowLink CRDs
  Background: Cluster available
    Given cluster "basic" is available
    And cluster "sasl" is available
    And I enable feature "enable_shadow_linking" on cluster "basic"
    And I enable feature "enable_shadow_linking" on cluster "sasl"
    # enable trace logging on the target cluster so we can debug a bit easier
    And I enable "trace" logging for the "cluster_link" logger on cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage ShadowLink
    Given there is no topic "topic1" in cluster "basic"
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
          name: basic
      partitions: 1
      replicationFactor: 1
    """
    And topic "topic1" is successfully synced
    When I apply Kubernetes manifest:
    """
    ---
# tag::basic-shadowlink-example[]
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: ShadowLink
    metadata:
      name: link
    spec:
      shadowCluster:
        clusterRef:
          name: sasl
      sourceCluster:
        clusterRef:
          name: basic
      topicMetadataSyncOptions:
        interval: 2s
        autoCreateShadowTopicFilters:
          - name: topic1
            filterType: include
            patternType: literal
# end::basic-shadowlink-example[]
    """
    And shadow link "link" is successfully synced
    Then I should find topic "topic1" in cluster "sasl"
