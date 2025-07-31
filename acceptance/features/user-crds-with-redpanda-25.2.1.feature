Feature: User CRDs
  @skip:gke @skip:aks @skip:eks
  Scenario: Manage users with an upgrade to 25.2
    Given I create a sasl cluster named "sasl"
    And cluster "sasl" is stable with 1 nodes
    And there is no user "bob" in cluster "sasl"
    And there is no user "james" in cluster "sasl"
    And there is no user "alice" in cluster "sasl"
    And I create CRD-based users for cluster "sasl":
      | name  | password | mechanism     | acls |
      | bob   |          | SCRAM-SHA-256 | [{"type":"allow","resource":{"type":"cluster"},"operations":["Alter"]}] |
      | james |          | SCRAM-SHA-512 |      |
      | alice | qwerty   | SCRAM-SHA-512 |      |
    When I upgrade to "sasl" cluster to 25.2.1
    Then cluster "sasl" should be stable with 1 nodes
    And "bob" should exist and be able to authenticate to the "sasl" cluster
    And "james" should exist and be able to authenticate to the "sasl" cluster
    And "alice" should exist and be able to authenticate to the "sasl" cluster
    And I should be able to modify CRD-based users for cluster "sasl":
      | name  | password | mechanism     | acls                                                                            |
      | bob   |          | SCRAM-SHA-256 | [{"type":"allow","resource":{"type":"cluster"},"operations":["Create"]}]        |
      | james |          | SCRAM-SHA-512 | [{"type":"allow","resource":{"type":"cluster"},"operations":["Describe"]}]      |
      | alice | abcdef   | SCRAM-SHA-512 | [{"type":"allow","resource":{"type":"cluster"},"operations":["ClusterAction"]}] |
    And "bob" should exist and be able to authenticate to the "sasl" cluster
    And "james" should exist and be able to authenticate to the "sasl" cluster
    And "alice" should exist and be able to authenticate to the "sasl" cluster
