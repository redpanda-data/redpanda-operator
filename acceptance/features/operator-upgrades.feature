@operator:none
Feature: Upgrading the operator

  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 2.3.x
    Given I kubectl kustomize "https://github.com/redpanda-data/redpanda-operator//operator/config/crd?ref=v2.3.9-24.3.11" | kubectl apply -f - --server-side
    And I helm install "redpanda-operator" "redpanda/operator" --version v2.3.9-24.3.11 with values:
    """

    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: operator-upgrade-2-3
    spec:
      clusterSpec:
        statefulset:
          replicas: 1
    """
    # use just a Ready status check here since that's all the
    # old operator supports
    And cluster "operator-upgrade-2-3" is available
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade-2-3" should be stable with 1 nodes

  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 2.4.x
    Given I kubectl kustomize "https://github.com/redpanda-data/redpanda-operator//operator/config/crd?ref=operator/v2.4.5" | kubectl apply -f - --server-side
    And I can helm install "redpanda-operator" "redpanda/operator" --version v2.4.5 with values:
    """
    console:
      enabled: false
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: operator-upgrade-2-4
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
    And cluster "operator-upgrade-2-4" is available
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade-2-4" should be stable with 1 nodes
