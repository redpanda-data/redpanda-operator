@multicluster
Feature: Multicluster Operator

  @skip:gke @skip:aks @skip:eks
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
    spec:
      rbac:
        enabled: true
    """
    Then in "multicluster" the Kubernetes object "cluster" in namespace "default" of type "StretchCluster.v1alpha2.cluster.redpanda.com" should have finalizer "operator.redpanda.com/finalizer"
    And I apply a NodePool Kubernetes manifest to "multicluster":
    """
    spec:
      clusterRef:
        group: cluster.redpanda.com
        kind: StretchCluster
        name: cluster
      replicas: 1
      image:
        repository: redpandadata/redpanda
        tag: v25.2.1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
      services:
        perPod:
          remote:
            enabled: false
    """
    And I expect 3 statefulsets in 3 kubernetes cluster to be created and eventually ready
    And I expect all 3 NodePools in "multicluster" to be eventually bound and deployed
    When I execute "rpk redpanda admin brokers list" command in the statefulset container in each cluster
    And I expect them to return the same Redpanda broker list
