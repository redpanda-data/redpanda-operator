@cluster:steering @variant:vectorized
Feature: Endpoint steering
  A cluster annotated for endpoint steering has the operator publish its
  Service endpoints itself, port by port: every broker on the Kafka port, but
  on the Schema Registry port only the brokers whose registry answers
  GET /status/ready. Pod readiness never enters into it, so a broker keeps
  serving Kafka while its registry is out of rotation.

  A pod that both Services take as a broker -- its labels say so -- and that
  serves no Schema Registry stands in for a registry that is still replaying
  _schemas, which nothing can trigger on demand. Taking a live broker's
  registry away instead would mean shutting one of its ports, and the only
  lever for that in a cluster is a NetworkPolicy, which on k3d is enforced
  for traffic between nodes and not for traffic within one: whether it
  reached the operator's probes at all came down to where the operator
  happened to be scheduled.

  So this scenario asserts the steady state, in a real cluster, on both the
  Service the operator renders and a Service managed alongside it -- for a V2
  Redpanda, and for a V1 Cluster as the vectorized variant.
  TestSteersSchemaRegistryPort covers the transitions -- a registry that stops
  answering losing its endpoint, and regaining it when it recovers -- against
  a real API server, where a registry can be switched off on demand.

  @skip:gke @skip:aks @skip:eks
  Scenario: Schema Registry endpoints carry only brokers whose registry answers
    Given cluster "steering" is available
    # A Service managed outside the operator opts in with the annotation and
    # no selector. Its port names differ from the cluster's own on purpose:
    # steering keys on the cluster's Schema Registry port number.
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
    Then the internal service of cluster "steering" should have no selector
    And every unsteered service of cluster "steering" should have a selector
    And the "kafka" listener of cluster "steering" should publish pods "steering-0, steering-1, steering-2"
    And the "schema registry" listener of cluster "steering" should publish pods "steering-0, steering-1, steering-2"
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
        # V1 selects its brokers on the component label as well; V2 ignores
        # it, so one pod is a broker to either.
        app.kubernetes.io/component: redpanda
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
    Then the "kafka" listener of cluster "steering" should publish pods "steering-0, steering-1, steering-2, steering-mute"
    And the "brokers" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2, steering-mute"
    And the "schema registry" listener of cluster "steering" should publish pods "steering-0, steering-1, steering-2"
    And the "registry" port of service "steering-clients" should publish pods "steering-0, steering-1, steering-2"
    # Readiness is untouched by any of this: the pod the Schema Registry port
    # left out is a pod Kubernetes considers ready.
    And pod "steering-mute" should be ready
