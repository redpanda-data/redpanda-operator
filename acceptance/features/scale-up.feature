Feature: Scaling up broker nodes
  @skip:gke @skip:aks @skip:eks
  Scenario: Scaling up nodes
    Given I create a basic cluster "scaleup" with 1 nodes
    And cluster "scaleup" is stable with 1 nodes
    When I scale "scaleup" to 3 nodes
    Then cluster "scaleup" should be stable with 3 nodes
