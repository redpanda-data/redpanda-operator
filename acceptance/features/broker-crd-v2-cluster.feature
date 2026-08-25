@serial
Feature: Broker CRD with V2 Redpanda
  @skip:gke @skip:aks @skip:eks
  Scenario: Redpanda with broker-cr annotation creates Broker CRs instead of a StatefulSet
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: broker-v2
        annotations:
          operator.redpanda.com/use-broker-cr: "true"
      spec:
        clusterSpec:
          statefulset:
            replicas: 3
          # The harpoon k3d cluster defaults not-ready/unreachable tolerations
          # to 10s (pkg/k3d) so node-failure tests evict fast. These scenarios
          # assert roll serialization — a transient agent-node blip must NOT
          # evict brokers mid-scenario.
          tolerations:
            - key: node.kubernetes.io/not-ready
              operator: Exists
              effect: NoExecute
              tolerationSeconds: 300
            - key: node.kubernetes.io/unreachable
              operator: Exists
              effect: NoExecute
              tolerationSeconds: 300
      """
    And cluster "broker-v2" is stable with 3 nodes
    Then cluster "broker-v2" should have 3 Broker CRs
    And no StatefulSet should exist for cluster "broker-v2"
    And all Broker CRs for cluster "broker-v2" should be Running
    And all Broker CRs for cluster "broker-v2" should have conditions Ready, PodScheduled, BrokerRegistered, StorageBound as True
    And cluster "broker-v2" admin API should show 3 brokers
    # A node-config change bumps the config checksum; the cluster controller
    # must serialize the resulting pod rotations via roll-grants.
    When I snapshot pod UIDs for cluster "broker-v2"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: broker-v2
        annotations:
          operator.redpanda.com/use-broker-cr: "true"
      spec:
        clusterSpec:
          statefulset:
            replicas: 3
          tolerations:
            - key: node.kubernetes.io/not-ready
              operator: Exists
              effect: NoExecute
              tolerationSeconds: 300
            - key: node.kubernetes.io/unreachable
              operator: Exists
              effect: NoExecute
              tolerationSeconds: 300
          config:
            node:
              crash_loop_limit: 10
      """
    Then pods for cluster "broker-v2" should roll one at a time
    And all Broker CRs for cluster "broker-v2" should be Running
    And cluster "broker-v2" admin API should show 3 brokers
    # Condemn a specific broker by setting spec.decommission on its Broker
    # CR: the operator decommissions it and replaces it in place — same pod
    # name, fresh node identity. A StatefulSet can only ever remove the
    # highest ordinal; with Broker CRs any broker can be replaced.
    When I set decommission on the Broker with index 0 of cluster "broker-v2"
    Then the Broker with index 0 of cluster "broker-v2" should be replaced
    And cluster "broker-v2" should eventually have 3 Broker CRs
    And all Broker CRs for cluster "broker-v2" should be Running
    And cluster "broker-v2" admin API should show 3 brokers
