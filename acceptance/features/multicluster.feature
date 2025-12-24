@operator:none
Feature: Multicluster Operator

  Scenario: Multicluster reconciliation
    Given I create a multicluster operator with 3 nodes
    And I apply the multicluster Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: redpanda
    """
    Then I become debuggable
    Then the Kubernetes object "cluster" in namespace "redpanda" of type "StretchCluster.v1alpha2.cluster.redpanda.com" should have finalizer "operator.redpanda.com/finalizer"
