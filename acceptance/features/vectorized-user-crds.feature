@cluster:vectorized/sasl
Feature: Vectorized User CRDs
  Background: Vectorized Cluster available
    Given vectorized cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage vectorized users
    Given there is no user "bob" in vectorized cluster "sasl"
    And there is no user "james" in vectorized cluster "sasl"
    And there is no user "alice" in vectorized cluster "sasl"
    When I create CRD-based users for vectorized cluster "sasl":
      | name  | password | mechanism     | acls |
      | bob   |          | SCRAM-SHA-256 |      |
      | james |          | SCRAM-SHA-512 |      |
      | alice | qwerty   | SCRAM-SHA-512 |      |
    Then "bob" should exist and be able to authenticate to the vectorized "sasl" cluster
    And "james" should exist and be able to authenticate to the vectorized "sasl" cluster
    And "alice" should exist and be able to authenticate to the vectorized "sasl" cluster

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage authentication-only vectorized users
    Given there is no user "jason" in vectorized cluster "sasl"
    And there are already the following ACLs in vectorized cluster "sasl":
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
          group: redpanda.vectorized.io
          kind: Cluster
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
    Then there should be ACLs in the vectorized cluster "sasl" for user "jason"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage authorization-only vectorized users
    Given there are the following pre-existing users in vectorized cluster "sasl"
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
          group: redpanda.vectorized.io
          kind: Cluster
          name: sasl
      authentication:
        type: scram-sha-512
        password:
          valueFrom:
            secretKeyRef:
              name: travis-password
              key: password
      authorization:
        acls:
        - type: allow
          resource:
            type: topic
            name: some-topic
            patternType: prefixed
          operations: [Read]
    """
    And user "travis" is successfully synced
    And I delete the CRD user "travis"
    Then "travis" should be able to authenticate to the vectorized "sasl" cluster with password "password" and mechanism "SCRAM-SHA-256"
