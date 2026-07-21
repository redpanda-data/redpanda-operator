@serial
Feature: Broker CRD migration from StatefulSet
  @skip:gke @skip:aks @skip:eks
  Scenario: Migrate V1 Cluster from StatefulSet to Broker CRs and roll back
    Given I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: broker-migrate
      spec:
        image: ${DEFAULT_REDPANDA_REPO}
        version: ${DEFAULT_REDPANDA_TAG}
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
        resources:
          requests:
            cpu: "100m"
            memory: 512Mi
          limits:
            cpu: "100m"
            memory: 512Mi
        configuration:
          rpcServer:
            port: 33145
          kafkaApi:
            - port: 9092
          adminApi:
            - port: 9644
          developerMode: true
          additionalCommandlineArguments:
            dump-memory-diagnostics-on-alloc-failure-kind: all
            abort-on-seastar-bad-alloc: ''
      """
    And cluster "broker-migrate" admin API should show 3 brokers
    And a StatefulSet should exist for cluster "broker-migrate"
    And I snapshot pod UIDs for cluster "broker-migrate"
    # Trigger migration
    When I set annotation "operator.redpanda.com/migrate-to-broker-cr" to "true" on V1 cluster "broker-migrate"
    Then cluster "broker-migrate" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-migrate"
    And all Broker CRs for cluster "broker-migrate" should be Running
    And cluster "broker-migrate" admin API should show 3 brokers
    And pods for cluster "broker-migrate" should have the same UIDs as the snapshot
    # Rollback
    When I remove annotation "operator.redpanda.com/migrate-to-broker-cr" from V1 cluster "broker-migrate"
    Then a StatefulSet should eventually exist for cluster "broker-migrate"
    And cluster "broker-migrate" should eventually have 0 Broker CRs
    And cluster "broker-migrate" admin API should show 3 brokers
    And pods for cluster "broker-migrate" should have the same UIDs as the snapshot
    # Re-migrate after rollback
    When I set annotation "operator.redpanda.com/migrate-to-broker-cr" to "true" on V1 cluster "broker-migrate"
    Then cluster "broker-migrate" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-migrate"
    And all Broker CRs for cluster "broker-migrate" should be Running
    And cluster "broker-migrate" admin API should show 3 brokers
    And pods for cluster "broker-migrate" should have the same UIDs as the snapshot
