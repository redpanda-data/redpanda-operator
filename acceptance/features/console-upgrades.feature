@operator:none
Feature: Upgrading the operator with Console installed
  @skip:gke @skip:aks @skip:eks
  Scenario: Console v2 to v3 no warnings
    Given I helm install "redpanda-operator" "redpanda/operator" --version v25.1.3 with values:
    """
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: operator-console-upgrade
    spec:
      clusterSpec:
        console:
          # Old versions have broken chart rendering for the console stanza
          # unless nameOverride is set due to mapping configmap values for
          # both the console deployment and redpanda statefulset to the same
          # name. Setting nameOverride to "broken" works around this.
          nameOverride: broken
        tls:
          enabled: false
        external:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "operator-console-upgrade" is available
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    And cluster "operator-console-upgrade" should be stable with 1 nodes
    And the migrated console cluster "operator-console-upgrade" should have 0 warnings

  @skip:gke @skip:aks @skip:eks
  Scenario: Console v2 to v3 with warnings
    Given I helm install "redpanda-operator" "redpanda/operator" --version v25.1.3 with values:
    """
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: operator-console-upgrade-warnings
    spec:
      clusterSpec:
        console:
          nameOverride: broken
          console:
            roleBindings:
            - roleName: admin
              subjects:
              - kind: group
                provider: OIDC
                name: devs
        tls:
          enabled: false
        external:
          enabled: false
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
    """
    And cluster "operator-console-upgrade-warnings" is available
    Then I can helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      experimental: true
    """
    And cluster "operator-console-upgrade-warnings" should be stable with 1 nodes
    And the migrated console cluster "operator-console-upgrade-warnings" should have 1 warning
