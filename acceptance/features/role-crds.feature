@cluster:roles
Feature: Role CRDs
  Background: Cluster available
    Given cluster "roles" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage roles
    Given there is no role "admin-role" in cluster "roles"
    And there are the following pre-existing users in cluster "roles"
      | name    | password | mechanism     |
      | alice   | password | SCRAM-SHA-256 |
      | bob     | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::manage-roles-with-principals[]
    # In this example manifest, a role called "admin-role" is created in a cluster called "roles".
    # The role includes two principals (alice and bob) who will inherit the role's permissions.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: admin-role
    spec:
      cluster:
        clusterRef:
          name: roles
      principals:
        - User:alice
        - User:bob
# end::manage-roles-with-principals[]
    """
    And role "admin-role" is successfully synced
    Then role "admin-role" should exist in cluster "roles"
    And role "admin-role" should have members "alice" and "bob" in cluster "roles"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage roles with authorization
    Given there is no role "read-only-role" in cluster "roles"
    And there are the following pre-existing users in cluster "roles"
      | name    | password | mechanism     |
      | charlie | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::manage-roles-with-authorization[]
    # In this example manifest, a role called "read-only-role" is created in a cluster called "roles".
    # The role includes authorization rules that allow reading from topics with names starting with "public-".
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: read-only-role
    spec:
      cluster:
        clusterRef:
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
# end::manage-roles-with-authorization[]
    """
    And role "read-only-role" is successfully synced
    Then role "read-only-role" should exist in cluster "roles"
    And role "read-only-role" should have ACLs for topic pattern "public-" in cluster "roles"
    And user "charlie" should be able to read from topic "public-test" in cluster "roles"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage authorization-only roles
    Given there are the following pre-existing users in cluster "roles"
      | name    | password | mechanism     |
      | travis  | password | SCRAM-SHA-256 |
    And there is a pre-existing role "travis-role" in cluster "roles"
    When I apply Kubernetes manifest:
    """
# tag::manage-authz-only-roles[]
    # In this example manifest, a role CRD called "travis-role" manages ACLs for an existing role.
    # The role includes authorization rules that allow reading from topics with names starting with "some-topic".
    # This example assumes that you already have a role called "travis-role" in your cluster.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Role
    metadata:
      name: travis-role
    spec:
      cluster:
        clusterRef:
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
# end::manage-authz-only-roles[]
    """
    And role "travis-role" is successfully synced
    And I delete the CRD role "travis-role"
    Then there should still be role "travis-role" in cluster "roles"
    And there should be no ACLs for role "travis-role" in cluster "roles"