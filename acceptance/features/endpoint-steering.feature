Feature: Endpoint steering
  With --enable-endpoint-steering the operator publishes Service endpoints
  itself, port by port: every broker on the Kafka port, but on the Schema
  Registry port only the brokers whose registry answers GET /status/ready.
  Pod readiness never enters into it, so a broker keeps serving Kafka while
  its registry is out of rotation.

  A pod that both Services take as a broker -- its labels and its place under
  the cluster's DNS say so -- and that serves no Schema Registry stands in for
  a registry that is still replaying _schemas, which nothing can trigger on
  demand. Taking a live broker's registry away instead would mean shutting one
  of its ports, and the only lever for that in a cluster is a NetworkPolicy,
  which on k3d is enforced for traffic between nodes and not for traffic
  within one: whether it reached the operator's probes at all came down to
  where the operator happened to be scheduled.

  So these scenarios assert the steady state, in a real cluster, on both the
  Services the operator renders and a Service managed alongside them.
  TestSteersSchemaRegistryPort covers the transitions -- a registry that stops
  answering losing its endpoint, and regaining it when it recovers -- against
  a real API server, where a registry can be switched off on demand.

  @skip:gke @skip:aks @skip:eks
  Scenario: Schema Registry endpoints carry only brokers whose registry answers
    Given I create a basic cluster "steering" with 3 nodes
    And cluster "steering" is stable with 3 nodes
    # A Service managed outside the operator opts in with the annotation and
    # no selector. Its port names differ from the internal Service's on
    # purpose: steering keys on the cluster's Schema Registry port number.
    #
    # It publishes not-ready addresses, as the cluster's own Services and the
    # cloud seed load balancers do, which is what leaves the Schema Registry
    # probe as the only thing that can move an endpoint here.
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
      publishNotReadyAddresses: true
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
    When I apply Kubernetes manifest:
    """
    apiVersion: v1
    kind: Pod
    metadata:
      name: steering-mute
      labels:
        app.kubernetes.io/name: redpanda
        app.kubernetes.io/instance: steering
    spec:
      subdomain: steering
      containers:
        - name: mute
          image: ${DEFAULT_REDPANDA_REPO}:${DEFAULT_REDPANDA_TAG}
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
    """
    And Pod "steering-mute" will eventually be Running
    Then the "kafka" port of service "steering" should publish pods "steering-0, steering-1, steering-2, steering-mute"
    And the "brokers" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2, steering-mute"
    And the "schemaregistry" port of service "steering" should publish pods "steering-0, steering-1, steering-2"
    And the "registry" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    # Readiness is untouched by any of this: the pod the Schema Registry port
    # left out is a pod Kubernetes considers ready.
    And pod "steering-mute" should be ready

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
    And I apply Kubernetes manifest:
    """
    apiVersion: v1
    kind: Service
    metadata:
      name: steering-v1-clients
      annotations:
        cluster.redpanda.com/endpoints-for: steering-v1
    spec:
      publishNotReadyAddresses: true
      ports:
        - name: brokers
          port: 9092
          targetPort: 9092
        - name: registry
          port: 8081
          targetPort: 8081
    """
    # The -cluster Service is the one carrying the Schema Registry, so it is
    # the one steered; the headless Service carries broker discovery and is
    # deliberately left to the native controller.
    #
    # Pod readiness is out of the picture on both Services here, as above. A
    # V1 broker's readiness is the whole cluster's health (`rpk cluster
    # health`), which flaps under CI load, and either Service would then have
    # endpoints moving for reasons that have nothing to do with steering.
    Then service "steering-v1-cluster" should have no selector
    And service "steering-v1" should have a selector
    And the "kafka" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And the "schema-registry" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And the "registry" port of service "steering-v1-clients" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And the "brokers" port of service "steering-v1-clients" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    When I apply Kubernetes manifest:
    """
    apiVersion: v1
    kind: Pod
    metadata:
      name: steering-v1-mute
      labels:
        app.kubernetes.io/name: redpanda
        app.kubernetes.io/instance: steering-v1
        app.kubernetes.io/component: redpanda
    spec:
      subdomain: steering-v1
      containers:
        - name: mute
          image: ${DEFAULT_REDPANDA_REPO}:${DEFAULT_REDPANDA_TAG}
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
    """
    And Pod "steering-v1-mute" will eventually be Running
    Then the "kafka" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2, steering-v1-mute"
    And the "brokers" port of service "steering-v1-clients" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2, steering-v1-mute"
    And the "schema-registry" port of service "steering-v1-cluster" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And the "registry" port of service "steering-v1-clients" should publish pods "steering-v1-0, steering-v1-1, steering-v1-2"
    And pod "steering-v1-mute" should be ready
