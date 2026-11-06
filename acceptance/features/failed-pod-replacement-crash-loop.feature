Feature: Replacing an outdated pod that crash-loops
  A pod that crash-loops on its current template is kubelet's to restart, and
  the roll waits on it. Once a new template lands, the operator replaces it
  first, without the health check, and the rest roll one at a time as usual.

  @skip:gke @skip:aks @skip:eks
  Scenario: Fixing a crash-looping template forward replaces the stuck pod before rolling the rest
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-crash
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
    And cluster "failed-pod-crash" is stable with 3 nodes
    When I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-crash
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
                    command: ["/bin/sh", "-c", "exit 1"]
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
    Then a pod of cluster "failed-pod-crash" should eventually be unable to start
    # A crash loop is judged at once and the stalled roll requeues every 10s:
    # an operator that replaced a pod on its current template would by now.
    And no pod of cluster "failed-pod-crash" should be replaced for 30 seconds
    When I snapshot pod UIDs for cluster "failed-pod-crash"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-crash
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
              annotations:
                failed-pod-replacement/fixed: "true"
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
    Then the pod of cluster "failed-pod-crash" that can't start should be replaced before any other
    And pods for cluster "failed-pod-crash" should roll one at a time
    And cluster "failed-pod-crash" should be stable with 3 nodes
