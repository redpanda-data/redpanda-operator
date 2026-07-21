@serial
Feature: Broker CRD re-migration after rollback of a broker-born cluster
  # Regression: a cluster created directly in broker mode has pods built by
  # the Broker controller, which carry no controller-revision-hash label. A
  # rollback creates a StatefulSet that adopts those pods, so its
  # status.updatedReplicas never converges (OnDelete strategy never re-labels
  # or rolls them). Re-migration must not depend on that revision bookkeeping
  # or it blocks forever on "StatefulSet rollout has not completed".
  @skip:gke @skip:aks @skip:eks
  Scenario: Roll back a broker-born V1 Cluster to StatefulSets and migrate again
    Given I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: broker-remigrate
        annotations:
          operator.redpanda.com/migrate-to-broker-cr: "true"
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
    Then cluster "broker-remigrate" should have 3 Broker CRs
    And no StatefulSet should exist for cluster "broker-remigrate"
    And all Broker CRs for cluster "broker-remigrate" should be Running
    And cluster "broker-remigrate" admin API should show 3 brokers
    And I snapshot pod UIDs for cluster "broker-remigrate"
    # Rollback: the created StatefulSet adopts the broker-built pods.
    When I remove annotation "operator.redpanda.com/migrate-to-broker-cr" from V1 cluster "broker-remigrate"
    Then a StatefulSet should eventually exist for cluster "broker-remigrate"
    And cluster "broker-remigrate" should eventually have 0 Broker CRs
    And cluster "broker-remigrate" admin API should show 3 brokers
    And pods for cluster "broker-remigrate" should have the same UIDs as the snapshot
    # Re-migrate: used to wedge on "StatefulSet rollout has not completed".
    When I set annotation "operator.redpanda.com/migrate-to-broker-cr" to "true" on V1 cluster "broker-remigrate"
    Then cluster "broker-remigrate" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "broker-remigrate"
    And all Broker CRs for cluster "broker-remigrate" should be Running
    And cluster "broker-remigrate" admin API should show 3 brokers
    And pods for cluster "broker-remigrate" should have the same UIDs as the snapshot
