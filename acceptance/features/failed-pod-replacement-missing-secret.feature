Feature: Replacing an outdated pod whose config can't be built
  A pod whose template references a Secret that doesn't exist never starts.
  Once its pod is outdated, the operator replaces it, touching no broker that
  runs.

  @skip:gke @skip:aks @skip:eks
  Scenario: Reverting a template that references a missing Secret replaces the stuck pod
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-secret
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
    And cluster "failed-pod-secret" is stable with 3 nodes
    When I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-secret
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
                containers:
                  - name: redpanda
                    env:
                      - name: FAILED_POD_REPLACEMENT
                        valueFrom:
                          secretKeyRef:
                            name: failed-pod-replacement-missing
                            key: value
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
    Then a pod of cluster "failed-pod-secret" should eventually be unable to start
    When I snapshot pod UIDs for cluster "failed-pod-secret"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-secret
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
    Then the pod of cluster "failed-pod-secret" that can't start should be replaced before any other
    And cluster "failed-pod-secret" should be stable with 3 nodes
    And only the pod of cluster "failed-pod-secret" that couldn't start should have been replaced since the snapshot
