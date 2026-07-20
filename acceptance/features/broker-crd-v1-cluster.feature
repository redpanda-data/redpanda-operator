@serial
Feature: Broker CRD with V1 Cluster
  @skip:gke @skip:aks @skip:eks
  Scenario: V1 Cluster with broker-cr annotation creates Broker CRs instead of StatefulSets
    Given I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: broker-v1
        annotations:
          operator.redpanda.com/migrate-to-broker-cr: "true"
      spec:
        image: ${DEFAULT_REDPANDA_REPO}
        version: ${DEFAULT_REDPANDA_TAG}
        replicas: 3
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
    Then cluster "broker-v1" should have 3 Broker CRs
    And no StatefulSet should exist for cluster "broker-v1"
    And all Broker CRs for cluster "broker-v1" should be Running
    And cluster "broker-v1" admin API should show 3 brokers
    # A node-config change bumps the config checksum; the cluster controller
    # must serialize the resulting pod rotations via roll-grants.
    When I snapshot pod UIDs for cluster "broker-v1"
    And I add additional configuration "pandaproxy_client.retries" with value "10" to V1 cluster "broker-v1"
    Then pods for cluster "broker-v1" should roll one at a time
    And all Broker CRs for cluster "broker-v1" should be Running
    And cluster "broker-v1" admin API should show 3 brokers
