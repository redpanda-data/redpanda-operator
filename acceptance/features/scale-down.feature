Feature: Scaling down broker nodes
  @skip:gke @skip:aks @skip:eks
  Scenario: Skipping incremental scale downs
    Given I create a basic cluster "scaledown" with 5 nodes
    And cluster "scaledown" is stable with 5 nodes
    When I scale "scaledown" to 3 nodes
    Then cluster "scaledown" should be stable with 3 nodes
    # K8S-755: after brokers leave, the in-pod rpk config must drop their seed
    # entries on every remaining pod, without a restart. Asserting across all
    # pods (not just one) also guards the per-pod, non-leader-elected behavior.
    And the in-pod rpk broker list for every broker in "scaledown" matches the current pods without a restart
