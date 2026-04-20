Feature: SASL bootstrap user secret lifecycle

  # Reproducer for a password-drift bug surfaced by users migrating from the
  # legacy Helm deploy flow to operator-managed clusters.
  #
  # Scenario the user hit in production:
  #   1. A Redpanda cluster was originally bootstrapped by the old Helm chart
  #      with a pre-existing `<fullname>-bootstrap-user` Secret holding a known
  #      password, referenced from `auth.sasl.bootstrapUser.secretKeyRef`.
  #   2. During cleanup, the user deletes the pre-existing Secret and removes
  #      the `bootstrapUser` block from the CR, expecting the operator to take
  #      full ownership of the Secret (which is the documented "let the
  #      operator manage it" path).
  #   3. The operator's render state looks for a default-named Secret, does not
  #      find it, and generates a NEW Secret with a freshly-randomized password
  #      via `helmette.RandAlphaNum(32)`.
  #      (`charts/redpanda/render_state.go:FetchBootstrapUser`,
  #       `operator/multicluster/secrets.go:secretBootstrapUser`)
  #   4. The running Redpanda process still has the ORIGINAL password in its
  #      internal SCRAM DB because `configwatcher.go:syncUser(..., recreate=false)`
  #      explicitly never updates the internal superuser password.
  #   5. On the next pod restart, the Pod's `RPK_USER` / `RPK_PASS` env vars are
  #      re-materialized from the new Secret, but Redpanda still rejects them:
  #      `rpk cluster info` -> `SASL_AUTHENTICATION_FAILED: Invalid credentials`.
  #
  # Expected behavior: after the operator regenerates the bootstrap user secret,
  # `rpk` inside the pod must continue to authenticate. The fix is either:
  #   (a) the operator preserves the original password (refusing to regenerate
  #       once the cluster has been bootstrapped), or
  #   (b) the operator synchronizes the new password into the running cluster
  #       via the admin API (e.g. `AlterUserSCRAMs`).
  #
  # This scenario is expected to FAIL on current `main` — it is a reproducer,
  # not a regression test. Once the bug is fixed it will start passing.
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
