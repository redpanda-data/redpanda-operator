Feature: Replacing an outdated Broker CR pod whose image can't be pulled
  In Broker CR mode, a pod that can't start gets a no-drain roll grant once
  it's outdated: the Broker controller replaces it without the health check
  its own outage fails, and no other Broker rolls while it's down.

  @skip:gke @skip:aks @skip:eks
  Scenario: Reverting a template whose image can't be pulled replaces the stuck Broker's pod
    Given I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-broker
        annotations:
          operator.redpanda.com/use-broker-cr: "true"
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
    And cluster "failed-pod-broker" is stable with 3 nodes
    And cluster "failed-pod-broker" should have 3 Broker CRs
    When I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-broker
        annotations:
          operator.redpanda.com/use-broker-cr: "true"
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
    Then a pod of cluster "failed-pod-broker" should eventually be unable to start
    When I snapshot pod UIDs for cluster "failed-pod-broker"
    And I apply Kubernetes manifest:
      """
      apiVersion: cluster.redpanda.com/v1alpha2
      kind: Redpanda
      metadata:
        name: failed-pod-broker
        annotations:
          operator.redpanda.com/use-broker-cr: "true"
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
    Then the pod of cluster "failed-pod-broker" that can't start should be replaced before any other
    And cluster "failed-pod-broker" should be stable with 3 nodes
    And all Broker CRs for cluster "failed-pod-broker" should be Running
    And only the pod of cluster "failed-pod-broker" that couldn't start should have been replaced since the snapshot
