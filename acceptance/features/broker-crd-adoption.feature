@serial
Feature: Broker CRD pod adoption
  @skip:gke @skip:aks @skip:eks
  Scenario: Adopt pods from orphan-deleted StatefulSet and decommission a broker
    Given I create a basic cluster "broker-test" with 3 nodes
    And cluster "broker-test" is stable with 3 nodes
    When I pause reconciliation on cluster "broker-test"
    And I orphan-delete the StatefulSet for cluster "broker-test"
    And I create Broker CRs for cluster "broker-test"
    # Adoption and pod-ensure are not gated on roll-grants (RFC Q5).
    Then all Broker CRs for cluster "broker-test" should be Running
    And cluster "broker-test" admin API should show 3 brokers
    When I update Broker "broker-test-0" pod template with env "ROTATION_TEST=applied" for cluster "broker-test"
    # Rotation IS gated: only the targeted broker gets a grant.
    And I grant a roll-grant to Broker "broker-test-0"
    Then Broker "broker-test-0" pod should have env "ROTATION_TEST" = "applied"
    And all Broker CRs for cluster "broker-test" should be Running
    When I set decommission on Broker "broker-test-2"
    Then Broker "broker-test-2" should reach phase "Decommissioned"
    And cluster "broker-test" admin API should show 2 brokers
