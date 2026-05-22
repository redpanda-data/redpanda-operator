@multicluster
Feature: Stretch Cluster Rolling Restart

  @skip:gke @skip:aks @skip:eks
  Scenario: Rolling upgrade preserves sentinel data and keeps cluster available
    Given I create a multicluster operator named "rolling" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "rolling":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: default
    spec:
      external:
        enabled: false
      rbac:
        enabled: true
    """
    And I apply a NodePool Kubernetes manifest to "rolling":
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
    And I expect all 3 NodePools in "rolling" to be eventually bound and deployed
    And I create a sentinel topic in the stretch cluster of "rolling"
    When I upgrade the NodePools in "rolling" to use image "redpandadata/redpanda:v25.2.11"
    Then the upgrade of "rolling" completes with at most 1 pod unavailable at a time
    And the sentinel data is still readable in "rolling"
