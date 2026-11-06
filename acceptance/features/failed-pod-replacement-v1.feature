Feature: Replacing an outdated V1 pod whose image can't be pulled
  A V1 Cluster's rolling update evicts one pod at a time once the cluster is
  healthy, so a pod that can't start stalls it. Once that pod is outdated, the
  operator replaces it ahead of the health check, touching no broker that runs.

  @skip:gke @skip:aks @skip:eks
  Scenario: Reverting a V1 image that can't be pulled replaces the stuck pod
    Given I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: failed-pod-v1
      spec:
        image: ${DEFAULT_REDPANDA_REPO}
        version: ${DEFAULT_REDPANDA_TAG}
        additionalConfiguration: {}
        configuration:
          developerMode: true
          rpcServer:
            port: 33145
          kafkaApi:
            - port: 9092
          adminApi:
            - port: 9644
          pandaproxyApi:
            - port: 8082
          schemaRegistryApi:
            - port: 8081
        nodePools:
          - name: first
            replicas: 3
            # UID stability is asserted: don't let a node blip evict brokers.
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
      """
    And vectorized cluster "failed-pod-v1" is available
    When I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: failed-pod-v1
      spec:
        image: localhost/failed-pod-replacement/redpanda
        version: ${DEFAULT_REDPANDA_TAG}
        additionalConfiguration: {}
        configuration:
          developerMode: true
          rpcServer:
            port: 33145
          kafkaApi:
            - port: 9092
          adminApi:
            - port: 9644
          pandaproxyApi:
            - port: 8082
          schemaRegistryApi:
            - port: 8081
        nodePools:
          - name: first
            replicas: 3
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
      """
    Then a pod of cluster "failed-pod-v1" should eventually be unable to start
    When I snapshot pod UIDs for cluster "failed-pod-v1"
    And I apply Kubernetes manifest:
      """
      apiVersion: redpanda.vectorized.io/v1alpha1
      kind: Cluster
      metadata:
        name: failed-pod-v1
      spec:
        image: ${DEFAULT_REDPANDA_REPO}
        version: ${DEFAULT_REDPANDA_TAG}
        additionalConfiguration: {}
        configuration:
          developerMode: true
          rpcServer:
            port: 33145
          kafkaApi:
            - port: 9092
          adminApi:
            - port: 9644
          pandaproxyApi:
            - port: 8082
          schemaRegistryApi:
            - port: 8081
        nodePools:
          - name: first
            replicas: 3
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
      """
    Then the pod of cluster "failed-pod-v1" that can't start should be replaced before any other
    And vectorized cluster "failed-pod-v1" is available
    And only the pod of cluster "failed-pod-v1" that couldn't start should have been replaced since the snapshot
