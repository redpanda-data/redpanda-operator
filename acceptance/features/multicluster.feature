@operator:none
Feature: Multicluster Operator

  @skip:gke @skip:aks @skip:eks @skip:k3d
  Scenario: Multicluster finalizers
    Given I create a multicluster operator named "multicluster" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "multicluster":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: default
    """
    Then in "multicluster" the Kubernetes object "cluster" in namespace "default" of type "StretchCluster.v1alpha2.cluster.redpanda.com" should have finalizer "operator.redpanda.com/finalizer"
