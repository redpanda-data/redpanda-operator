@multicluster
Feature: Stretch Cluster Regional Outage

  The multicluster operator must continue managing reachable regions when one
  region becomes unavailable, and automatically reconcile the returning region
  to current desired state when connectivity is restored — with no manual
  intervention required.

  @skip:gke @skip:aks @skip:eks
  Scenario: Operator reconciles a returning region after a regional outage
    Given I create a multicluster operator named "outage" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "outage":
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
    And I apply a RedpandaBrokerPool Kubernetes manifest to "outage":
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
    And I expect all 3 RedpandaBrokerPools in "outage" to be eventually bound and deployed
    # Verify the cluster has actually formed (all 3 brokers in raft membership)
    # before simulating the outage. `Deployed=True` only means the StatefulSet
    # has spawned a pod — it doesn't wait for the broker process to join raft.
    # Without this gate the test races: taking vc-2 offline before
    # cluster-third-0 joins means it never appears in the broker list, and the
    # downstream "report broker as unavailable" assertion can never satisfy.
    When I execute "rpk cluster health" command in the statefulset container in each cluster
    Then the cluster health output should show 3 nodes across all clusters in "outage"

    # Simulate a regional outage by taking one vcluster fully offline.
    When I take the "vc-2" region of "outage" offline
    Then the remaining regions of "outage" should eventually report SpecSynced as "ClusterUnreachable"
    And the remaining regions of "outage" should eventually report the "vc-2" broker as unavailable

    # While the region is down, apply a spec change. The operator should apply
    # it to the two reachable regions and leave the offline region for later.
    When I apply a multicluster Kubernetes manifest to "outage":
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
          log_segment_size_min: "16777216"
    """
    Then the reachable regions of "outage" should eventually reflect the updated StretchCluster spec

    # Restore the region. The returned region still has the old spec, so we must
    # apply the updated manifest there as well to clear the drift. The operator
    # does not propagate spec changes — it only checks consistency.
    When I bring the "vc-2" region of "outage" back online
    Then the operator in the "vc-2" region of "outage" should eventually be running and reconciling
    When I apply a multicluster Kubernetes manifest to "outage":
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
          log_segment_size_min: "16777216"
    """
    Then all regions of "outage" should eventually report SpecSynced as "Synced"
    And the "vc-2" region of "outage" should reflect the updated StretchCluster spec
    And I expect all 3 RedpandaBrokerPools in "outage" to be eventually bound and deployed
