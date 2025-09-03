@operator:none
Feature: Upgrading the operator
  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 2.4.5
    Given I install local CRDs from "../operator/config/crd/bases"
    And I install redpanda helm chart version "v2.4.5" with the values:
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
    Then I can upgrade to the latest operator with the values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade" should be stable with 1 nodes

  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 25.1.3
    And I install redpanda helm chart version "v25.1.3" with the values:
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
    Then I can upgrade to the latest operator with the values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade" should be stable with 1 nodes
