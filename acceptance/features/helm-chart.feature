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

  Scenario: Bootstrap user Secret survives upgrades without server-set metadata
    Given I helm install "sasl-upgrade" "../charts/redpanda/chart" with values:
    ```yaml
     fullnameOverride: saslupgrade

     # Use the test image rather than the chart's default. The default tag
     # may not be published yet while a release is being staged.
     image:
       repository: ${DEFAULT_REDPANDA_REPO}
       tag: ${DEFAULT_REDPANDA_TAG}

     statefulset:
       replicas: 1
       sideCars:
         image:
           tag: dev
           repository: localhost/redpanda-operator

     external:
       enabled: false

     console:
       enabled: false

     auth:
       sasl:
         enabled: true
         users:
           - name: admin
             password: sasl-password
    ```
    When I helm upgrade "sasl-upgrade" "../charts/redpanda/chart" with values:
    ```yaml
     fullnameOverride: saslupgrade

     # Use the test image rather than the chart's default. The default tag
     # may not be published yet while a release is being staged.
     image:
       repository: ${DEFAULT_REDPANDA_REPO}
       tag: ${DEFAULT_REDPANDA_TAG}

     statefulset:
       replicas: 1
       sideCars:
         image:
           tag: dev
           repository: localhost/redpanda-operator

     external:
       enabled: false

     console:
       enabled: false

     auth:
       sasl:
         enabled: true
         users:
           - name: admin
             password: sasl-password
    ```
    Then the stored manifest for release "sasl-upgrade" re-renders Secret "saslupgrade-bootstrap-user" without server-set metadata
    And the stored manifest for release "sasl-upgrade" re-renders Secret "saslupgrade-bootstrap-user" with the password in use
