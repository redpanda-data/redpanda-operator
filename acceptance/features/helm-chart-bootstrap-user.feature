Feature: Redpanda Helm Chart bootstrap user

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
