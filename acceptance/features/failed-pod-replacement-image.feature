Feature: Replacing an outdated pod whose image can't be pulled
  A broker that can't start keeps the cluster unhealthy, so the health-gated
  roll never reaches it. Once its pod is outdated, the operator replaces it
  without that check, touching no broker that runs.

  @skip:gke @skip:aks @skip:eks
  Scenario: Reverting a template whose image can't be pulled replaces the stuck pod
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-image
      spec:
        clusterSpec:
          external:
            enabled: false
          console:
            enabled: false
          image:
            repository: ${DEFAULT_REDPANDA_REPO}
            tag: ${DEFAULT_REDPANDA_TAG}
          statefulset:
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
      """
    And cluster "failed-pod-image" is stable with 3 nodes
    When I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-image
      spec:
        clusterSpec:
          external:
            enabled: false
          console:
            enabled: false
          image:
            repository: localhost/failed-pod-replacement/redpanda
            tag: ${DEFAULT_REDPANDA_TAG}
          statefulset:
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
      """
    Then a pod of cluster "failed-pod-image" should eventually be unable to start
    When I snapshot pod UIDs for cluster "failed-pod-image"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-image
      spec:
        clusterSpec:
          external:
            enabled: false
          console:
            enabled: false
          image:
            repository: ${DEFAULT_REDPANDA_REPO}
            tag: ${DEFAULT_REDPANDA_TAG}
          statefulset:
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
      """
    Then the pod of cluster "failed-pod-image" that can't start should be replaced before any other
    And cluster "failed-pod-image" should be stable with 3 nodes
    And only the pod of cluster "failed-pod-image" that couldn't start should have been replaced since the snapshot
