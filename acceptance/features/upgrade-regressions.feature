@operator:none @vcluster
# Note: use the same version of RP across upgrades to minimize
# issues not related to operator upgrade regressions.
Feature: Operator upgrade regressions
  @skip:gke @skip:aks @skip:eks
  Scenario: Regression - field managers
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
        image:
          repository: redpandadata/redpanda
          tag: v25.2.11
        console:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "operator-upgrade" is available
    And service "operator-upgrade" should have field managers:
    """
    cluster.redpanda.com/operator
    """
    And service "operator-upgrade" should not have field managers:
    """
    *kube.Ctl
    """
    Then I helm upgrade "redpanda-operator" "redpanda/operator" --version v25.2.1 with values:
    """
    crds:
      enabled: true
    """
    And cluster "operator-upgrade" should be stable with 1 nodes
    And service "operator-upgrade" should have field managers:
    """
    cluster.redpanda.com/operator
    *kube.Ctl
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
        image:
          repository: redpandadata/redpanda
          tag: v25.2.11
        listeners:
          kafka:
            port: 19093
        console:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "operator-upgrade" should have sync error:
    """
    Service "operator-upgrade" is invalid: spec.ports[3].name: Duplicate value: "kafka"
    """
    Then I helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
    """
    And service "operator-upgrade" should have field managers:
    """
    cluster.redpanda.com/operator
    """
    And service "operator-upgrade" should not have field managers:
    """
    *kube.Ctl
    """
    And cluster "operator-upgrade" should be stable with 1 nodes
