@vcluster
Feature: Migrating NodePools to RedpandaBrokerPools without a license
  # A pool owned by a RedpandaBrokerPool needs an enterprise license. Without
  # one the operator freezes the pool instead of dropping it from the cluster.
  # The feature runs its own operator in a vcluster, so it can start
  # unlicensed and add the license later.

  @skip:gke @skip:aks @skip:eks
  Scenario: Without a license, deleting a NodePool freezes its pool instead of removing it
    Given the enterprise license is available as Secret "redpanda-license"
    And I helm install "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    additionalCmdFlags:
    - --enable-v2-nodepools
    """
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: Redpanda
    metadata:
      name: np-migration
    spec:
      clusterSpec:
        image:
          repository: ${DEFAULT_REDPANDA_REPO}
          tag: ${DEFAULT_REDPANDA_TAG}
        external:
          enabled: false
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
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: NodePool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    And cluster "np-migration" is stable with 2 nodes
    When I snapshot pod UIDs for cluster "np-migration"
    And I apply Kubernetes manifest:
    """
    ---
    apiVersion: cluster.redpanda.com/v1alpha2
    kind: RedpandaBrokerPool
    metadata:
      name: blue
    spec:
      clusterRef:
        name: np-migration
      replicas: 1
      sidecarImage:
        repository: localhost/redpanda-operator
        tag: dev
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "LicenseInvalid" and a message containing "no license configured"
    # Deleting the NodePool hands the pool to the unlicensed RedpandaBrokerPool.
    # The operator freezes the pool rather than dropping it, which would
    # decommission its brokers.
    When I delete NodePool "blue"
    Then Redpanda "np-migration" should eventually have condition "ResourcesSynced" with reason "TerminalError" and a message containing "RedpandaBrokerPools blue require an enterprise license"
    And the StatefulSet for pool "blue" of cluster "np-migration" should keep 1 replica for 60 seconds
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
    # Licensing the operator resumes management with the pool intact.
    When I helm upgrade "redpanda-operator" "../operator/chart" with values:
    """
    image:
      tag: dev
      repository: localhost/redpanda-operator
    crds:
      enabled: true
      experimental: true
    enterprise:
      licenseSecretRef:
        name: redpanda-license
        key: redpanda.license
    additionalCmdFlags:
    - --enable-v2-nodepools
    """
    Then RedpandaBrokerPool "blue" should eventually have condition "Deployed" with reason "Deployed"
    And cluster "np-migration" should be stable with 2 nodes
    And pods for cluster "np-migration" should have the same UIDs as the snapshot
