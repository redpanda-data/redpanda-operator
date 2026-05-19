@multicluster
Feature: Stretch Cluster Operator Upgrade

  Upgrade a stretch-cluster's multicluster operator one vcluster at a time,
  starting from the published v26.2.1-beta.2 chart on https://charts.redpanda.com
  and ending on the local dev chart (../operator/chart) with
  localhost/redpanda-operator:dev. The Redpanda data plane stays up throughout,
  and after each per-vcluster helm upgrade the operator raft quorum recovers
  with is_healthy=true AND a strictly advanced raft term — proving the new
  operator pod actually joined the quorum rather than being silently isolated.

  @skip:gke @skip:aks @skip:eks
  Scenario: Per-vcluster operator upgrade preserves raft quorum and sentinel data
    Given I create a multicluster operator named "op-upgrade" with 3 nodes using helm chart "redpanda/operator" version "v26.2.1-beta.2"
    And I apply a multicluster Kubernetes manifest to "op-upgrade":
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: StretchCluster
    metadata:
      name: cluster
      namespace: default
    spec: {}
    """
    And I apply a RedpandaBrokerPool Kubernetes manifest to "op-upgrade":
    """
    spec:
      rbac:
        enabled: true
      external:
        enabled: false
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
    And I expect all 3 RedpandaBrokerPools in "op-upgrade" to be eventually bound and deployed
    And I create a sentinel topic in the stretch cluster of "op-upgrade"
    And the multicluster operator raft quorum in "op-upgrade" is healthy
    When I upgrade the multicluster operator in "op-upgrade" to the local dev chart, one vcluster at a time
    Then the sentinel data is still readable in "op-upgrade"
