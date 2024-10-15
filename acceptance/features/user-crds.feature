@cluster:sasl
Feature: User CRDs
  Background: Cluster available
    Given cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Managing Users
    Given there is no user "bob" in cluster "sasl"
    And there is no user "james" in cluster "sasl"
    And there is no user "alice" in cluster "sasl"
    When I create CRD-based users for cluster "sasl":
      | name  | password | mechanism     | acls |
      | bob   |          | SCRAM-SHA-256 |      |
      | james |          | SCRAM-SHA-512 |      |
      | alice | qwerty   | SCRAM-SHA-512 |      |
    Then "bob" should exist and be able to authenticate to the "sasl" cluster
    And "james" should exist and be able to authenticate to the "sasl" cluster
    And "alice" should exist and be able to authenticate to the "sasl" cluster

  @skip:gke @skip:aks @skip:eks
  Scenario: Managing Authentication-only Users
    Given there is no user "jason" in cluster "sasl"
    And there are already the following ACLs in cluster "sasl":
      | user   | acls |
      | jason  | [{"type":"allow","resource":{"type":"cluster"},"operations":["Read"]}] |
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: User
    metadata:
        name: jason
    spec:
        cluster:
            clusterRef:
                name: sasl
        authentication:
            type: scram-sha-512
            password:
                valueFrom:
                    secretKeyRef:
                        name: jason-password
                        key: password
    """
    And user "jason" is successfully synced
    And I delete the CRD user "jason"
    Then there should be ACLs in the cluster "sasl" for user "jason"

  @skip:gke @skip:aks @skip:eks
  Scenario: Managing Authorization-only Users
    Given there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | travis  | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: User
    metadata:
        name: travis
    spec:
        cluster:
            clusterRef:
                name: sasl
        authorization:
            acls:
            -   type: allow
                resource:
                    type: topic
                    name: some-topic
                    patternType: prefixed
                operations: [Read]
    """
    And user "travis" is successfully synced
    And I delete the CRD user "travis"
    Then "travis" should be able to authenticate to the "sasl" cluster with password "password" and mechanism "SCRAM-SHA-256"
