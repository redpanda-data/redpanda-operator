@operator:none @vcluster
Feature: Upgrading the operator
  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 25.2.2
    Given I helm install "redpanda-operator" "redpanda/operator" --version v25.2.2 with values:
    """
    crds:
      enabled: true
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: operator-upgrade
    spec:
      clusterSpec:
        console:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "operator-upgrade" should be stable with 1 nodes
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    And cluster "operator-upgrade" should be stable with 1 nodes
