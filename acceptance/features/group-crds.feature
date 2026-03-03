@cluster:sasl @variant:vectorized
Feature: Group CRDs
  Background: Cluster available
    Given cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage group ACLs
    When I apply Kubernetes manifest:
    """
# tag::manage-group-acls[]
    # In this example manifest, ACLs are created for an OIDC group called "engineering"
    # in a cluster called "sasl". The group is granted read access to topics matching "team-".
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: engineering
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: team-
              patternType: prefixed
            operations: [Read, Describe]
# end::manage-group-acls[]
    """
    And group "engineering" is successfully synced
    Then group "engineering" should have ACLs for topic pattern "team-" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage group without authorization
    When I apply Kubernetes manifest:
    """
    # In this example manifest, a group called "empty-group" is created without any ACLs.
    # The operator will ensure no ACLs exist for this group principal.
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: empty-group
    spec:
      cluster:
        clusterRef:
          name: sasl
    """
    And group "empty-group" is successfully synced
    Then there should be no ACLs for group "empty-group" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Delete group CRD removes ACLs
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: temp-group
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: temp-topic
            operations: [Read]
    """
    And group "temp-group" is successfully synced
    Then group "temp-group" should have ACLs in cluster "sasl"
    When I delete the CRD group "temp-group"
    Then there should be no ACLs for group "temp-group" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Update group ACLs
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: platform
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: platform-
              patternType: prefixed
            operations: [Read]
    """
    And group "platform" is successfully synced
    Then group "platform" should have ACLs for topic pattern "platform-" in cluster "sasl"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: platform
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: platform-
              patternType: prefixed
            operations: [Read, Write, Describe]
    """
    And group "platform" is successfully synced
    Then group "platform" should have ACLs for topic pattern "platform-" in cluster "sasl"

  @skip:gke @skip:aks @skip:eks
  Scenario: Remove group authorization
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: devops
    spec:
      cluster:
        clusterRef:
          name: sasl
      authorization:
        acls:
          - type: allow
            resource:
              type: topic
              name: devops-
              patternType: prefixed
            operations: [Read]
    """
    And group "devops" is successfully synced
    Then group "devops" should have ACLs for topic pattern "devops-" in cluster "sasl"
    When I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Group
    metadata:
      name: devops
    spec:
      cluster:
        clusterRef:
          name: sasl
    """
    And group "devops" is successfully synced
    Then there should be no ACLs for group "devops" in cluster "sasl"
