Feature: SASL bootstrap user secret lifecycle

  # Regression test for a password-drift bug surfaced by users migrating from
  # the legacy Helm deploy flow to operator-managed clusters.
  #
  # Scenario the user hit in production:
  #   1. A Redpanda cluster was originally bootstrapped by the old Helm chart
  #      with a pre-existing `<fullname>-bootstrap-user` Secret holding a known
  #      password, referenced from `auth.sasl.bootstrapUser.secretKeyRef`.
  #   2. During cleanup, the user deletes the pre-existing Secret and removes
  #      the `bootstrapUser` block from the CR, expecting the operator to take
  #      full ownership of the Secret (the documented "let the operator
  #      manage it" path).
  #   3. The operator's render state looks for a default-named Secret, does not
  #      find it, and generates a new Secret with a freshly-randomized password.
  #   4. Previously the running Redpanda kept the original password in its
  #      internal SCRAM DB because the sidecar configwatcher only ever *created*
  #      the internal superuser and never updated it. `rpk` inside any pod that
  #      restarted after the rotation then failed with
  #      `SASL_AUTHENTICATION_FAILED: Invalid credentials`.
  #
  # Fix: the configwatcher now mirrors the Secret's password into Redpanda's
  # SCRAM DB on every sync via `UpdateUser` (AlterUserSCRAMs), so a rotated
  # bootstrap user Secret propagates into the running cluster.
  @skip:gke @skip:aks @skip:eks
  Scenario: Bootstrap user secret deleted and regenerated; rpk still authenticates
    Given I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: bootstrap-regen
    spec:
      clusterSpec:
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        statefulset:
          replicas: 1
          sideCars:
            image:
              tag: dev
              repository: localhost/redpanda-operator
            controllers:
              image:
                tag: dev
                repository: localhost/redpanda-operator
        external:
          enabled: false
        auth:
          sasl:
            enabled: true
    """
    And cluster "bootstrap-regen" is stable with 1 nodes
    And rpk is configured correctly in "bootstrap-regen" cluster
    When I delete the bootstrap user secret for cluster "bootstrap-regen"
    And the bootstrap user secret for cluster "bootstrap-regen" is regenerated with a new password
    And I restart all pods in cluster "bootstrap-regen"
    Then cluster "bootstrap-regen" is stable with 1 nodes
    And rpk is configured correctly in "bootstrap-regen" cluster
