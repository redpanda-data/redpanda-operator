@multicluster
@serial
Feature: StretchCluster ghost node ejection

  When a node in a stretch cluster becomes permanently unreachable, Redpanda's
  continuous data balancing should automatically decommission (eject) the ghost
  node after the configured timeouts elapse.

  The partition balancer's default timeouts are measured in hours, so we scale
  all the interrelated tunables together per Redpanda's validators.cc rules —
  scaling one without the others breaks implicit assumptions. The ejection
  timer runs for autodecommission_timeout seconds from the moment a quorum of
  alive brokers agrees the node has been missing too long, so total expected
  ejection time is ~90 seconds after the outage begins.

  @skip:gke @skip:aks @skip:eks
  Scenario: Ghost node is ejected after a regional outage
    Given I create a multicluster operator named "ghost" with 3 nodes
    And I apply a multicluster Kubernetes manifest to "ghost":
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
          partition_autobalancing_node_availability_timeout_sec: 45
          partition_autobalancing_node_autodecommission_timeout_sec: 90
          partition_autobalancing_tick_interval_ms: 10000
          health_monitor_tick_interval: 5000
          node_status_interval: 5000
    """
    And I apply a NodePool Kubernetes manifest to "ghost":
    """
    spec:
      clusterRef:
        group: cluster.redpanda.com
        kind: StretchCluster
        name: cluster
      replicas: 1
      image:
        repository: redpandadata/redpanda
        tag: v26.1.5
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
      services:
        perPod:
          remote:
            enabled: false
    """
    And I expect 3 statefulsets in 3 kubernetes cluster to be created and eventually ready
    And I expect all 3 NodePools in "ghost" to be eventually bound and deployed
    # Verify the cluster starts healthy with all 3 nodes.
    When I execute "rpk cluster health" command in the statefulset container in each cluster
    Then the cluster health output should show 3 nodes across all clusters in "ghost"
    # Simulate a regional outage by taking a non-controller region offline. We
    # avoid the controller region so the remaining cluster retains a stable
    # controller for the decommission decision.
    When I take a non-controller region of "ghost" offline
    # Wait for the ghost node to be ejected. With our config this takes ~90s
    # (partition_autobalancing_node_autodecommission_timeout_sec).
    Then the cluster health output should eventually show 2 nodes in the remaining clusters of "ghost"
