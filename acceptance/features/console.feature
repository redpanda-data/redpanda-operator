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
    # These steps demonstrate that console is correct connected to Redpanda.
    And I exec "curl localhost:8080/api/schema-registry/mode" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"mode":"READWRITE"}
    ```
    And I exec "curl localhost:8080/api/topics" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"topics":[{"topicName":"_schemas","isInternal":false,"partitionCount":1,"replicationFactor":1,"cleanupPolicy":"compact","documentation":"NOT_CONFIGURED","logDirSummary":{"totalSizeBytes":117}}]}
    ```
    And I exec "curl localhost:8080/api/console/endpoints" in a Pod matching "app.kubernetes.io/instance=console", it will output:
    ```
    {"distribution":"redpanda","endpointCompatibility":{"kafkaVersion":"between v0.11.0 and v1.0","endpoints":[{"endpoint":"/api/consumer-groups","method":"GET","isSupported":true},{"endpoint":"/api/consumer-groups/{groupId}","method":"PATCH","isSupported":true},{"endpoint":"/api/consumer-groups/{groupId}","method":"DELETE","isSupported":true},{"endpoint":"/api/users","method":"DELETE","isSupported":true},{"endpoint":"redpanda.api.console.v1alpha1.PipelineService","method":"POST","isSupported":false},{"endpoint":"redpanda.api.console.v1alpha1.SecurityService","method":"POST","isSupported":true},{"endpoint":"/api/topics/{topicName}/records","method":"DELETE","isSupported":true},{"endpoint":"/api/consumer-groups/{groupId}/offsets","method":"DELETE","isSupported":true},{"endpoint":"/api/operations/reassign-partitions","method":"PATCH","isSupported":true},{"endpoint":"/api/secrets","method":"POST","isSupported":false},{"endpoint":"/api/users","method":"GET","isSupported":true},{"endpoint":"redpanda.api.console.v1alpha1.TransformService","method":"POST","isSupported":false},{"endpoint":"redpanda.api.dataplane.v1.ACLService","method":"POST","isSupported":true},{"endpoint":"/api/secrets","method":"GET","isSupported":false},{"endpoint":"/api/secrets/{secretId}","method":"PUT","isSupported":false},{"endpoint":"/api/cluster/config","method":"GET","isSupported":true},{"endpoint":"/api/operations/reassign-partitions","method":"GET","isSupported":true},{"endpoint":"/api/quotas","method":"GET","isSupported":true},{"endpoint":"/api/users","method":"POST","isSupported":true},{"endpoint":"redpanda.api.console.v1alpha1.DebugBundleService","method":"POST","isSupported":true},{"endpoint":"/api/secrets/{secretId}","method":"DELETE","isSupported":false}]}}
    ```

  # Scenario: Using staticConfig
  #   Given I apply Kubernetes manifest:
  #   ```yaml
  #   ---
  #   apiVersion: cluster.redpanda.com/v1alpha2
  #   kind: Console
  #   metadata:
  #     name: console
  #   spec:
  #     cluster:
  #   ```
