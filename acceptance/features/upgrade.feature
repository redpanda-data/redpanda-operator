Feature: Upgrading redpanda
  @skip:gke @skip:aks @skip:eks @skip:k3d
  Scenario: Redpanda upgrade from 25.2.11
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: cluster-upgrade
    spec:
      clusterSpec:
        image:
          repository: redpandadata/redpanda
          tag: v25.2.11
        console:
          enabled: false
        statefulset:
          replicas: 3
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "cluster-upgrade" should be stable with 3 nodes
    Then I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: cluster-upgrade
    spec:
      clusterSpec:
        image:
          repository: redpandadata/redpanda-unstable
          tag: v25.3.1-rc4
        console:
          enabled: false
        statefulset:
          replicas: 3
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "cluster-upgrade" should be stable with 3 nodes
