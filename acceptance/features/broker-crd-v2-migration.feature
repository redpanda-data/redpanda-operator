@serial
Feature: Broker CRD migration from StatefulSet (V2 Redpanda)
  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate V2 Redpanda from StatefulSet to Broker CRs, scale, and roll back
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: broker-v2-migrate
      spec:
        clusterSpec:
          statefulset:
            replicas: 3
          # The harpoon k3d cluster defaults not-ready/unreachable tolerations
          # to 10s (pkg/k3d) so node-failure tests evict fast. These scenarios
          # assert pod-UID stability — a transient agent-node blip must NOT
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
    And cluster "broker-v2-migrate" is stable with 3 nodes
    And a StatefulSet should exist for cluster "broker-v2-migrate"
    And I snapshot pod UIDs for cluster "broker-v2-migrate"
    # Trigger migration
    When I set annotation "operator.redpanda.com/use-broker-cr" to "true" on Redpanda "broker-v2-migrate"
    Then cluster "broker-v2-migrate" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-v2-migrate"
    And all Broker CRs for cluster "broker-v2-migrate" should be Running
    And cluster "broker-v2-migrate" admin API should show 3 brokers
    And pods for cluster "broker-v2-migrate" should have the same UIDs as the snapshot
    # Scale up without a StatefulSet
    When I scale "broker-v2-migrate" to 4 nodes
    Then cluster "broker-v2-migrate" should be stable with 4 nodes
    And cluster "broker-v2-migrate" should have 4 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-v2-migrate"
    And cluster "broker-v2-migrate" admin API should show 4 brokers
    # Scale back down: the excess broker decommissions via its CR intent
    When I scale "broker-v2-migrate" to 3 nodes
    Then cluster "broker-v2-migrate" should eventually have 3 Broker CRs
    And cluster "broker-v2-migrate" admin API should show 3 brokers
    And pods for cluster "broker-v2-migrate" should have the same UIDs as the snapshot
    And pods for cluster "broker-v2-migrate" should have no container restarts
    # Rollback
    When I remove annotation "operator.redpanda.com/use-broker-cr" from Redpanda "broker-v2-migrate"
    Then a StatefulSet should eventually exist for cluster "broker-v2-migrate"
    And cluster "broker-v2-migrate" should eventually have 0 Broker CRs
    And cluster "broker-v2-migrate" admin API should show 3 brokers
    And pods for cluster "broker-v2-migrate" should have the same UIDs as the snapshot
    And pods for cluster "broker-v2-migrate" should have no container restarts
    # Re-migrate: opting back in after a rollback must adopt the restored
    # pods in place again — promptly, not after waiting out a periodic
    # requeue — leaving the original pods untouched through the whole
    # migrate -> rollback -> migrate cycle.
    When I set annotation "operator.redpanda.com/use-broker-cr" to "true" on Redpanda "broker-v2-migrate"
    Then cluster "broker-v2-migrate" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-v2-migrate"
    And all Broker CRs for cluster "broker-v2-migrate" should be Running
    And cluster "broker-v2-migrate" admin API should show 3 brokers
    And pods for cluster "broker-v2-migrate" should have the same UIDs as the snapshot
    And pods for cluster "broker-v2-migrate" should have no container restarts
