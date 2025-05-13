@operator:none
Feature: Upgrading the operator
  @skip:gke @skip:aks @skip:eks
  Scenario: Operator upgrade from 2.3.x
    Given I install redpanda helm chart version "v2.3.9-24.3.11" with the values:
    """
    
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
        statefulset:
          replicas: 1
    """
    # use just a Ready status check here since that's all the
    # old operator supports
    And cluster "operator-upgrade" is available
    Then I can upgrade to the latest operator with the values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    """
    # use the new status as this will eventually get set
    And cluster "operator-upgrade" should be stable with 1 nodes
