Feature: Decommissioning brokers
  # note that this test requires both the decommissioner and pvc unbinder
  # run in order to pass  
  @skip:gke @skip:aks @skip:eks
  Scenario: Pruning brokers on failed nodes
    Given I create a basic cluster "decommissioning" with 3 nodes
    And cluster "decommissioning" is stable with 3 nodes
    When I physically shutdown a kubernetes node for cluster "decommissioning"
    And cluster "decommissioning" is unhealthy
    And cluster "decommissioning" has only 2 remaining nodes
    And I prune any kubernetes node that is now in a NotReady status
    Then cluster "decommissioning" should recover
    And cluster "decommissioning" should be stable with 3 nodes