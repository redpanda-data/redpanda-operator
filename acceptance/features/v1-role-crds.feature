@cluster:v1/roles
Feature: Vectorized Role CRDs
  Background: Cluster available
    Given vectorized cluster "roles" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage vectorized roles
    Given there is no role "admin-role" in vectorized cluster "roles"
    And there are the following pre-existing users in vectorized cluster "roles"
      | name    | password | mechanism     |
      | alice   | password | SCRAM-SHA-256 |
      | bob     | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: admin-role
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: roles
      principals:
        - User:alice
        - User:bob
    """
    And role "admin-role" is successfully synced
    Then role "admin-role" should exist in vectorized cluster "roles"
    And role "admin-role" should have members "alice and bob" in vectorized cluster "roles"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage vectorized roles with authorization
    Given there is no role "read-only-role" in vectorized cluster "roles"
    And there are the following pre-existing users in vectorized cluster "roles"
      | name    | password | mechanism     |
      | charlie | password | SCRAM-SHA-256 |
    When I create topic "public-test" in vectorized cluster "roles"
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: read-only-role
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: roles
      principals:
        - User:charlie
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: public-
              patternType: prefixed
            operations: [Read, Describe]
    """
    And role "read-only-role" is successfully synced
    Then role "read-only-role" should exist in vectorized cluster "roles"
    And role "read-only-role" should have ACLs for topic pattern "public-" in vectorized cluster "roles"
    And "charlie" should be able to read from topic "public-test" in vectorized cluster "roles"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage vectorized authorization-only roles
    Given there are the following pre-existing users in vectorized cluster "roles"
      | name    | password | mechanism     |
      | travis  | password | SCRAM-SHA-256 |
    And there is a pre-existing role "travis-role" in vectorized cluster "roles"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: travis-role
    spec:
      cluster:
        clusterRef:
          group: redpanda.vectorized.io
          kind: Cluster
          name: roles
      principals:
        - User:travis
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: some-topic
              patternType: prefixed
            operations: [Read]
    """
    And role "travis-role" is successfully synced
    And I delete the CRD role "travis-role"
    Then there should still be role "travis-role" in vectorized cluster "roles"
    And there should be no ACLs for role "travis-role" in vectorized cluster "roles"