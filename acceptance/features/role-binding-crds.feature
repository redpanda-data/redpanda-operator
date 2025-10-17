@cluster:sasl @variant:vectorized
Feature: RoleBinding CRDs
  Background: Cluster available
    Given cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage role with RoleBinding
    Given there is no role "developer-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | alice   | password | SCRAM-SHA-256 |
      | bob     | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::manage-role-without-inline-principals[]
    # In this example, a role called "developer-role" is created without inline principals.
    # Principals will be managed via RoleBinding resources.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: developer-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: dev-
              patternType: prefixed
            operations: [Read, Write, Describe]
# end::manage-role-without-inline-principals[]
    """
    And role "developer-role" is successfully synced
    And I apply Kubernetes manifest:
    """
# tag::manage-rolebinding[]
    # In this example, a RoleBinding assigns principals alice and bob to the developer-role.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: developer-binding
    spec:
      roleRef:
        name: developer-role
      principals:
        - User:alice
        - User:bob
# end::manage-rolebinding[]
    """
    And rolebinding "developer-binding" is successfully synced
    Then role "developer-role" should exist in cluster "sasl"
    And role "developer-role" should have members "alice and bob" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Multiple RoleBindings for single Role
    Given there is no role "shared-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | alice   | password | SCRAM-SHA-256 |
      | bob     | password | SCRAM-SHA-256 |
      | charlie | password | SCRAM-SHA-256 |
      | dave    | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    # Create a role without inline principals
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: shared-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: shared-
              patternType: prefixed
            operations: [Read, Describe]
    """
    And role "shared-role" is successfully synced
    And I apply Kubernetes manifest:
    """
# tag::multiple-rolebindings[]
    # Multiple RoleBindings can reference the same Role
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: team-a-binding
    spec:
      roleRef:
        name: shared-role
      principals:
        - User:alice
        - User:bob
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: team-b-binding
    spec:
      roleRef:
        name: shared-role
      principals:
        - User:charlie
        - User:dave
# end::multiple-rolebindings[]
    """
    And rolebinding "team-a-binding" is successfully synced
    And rolebinding "team-b-binding" is successfully synced
    Then role "shared-role" should exist in cluster "sasl"
    And role "shared-role" should have members "alice and bob and charlie and dave" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: RoleBinding with mixed principal formats
    Given there is no role "mixed-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name             | password | mechanism     |
      | alice            | password | SCRAM-SHA-256 |
      | santiago@test.foo| password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    # Create a role for testing mixed principal formats
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: mixed-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: test-
              patternType: prefixed
            operations: [Read]
    """
    And role "mixed-role" is successfully synced
    And I apply Kubernetes manifest:
    """
# tag::mixed-principal-formats[]
    # RoleBinding supports different principal formats
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: mixed-principals-binding
    spec:
      roleRef:
        name: mixed-role
      principals:
        - User:alice
        - santiago@test.foo
# end::mixed-principal-formats[]
    """
    And rolebinding "mixed-principals-binding" is successfully synced
    Then role "mixed-role" should exist in cluster "sasl"
    And role "mixed-role" should have members "alice and santiago@test.foo" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: RoleBinding with non-existent Role
    Given there is no role "non-existent-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name  | password | mechanism     |
      | alice | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::rolebinding-nonexistent-role[]
    # RoleBinding referencing a Role that doesn't exist will have Synced=False
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: orphan-binding
    spec:
      roleRef:
        name: non-existent-role
      principals:
        - User:alice
# end::rolebinding-nonexistent-role[]
    """
    Then rolebinding "orphan-binding" condition "Synced" should be "False"
    And I delete the CRD rolebinding "orphan-binding"

  @skip:gke @skip:aks @skip:eks
  Scenario: Deleting RoleBinding removes principals but not the role
    Given there is no role "persistent-role" in cluster "sasl"
    And there are the following pre-existing users in cluster "sasl"
      | name  | password | mechanism     |
      | alice | password | SCRAM-SHA-256 |
      | bob   | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
    # Create a role with authorization
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRole
    metadata:
      name: persistent-role
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: persist-
              patternType: prefixed
            operations: [Read]
    """
    And role "persistent-role" is successfully synced
    And I apply Kubernetes manifest:
    """
    # Create RoleBinding
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaRoleBinding
    metadata:
      name: temporary-binding
    spec:
      roleRef:
        name: persistent-role
      principals:
        - User:alice
        - User:bob
    """
    And rolebinding "temporary-binding" is successfully synced
    And role "persistent-role" should have members "alice and bob" in cluster "sasl"
    And I delete the CRD rolebinding "temporary-binding"
    And role "persistent-role" should have removed principals "alice and bob" in cluster "sasl"
    And role "persistent-role" is successfully synced
    Then there should still be role "persistent-role" in cluster "sasl"
