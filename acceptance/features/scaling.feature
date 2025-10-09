Feature: Scaling down broker nodes
  @skip:gke @skip:aks @skip:eks
  Scenario: Skipping incremental scale downs
    Given I create a basic cluster "scaledown" with 5 nodes
    And cluster "scaledown" is stable with 5 nodes
    When I scale "scaledown" to 3 nodes
    Then cluster "scaledown" should be stable with 3 nodes

  @skip:gke @skip:aks @skip:eks
  Scenario: Scaling up nodes
    Given I create a basic cluster "scaleup" with 1 nodes
    And cluster "scaleup" is stable with 1 nodes
    When I scale "scaleup" to 3 nodes
    Then cluster "scaleup" should be stable with 3 nodes
