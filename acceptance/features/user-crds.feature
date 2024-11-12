@cluster:sasl
Feature: User CRDs
  Background: Cluster available
    Given cluster "sasl" is available

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage users
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
  Scenario: Manage authentication-only users
    Given there is no user "jason" in cluster "sasl"
    And there are already the following ACLs in cluster "sasl":
      | user   | acls |
      | jason  | [{"type":"allow","resource":{"type":"cluster"},"operations":["Read"]}] |
    When I apply Kubernetes manifest:
    """
# tag::manage-authn-only-manifest[]
    # In this example manifest, a user called "jason" is created in a cluster called "sasl".
    # The user's password is defined in a Secret called "jason-password".
    # This example assumes that you will create ACLs for this user separately.
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
# end::manage-authn-only-manifest[]
    """
    And user "jason" is successfully synced
    And I delete the CRD user "jason"
    Then there should be ACLs in the cluster "sasl" for user "jason"

  @skip:gke @skip:aks @skip:eks
  Scenario: Manage authorization-only users
    Given there are the following pre-existing users in cluster "sasl"
      | name    | password | mechanism     |
      | travis  | password | SCRAM-SHA-256 |
    When I apply Kubernetes manifest:
    """
# tag::manage-authz-only-manifest[]
    # In this example manifest, an ACL called "travis" is created in a cluster called "sasl".
    # The ACL give an existing user called "travis" permissions to read from all topics whose names start with some-topic.
    # This example assumes that you already have a user called "travis" in your cluster.
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
        - type: allow
          resource:
            type: topic
            name: some-topic
            patternType: prefixed
          operations: [Read]
# end::manage-authz-only-manifest[]
    """
    And user "travis" is successfully synced
    And I delete the CRD user "travis"
    Then "travis" should be able to authenticate to the "sasl" cluster with password "password" and mechanism "SCRAM-SHA-256"
