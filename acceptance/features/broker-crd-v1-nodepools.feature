@serial
Feature: Broker CRD with V1 Cluster inlined nodePools
  @skip:gke @skip:aks @skip:eks
  Scenario: V1 Cluster with inlined nodePools creates Broker CRs per pool
    Given I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: rp-with-pools
        annotations:
          operator.redpanda.com/migrate-to-broker-cr: "true"
      spec:
        image: ${DEFAULT_REDPANDA_REPO}
        version: ${DEFAULT_REDPANDA_TAG}
        additionalConfiguration: {}
        podDisruptionBudget:
          enabled: true
          maxUnavailable: 1
        configuration:
          developerMode: true
          additionalCommandlineArguments:
            abort-on-seastar-bad-alloc: ""
            dump-memory-diagnostics-on-alloc-failure-kind: "all"
          rpcServer:
            port: 33145
          kafkaApi:
            - port: 9092
              tls:
                enabled: true
          adminApi:
            - port: 9644
          pandaproxyApi:
            - port: 8082
          schemaRegistryApi:
            - port: 8081
              name: "internal"
              authenticationMethod: http_basic
        nodePools:
          - name: blue-a
            replicas: 3
            # See the sibling features: override the harpoon k3d cluster's
            # aggressive 10s not-ready/unreachable eviction default — UID
            # stability is asserted here.
            tolerations:
              - key: node.kubernetes.io/not-ready
                operator: Exists
                effect: NoExecute
                tolerationSeconds: 300
              - key: node.kubernetes.io/unreachable
                operator: Exists
                effect: NoExecute
                tolerationSeconds: 300
            storage:
              capacity: 2Gi
            cloudCacheStorage:
              capacity: 1Gi
            resources:
              requests:
                cpu: "100m"
                memory: 512Mi
              limits:
                cpu: "100m"
                memory: 512Mi
            additionalCommandlineArguments:
              abort-on-seastar-bad-alloc: ""
              dump-memory-diagnostics-on-alloc-failure-kind: "all"
            hostIndexOffset: 100
      """
    Then cluster "rp-with-pools" should eventually have 3 Broker CRs
    And no StatefulSet should exist for cluster "rp-with-pools"
    And all Broker CRs for cluster "rp-with-pools" should be Running
    And all Broker CRs for cluster "rp-with-pools" should be Stable
    And cluster "rp-with-pools" admin API should show 3 brokers
    And I snapshot pod UIDs for cluster "rp-with-pools"
    # Rollback: the nodePool StatefulSet is created and adopts the broker-built pods.
    When I remove annotation "operator.redpanda.com/migrate-to-broker-cr" from V1 cluster "rp-with-pools"
    Then a StatefulSet should eventually exist for cluster "rp-with-pools"
    And cluster "rp-with-pools" should eventually have 0 Broker CRs
    And cluster "rp-with-pools" admin API should show 3 brokers
    And pods for cluster "rp-with-pools" should have the same UIDs as the snapshot
    # Re-migrate back to Broker CRs.
    When I set annotation "operator.redpanda.com/migrate-to-broker-cr" to "true" on V1 cluster "rp-with-pools"
    Then cluster "rp-with-pools" should have 3 Broker CRs
    And no StatefulSet should eventually exist for cluster "rp-with-pools"
    And all Broker CRs for cluster "rp-with-pools" should be Running
    And all Broker CRs for cluster "rp-with-pools" should be Stable
    And cluster "rp-with-pools" admin API should show 3 brokers
    And pods for cluster "rp-with-pools" should have the same UIDs as the snapshot
    # Scale the nodePool up while in broker mode.
    When I set nodePool "blue-a" replicas to 4 on V1 cluster "rp-with-pools"
    Then cluster "rp-with-pools" should eventually have 4 Broker CRs
    And all Broker CRs for cluster "rp-with-pools" should be Running
    And all Broker CRs for cluster "rp-with-pools" should be Stable
    And cluster "rp-with-pools" admin API should show 4 brokers
    # Scale back down: the excess broker is decommissioned and removed.
    When I set nodePool "blue-a" replicas to 3 on V1 cluster "rp-with-pools"
    Then cluster "rp-with-pools" should eventually have 3 Broker CRs
    And all Broker CRs for cluster "rp-with-pools" should be Running
    And all Broker CRs for cluster "rp-with-pools" should be Stable
    And cluster "rp-with-pools" admin API should show 3 brokers
    # Scale up again and manually decommission a broker before the scale-up
    # completes: the intent must be honored (never unset by the operator) and
    # the still-desired index healed by replacing the Broker CR with a fresh
    # node identity once the decommission finishes.
    When I set nodePool "blue-a" replicas to 4 on V1 cluster "rp-with-pools"
    And I set decommission on the Broker with index 1 of cluster "rp-with-pools"
    Then the Broker with index 1 of cluster "rp-with-pools" should be replaced
    And cluster "rp-with-pools" should eventually have 4 Broker CRs
    And all Broker CRs for cluster "rp-with-pools" should be Running
    And all Broker CRs for cluster "rp-with-pools" should be Stable
    And cluster "rp-with-pools" admin API should show 4 brokers
