@operator:none
Feature: Redpanda Helm Chart

  Scenario: Tolerating Node Failure
    Given I helm install "redpanda" "../charts/redpanda/chart" with values:
    ```yaml
     nameOverride: foobar
     fullnameOverride: bazquux

     statefulset:
       sideCars:
         image:
           tag: dev
           repository: localhost/redpanda-operator
         pvcUnbinder:
           enabled: true
           unbindAfter: 15s
         brokerDecommissioner:
           enabled: true
           decommissionAfter: 15s
    ```
    When I stop the Node running Pod "bazquux-2"
    # Wait for the Pod to get evicted from the stopped node.
    And Pod "bazquux-2" is eventually Pending
    # Observe that it gets rescheduled.
    Then Pod "bazquux-2" will eventually be Running
    # And showcase that ghost brokers have been pruned.
    And kubectl exec -it "bazquux-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    ```
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    ```
    And kubectl exec -it "bazquux-0" "rpk redpanda admin brokers list --include-decommissioned | sed -E 's/\s+/ /gm' | cut -d ' ' -f 1,6" will eventually output:
    ```
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    2 -
    ```
