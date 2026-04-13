@multicluster
Feature: Ghost Node Ejection on StretchCluster

  @skip:gke @skip:aks @skip:eks
  Scenario: Auto-decommission ghost broker after pod recreation with PVC deletion
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
      external:
        enabled: false
      rbac:
        enabled: true
      config:
        cluster:
          partition_autobalancing_mode: continuous
          partition_autobalancing_node_autodecommission_timeout_sec: 120
          partition_autobalancing_node_availability_timeout_sec: 60
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
        repository: redpandadata/redpanda-unstable
        tag: v26.1.1-rc5
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
    And all clusters should report exactly 3 brokers
    And the StretchCluster "cluster" in namespace "default" in "multicluster" should have condition "ConfigurationApplied" with status "True" within 120 seconds

    # Delete the pod and its PVC in the second cluster to force a new broker ID on restart
    When I delete the Redpanda pod and its PVC in cluster "second" of "multicluster"
    And the pod in cluster "second" of "multicluster" is eventually running and ready

    # Wait for Redpanda auto-decommission to remove the ghost broker
    Then Redpanda should auto-decommission the ghost broker within 400 seconds

    # Verify cluster recovered with 3 alive brokers
    When I execute "rpk redpanda admin brokers list" command in the statefulset container in each cluster
    And all clusters should report exactly 3 brokers
    And all brokers should be alive and active
    And all clusters in "multicluster" should report StretchClusters as healthy
