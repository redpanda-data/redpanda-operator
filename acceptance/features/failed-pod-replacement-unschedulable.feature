Feature: Replacing an outdated pod that can't be scheduled
  A pod whose template fits no node never starts. Once its pod is outdated,
  the operator replaces it, and the replacement lands back on its volume's
  node.

  @skip:gke @skip:aks @skip:eks
  Scenario: Reverting a template that fits no node replaces the stuck pod
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-unschedulable
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
    And cluster "failed-pod-unschedulable" is stable with 3 nodes
    When I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-unschedulable
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
            podTemplate:
              spec:
                nodeSelector:
                  failed-pod-replacement/nowhere: "true"
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
    Then a pod of cluster "failed-pod-unschedulable" should eventually be unable to start
    When I snapshot pod UIDs for cluster "failed-pod-unschedulable"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-unschedulable
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
    Then the pod of cluster "failed-pod-unschedulable" that can't start should be replaced before any other
    And cluster "failed-pod-unschedulable" should be stable with 3 nodes
    And only the pod of cluster "failed-pod-unschedulable" that couldn't start should have been replaced since the snapshot
