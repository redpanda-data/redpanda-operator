@serial
Feature: Redpanda Helm Chart

  Scenario: Tolerating Node Failure
    Given I helm install "redpanda" "../charts/redpanda/chart" with values:
    ```yaml
     nameOverride: foobar
     fullnameOverride: bazquux

     # Use the test image rather than the chart's default. The default tag
     # may not be published yet while a release is being staged.
     image:
       repository: ${DEFAULT_REDPANDA_REPO}
       tag: ${DEFAULT_REDPANDA_TAG}

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
    And Pod "bazquux-2" is eventually Pending
    Then Pod "bazquux-2" will eventually be Running
    # As of Redpanda 26.2 `rpk redpanda admin brokers list` renders the
    # sectioned `rpk cluster info` format: the BROKERS table is
    # `ID HOST PORT CORES MEMBERSHIP IS-ALIVE VERSION [UUID]` (the controller
    # row's ID carries a `*` suffix, stripped by `tr` since any broker may be
    # controller), followed by a DISK SPACE section the `awk` window drops by
    # keeping only the header-to-blank-line span of the BROKERS table.
    And kubectl exec -it "bazquux-0" "rpk redpanda admin brokers list | sed -E 's/\s+/ /gm' | awk '/^ID /{found=1} /^$/{found=0} found' | cut -d ' ' -f 1,5 | tr -d '*'" will eventually output:
    ```
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    ```
    And kubectl exec -it "bazquux-0" "rpk redpanda admin brokers list --include-decommissioned | sed -E 's/\s+/ /gm' | awk '/^ID /{found=1} /^$/{found=0} found' | cut -d ' ' -f 1,5 | tr -d '*'" will eventually output:
    ```
    ID MEMBERSHIP
    0 active
    1 active
    3 active
    2 -
    ```
