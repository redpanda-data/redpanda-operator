Feature: Endpoint steering
  With --enable-endpoint-steering the operator publishes Service endpoints
  itself, port by port: every broker on the Kafka port, but on the Schema
  Registry port only the brokers whose registry answers GET /status/ready.
  Pod readiness never enters into it, so a broker keeps serving Kafka while
  its registry is out of rotation. Blocking a pod's registry port stands in
  for a registry that is still replaying _schemas, which nothing can trigger
  on demand.

  @skip:gke @skip:aks @skip:eks
  Scenario: Schema Registry endpoints follow registry health, not pod readiness
    Given I create a basic cluster "steering" with 3 nodes
    And cluster "steering" is stable with 3 nodes
    # A Service managed outside the operator opts in with the annotation and
    # no selector. Its port names differ from the internal Service's on
    # purpose: steering keys on the cluster's Schema Registry port number.
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: v1
    kind: Service
    metadata:
      name: steering-clients
      annotations:
        cluster.redpanda.com/endpoints-for: steering
    spec:
      ports:
        - name: brokers
          port: 9093
          targetPort: 9093
        - name: registry
          port: 8081
          targetPort: 8081
    """
    Then service "steering" should have no selector
    And the "kafka" port of service "steering" should publish pods "steering-0, steering-1, steering-2"
    And the "schemaregistry" port of service "steering" should publish pods "steering-0, steering-1, steering-2"
    And the "brokers" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    And the "registry" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    When I block ingress to port 8081 of pod "steering-0"
    # The block is a precondition, not the assertion: k3s programs policy
    # rules on its own sync loop, so wait until the port really is shut.
    Then port 8081 of pod "steering-0" should be unreachable
    And the "registry" port of service "steering-clients" should publish pods "steering-1, steering-2"
    And the "schemaregistry" port of service "steering" should publish pods "steering-1, steering-2"
    And the "brokers" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    And the "kafka" port of service "steering" should publish pods "steering-0, steering-1, steering-2"
    And pod "steering-0" should be ready
    When I unblock ingress to pod "steering-0"
    Then port 8081 of pod "steering-0" should be reachable
    And the "registry" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    And the "schemaregistry" port of service "steering" should publish pods "steering-0, steering-1, steering-2"

  @skip:gke @skip:aks @skip:eks
  Scenario: V1 Cluster Services are steered the same way
    Given I apply Kubernetes manifest:
    """
    apiVersion: redpanda.vectorized.io/v1alpha1
    kind: Cluster
    metadata:
      name: steering-v1
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
        schemaRegistry:
          port: 8081
        developerMode: true
        additionalCommandlineArguments:
          dump-memory-diagnostics-on-alloc-failure-kind: all
          abort-on-seastar-bad-alloc: ''
    """
    And vectorized cluster "steering-v1" is available
    # The -cluster Service is the one carrying the Schema Registry, so it is
    # the one steered; the headless Service carries broker discovery and is
    # deliberately left to the native controller.
    #
    # Everything here is asserted on that Service alone, because it publishes
    # not-ready addresses. A V1 broker's readiness is the whole cluster's
    # health (`rpk cluster health`), which flaps under CI load, so a Service
    # that gates on readiness would have its endpoints moving for reasons
    # that have nothing to do with steering. Steering an independently
    # managed Service is covered by the scenario above.
    Then service "steering-v1-cluster" should have no selector
    And service "steering-v1" should have a selector
    And the "kafka" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And the "schema-registry" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    When I block ingress to port 8081 of pod "steering-v1-0"
    Then port 8081 of pod "steering-v1-0" should be unreachable
    And the "schema-registry" port of service "steering-v1-cluster" should publish pods "steering-v1-1, steering-v1-2"
    And the "kafka" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    When I unblock ingress to pod "steering-v1-0"
    Then port 8081 of pod "steering-v1-0" should be reachable
    And the "schema-registry" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
