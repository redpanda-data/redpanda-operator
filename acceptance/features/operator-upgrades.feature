@operator:none @vcluster
Feature: Upgrading the operator
  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 25.1.3
    Given I helm install "redpanda-operator" "redpanda/operator" --version v25.1.3 with values:
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
    # use just a Ready status check here since that's all the
    # old operator supports
    And cluster "operator-upgrade" is available
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade" should be stable with 1 nodes
