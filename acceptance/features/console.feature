@cluster:basic
Feature: Console CRDs
  Background: Cluster available
    Given cluster "basic" is available

  Scenario: Using clusterRef
    When I apply Kubernetes manifest:
    ```yaml
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Console
    metadata:
      name: console
    spec:
      cluster:
        clusterRef:
          name: basic
    ```
    Then Console "console" will be healthy
    # These steps demonstrate that console is correctly connected to Redpanda (Kafka, Schema Registry, and Admin API).
    And I exec "curl localhost:8080/api/schema-registry/mode" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"mode":"READWRITE"}
    ```
    And I exec "curl localhost:8080/api/topics" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"topics":[{"topicName":"_schemas","isInternal":false,"partitionCount":1,"replicationFactor":1,"cleanupPolicy":"compact","documentation":"NOT_CONFIGURED","logDirSummary":{"totalSizeBytes":117}}]}
    ```
    And I exec "curl localhost:8080/api/console/endpoints | grep -o '{[^{}]*DebugBundleService[^{}]*}'" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"endpoint":"redpanda.api.console.v1alpha1.DebugBundleService","method":"POST","isSupported":true}
    ```

  Scenario: Using staticConfig
    When I apply Kubernetes manifest:
    ```yaml
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Console
    metadata:
      name: console
    spec:
      cluster:
        staticConfiguration:
          kafka:
            brokers:
            - basic-0.basic.${NAMESPACE}.svc.cluster.local.:9093
            tls:
              caCertSecretRef:
                name: "basic-default-cert"
                key: "ca.crt"
          admin:
            urls:
            - https://basic-0.basic.${NAMESPACE}.svc.cluster.local.:9644
            tls:
              caCertSecretRef:
                name: "basic-default-cert"
                key: "ca.crt"
          schemaRegistry:
            urls:
            - https://basic-0.basic.${NAMESPACE}.svc.cluster.local.:8081
            tls:
              caCertSecretRef:
                name: "basic-default-cert"
                key: "ca.crt"
    ```
    Then Console "console" will be healthy
    # These steps demonstrate that console is correctly connected to Redpanda (Kafka, Schema Registry, and Admin API).
    And I exec "curl localhost:8080/api/schema-registry/mode" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"mode":"READWRITE"}
    ```
    And I exec "curl localhost:8080/api/topics" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"topics":[{"topicName":"_schemas","isInternal":false,"partitionCount":1,"replicationFactor":1,"cleanupPolicy":"compact","documentation":"NOT_CONFIGURED","logDirSummary":{"totalSizeBytes":117}}]}
    ```
    And I exec "curl localhost:8080/api/console/endpoints | grep -o '{[^{}]*DebugBundleService[^{}]*}'" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"endpoint":"redpanda.api.console.v1alpha1.DebugBundleService","method":"POST","isSupported":true}
    ```
