@cluster:sasl @variant:vectorized
Feature: Role CRDs
  Background: Cluster available
    Given cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage roles
    Given there is no role "admin-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | alice   | password | SCRAM-SHA-256 |
      | bob     | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::manage-roles-with-principals[]
    # In this example manifest, a role called "admin-role" is created in a cluster called "sasl".
    # The role includes two principals (alice and bob) who will inherit the role's permissions.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: admin-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:alice
        - User:bob
# end::manage-roles-with-principals[]
    """
    And role "admin-role" is successfully synced
    Then role "admin-role" should exist in cluster "sasl"
    And role "admin-role" should have members "alice and bob" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage roles with authorization
    Given there is no role "read-only-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | charlie | password | SCRAM-SHA-256 |
    When I create topic "public-test" in cluster "sasl"
    And I apply Kubernetes manifest:
    """
# tag::manage-roles-with-authorization[]
    # In this example manifest, a role called "read-only-role" is created in a cluster called "sasl".
    # The role includes authorization rules that allow reading from topics with names starting with "public-".
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: read-only-role
    spec:
      cluster:
        clusterRef:
          name: sasl
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
    Then role "read-only-role" should exist in cluster "sasl"
    And role "read-only-role" should have ACLs for topic pattern "public-" in cluster "sasl"
    And "charlie" should be able to read from topic "public-test" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage authorization-only roles
    Given there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | travis  | password | SCRAM-SHA-256 |
    And there is a pre-existing role "travis-role" in cluster "sasl"
    When I apply Kubernetes manifest:
    """
# tag::manage-authz-only-roles[]
    # In this example manifest, a role CRD called "travis-role" manages ACLs for an existing role.
    # The role includes authorization rules that allow reading from topics with names starting with "some-topic".
    # This example assumes that you already have a role called "travis-role" in your cluster.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: travis-role
    spec:
      cluster:
        clusterRef:
          name: sasl
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
    Then there should still be role "travis-role" in cluster "sasl"
    And there should be no ACLs for role "travis-role" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Add managed principals to the role
    Given there is no role "team-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | user1   | password | SCRAM-SHA-256 |
      | user2   | password | SCRAM-SHA-256 |
      | user3   | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: team-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:user1
        - User:user2
    """
    And role "team-role" is successfully synced
    Then role "team-role" should exist in cluster "sasl"
    And role "team-role" should have members "user1 and user2" in cluster "sasl"
    And RedpandaRole "team-role" should have status field "managedPrincipals" set to "true"
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: team-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:user1
        - User:user2
        - User:user3
    """
    And role "team-role" is successfully synced
    Then role "team-role" should have members "user1 and user2 and user3" in cluster "sasl"
    And RedpandaRole "team-role" should have status field "managedPrincipals" set to "true"

  @skip:gke @skip:aks @skip:eks
  Scenario: Remove managed principals from the role
    Given there is no role "shrinking-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | dev1    | password | SCRAM-SHA-256 |
      | dev2    | password | SCRAM-SHA-256 |
      | dev3    | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: shrinking-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:dev1
        - User:dev2
        - User:dev3
    """
    And role "shrinking-role" is successfully synced
    Then role "shrinking-role" should have members "dev1 and dev2 and dev3" in cluster "sasl"
    And RedpandaRole "shrinking-role" should have status field "managedPrincipals" set to "true"
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: shrinking-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:dev1
    """
    And role "shrinking-role" is successfully synced
    Then role "shrinking-role" should have members "dev1" in cluster "sasl"
    And role "shrinking-role" should not have member "dev2" in cluster "sasl"
    And role "shrinking-role" should not have member "dev3" in cluster "sasl"
    And RedpandaRole "shrinking-role" should have status field "managedPrincipals" set to "true"

  @skip:gke @skip:aks @skip:eks
  Scenario: Stop managing principals
    Given there is no role "clearing-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | temp1   | password | SCRAM-SHA-256 |
      | temp2   | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: clearing-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:temp1
        - User:temp2
    """
    And role "clearing-role" is successfully synced
    Then role "clearing-role" should have members "temp1 and temp2" in cluster "sasl"
    And RedpandaRole "clearing-role" should have status field "managedPrincipals" set to "true"
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: clearing-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals: []
    """
    And role "clearing-role" is successfully synced
    Then RedpandaRole "clearing-role" should have no members in cluster "sasl"
    And RedpandaRole "clearing-role" should have status field "managedPrincipals" set to "false"

  @skip:gke @skip:aks @skip:eks
  Scenario: Replace all managed principals
    Given there is no role "swap-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | olduser1| password | SCRAM-SHA-256 |
      | olduser2| password | SCRAM-SHA-256 |
      | newuser1| password | SCRAM-SHA-256 |
      | newuser2| password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: swap-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:olduser1
        - User:olduser2
    """
    And role "swap-role" is successfully synced
    Then role "swap-role" should have members "olduser1 and olduser2" in cluster "sasl"
    And RedpandaRole "swap-role" should have status field "managedPrincipals" set to "true"
    When I apply Kubernetes manifest:
    """
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: swap-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      principals:
        - User:newuser1
        - User:newuser2
    """
    And role "swap-role" is successfully synced
    Then role "swap-role" should have members "newuser1 and newuser2" in cluster "sasl"
    And role "swap-role" should not have member "olduser1" in cluster "sasl"
    And role "swap-role" should not have member "olduser2" in cluster "sasl"
    And RedpandaRole "swap-role" should have status field "managedPrincipals" set to "true"
